# Отвязка от Telegram: вход через Google/Apple + диплинк-вход в группу

> **Ревизия 5** — после четырёх кругов ревью (plan-review → Fable → Codex → Opus 5).
> Круг 4 (Opus 5) исправил: `getFrom(u).ID` как доменный id в 17 местах бота (план закрывал только 3, а `operation_screen.go:2135` писал сырой telegram id в `Operation.Donor`); недостижимость `chat_state`/`bug_report`/`push_outbox` из REST-графа, из-за которой чистка PII в Task 13 была неисполнима; аллокатор, пробрасываемый в `rest.Server`, тогда как нужен он в репозитории; дубль аккаунта после отвязки telegram; отсутствие отзыва Apple-токенов при удалении аккаунта; второй вызов `parseRoomCode`; неверное описание seed-данных с `recipients`; `image: mongo` вместо `mongo:7` в compose.
>
> **Ревизия 4** — после трёх кругов ревью (plan-review → Fable → Codex).
> Круг 3 (Codex) исправил: порядок удаления аккаунта, при котором сбой посередине оставлял живой аккаунт с затёртым именем; молча ломающийся `chat_state` у привязанных аккаунтов; гонку, возвращающую личность на tombstone; PII в `chat_state`/`bug_report`/`push_outbox`, которые план ошибочно объявлял «техническим мусором».
>
> **Ревизия 3** — после двух кругов ревью.
> Круг 1 исправил: самовосстанавливающийся бэкфилл, отсутствие инфраструктуры mongo-тестов, ломающиеся фейки, неверный список файлов в задаче про отправку, фиктивную инвалидацию токена, утечку PII во встроенные снимки.
> Круг 2 исправил: невозможность записать `google_sub`/`apple_sub` (метода не существовало), потерю кликабельных упоминаний у telegram-пользователей, падение анонимизации на `recipients: null`, потерю языка пользователя, самоубийство демо-аккаунта ревьюера Apple, коллизию ключей троттлинга, неверную обработку гонок при создании.

## Overview

Сегодня **личность пользователя = telegram id**: `api.User.ID int` одновременно является telegram user id, `_id` в Mongo и `sub` в JWT. Без Telegram в Splitty попасть нельзя, а появились люди, которые пользуются ботом и хотят приложение без привязки к мессенджеру.

Задача — сделать Telegram **одним из** способов входа:

- вход через **Google** (iOS + Android) и **Sign in with Apple** (iOS);
- **удаление аккаунта** в приложении (обязательно по Apple Guideline 5.1.1(v));
- **диплинк-вход в группу** (`https://<domain>/join/<roomId>`), включая «пришёл по ссылке, аккаунта ещё нет».

**Ключевое решение:** `_id int` остаётся и не меняет значений — меняется только его *смысл*: это больше не «telegram id», а «номер пользователя Splitty». Telegram становится отдельным nullable-полем `telegram_id`.

### Инварианты, которые нельзя нарушать

1. **`_id` существующих пользователей не меняется никогда.**
2. **Документы `room` не мигрируются.** Единственное исключение — Task 13 (анонимизация), и там меняются только `display_name`/`user_name` и вычищаются поля личности; id, суммы и доли — никогда.
3. **Логика расчёта долгов и балансов не трогается ни одной задачей.** Если задача этого требует — остановиться и записать ⚠️ в план.
4. **`user.ID` больше никогда не передаётся в Telegram API.** Только `user.TelegramID`. Единственное исключение — id, взятые напрямую из входящего telegram-апдейта (см. Task 7).
5. **Поля личности (`telegram_id`, `google_sub`, `apple_sub`, `email`) никогда не попадают во встроенные снимки комнат.**

## Context (from discovery)

### Backend (Go, `internal/`)

| Что | Где | Факт |
|---|---|---|
| Модель пользователя | `internal/api/tg.go:82-102` | `User.ID int` с тегом `bson:"_id"` |
| Выпуск/разбор JWT | `internal/rest/auth.go:35-55` | HS256, `sub` = `strconv.Itoa(userId)`, TTL 90 дней |
| Auth middleware | `internal/rest/auth.go:58-78` | **только парсит токен, в базу не ходит** |
| Способы входа | `internal/rest/auth.go:132,196,279` | `/auth/telegram`, `/auth/code`, `/auth/dev` |
| Общий хвост входа | `internal/rest/auth.go:~296` | `finishAuth` — upsert + issueToken |
| **Единственная точка входа telegram-личности** | `internal/events/telegram.go:83` | `UpsertUser(ctx, *user)`; узкий интерфейс `UserService` объявлен на `internal/events/telegram.go:20` |
| **Сырой пользователь из апдейта** | `internal/bot/bot.go:176` | `getFrom(update)` возвращает `From` из апдейта — там **telegram id**, а не номер Splitty; используется как доменный id в 17 местах (см. Task 6) |
| Отправка в telegram | 15 мест в 5 файлах (см. Task 7) | `NewMessage(int64(user.ID), …)` |
| Ссылка на пользователя | `internal/bot/tg_helper.go:222` | `tg://user?id=%d` |
| Аватары | `internal/rest/avatar.go:160-161` | `getUserProfilePhotos?user_id=<_id>` |
| Репозиторий пользователя | `internal/repository/repository.go:19-32`, `:495+` | все методы принимают `userId int` |
| **Upsert-воскрешение** | `repository.go:596,607,618,639,650` | 5 методов с `SetUpsert(true)` по `_id` — создадут документ заново после удаления |
| Запись снимка в комнату | `repository.go:208-221` | `JoinToRoom` делает `$push {users: u}` **целым `api.User`** |
| Пример индексов | `repository.go:143-160` | `MongoLoginCodeRepository.EnsureIndexes`, вызов — `cmd/splitty/main.go:139` |
| Конфиг REST | `internal/rest/server.go:32-42` | собирается вручную в `cmd/splitty/main.go:122-137` |
| Роуты | `internal/rest/server.go:147-195` | `http.ServeMux`, паттерны `"POST /api/v1/…"` |
| Троттлинг | `internal/rest/throttle.go`, использование `auth.go:199` | `s.authThrottle.allow("ip:"+clientIP(r), N)` |
| DTO | `internal/rest/dto.go:37-43,206-238` | `meDto`, `authResponseDto`, `toMeDto`, `toUserDto` |
| **Фейки для тестов** | `internal/rest/fakes_test.go:45-182` | `fakeUserRepo` реализует **весь** `repository.UserRepository`; `fakeRoomRepo` — `:184-442` |
| Env-конфиг | `cmd/splitty/config.go` | теги `env:"…" envDefault:"…"` |
| DI | `cmd/splitty/wire.go` | **собирает только бота**; REST-сервер собран руками в `main.go` — wire для этого плана не нужен |

### Денормализация (почему не меняем тип id)

`internal/api/service_models.go`: `Room.Members *[]User` (:12), `Operation.Donor *User` (:35), `Operation.Recipients *[]User` (:36), `RecipientWithSum.User` (:56), `Debt.Lender/Debtor *User` (:107-108), `RoomStatesUsers.*Ids []int` (:26-28), `Operation.NotificationSent []int` (:40), `ItemShare.UserId int` (:78).

Снимки уже считаются несвежими — `internal/bot/notifier.go:80-99` явно перечитывает канонический документ, потому что `Notify` в снимках всегда `nil`.

### iOS (`ios/Splitty/`)

- `Core/Models.swift:8-12` — `User { id: Int, … }`; **на `Models.swift:5` комментарий «id пользователей — Telegram user id», после плана он станет ложью**
- `Core/SessionStore.swift` — JWT в Keychain, `loginWithCode` (:202), `loginDev` (:178), `logout` (:213)
- `Core/APIClient.swift:219,228-232,306` — `/auth/dev`, `/auth/code`, `joinRoom`
- `Features/Auth/LoginView.swift` — единственный экран входа; **`devLoginFields` (:195-217) содержит лейблы «Telegram ID»/«Имя»/«Войти», на которые завязан `DemoFlowUITests`**
- `Features/Groups/JoinGroupView.swift:22-34` — парсер кода/ссылки, живёт **приватным свойством внутри View** (переиспользовать нельзя без выноса)
- `App/SplittyApp.swift`, `App/RootView.swift` — точка входа и переключатель логин/табы
- `project.yml` — XcodeGen: `packages:`, `entitlements.properties`, `info.properties`

### Android (`android/app/src/main/java/com/zagir/splitty/`)

- `core/model/Models.kt:61-65` — `User { id: Long, … }`
- `core/session/SessionStore.kt` — DataStore, токен AES-GCM
- `ui/auth/LoginViewModel.kt`, `ui/auth/LoginScreen.kt`
- `ui/profile/ProfileScreen.kt` + `ui/profile/ProfileViewModel.kt` (`logout()` на :132) — **каталога `ui/account/` не существует**
- `ui/AppRoot.kt:36-51`, `ui/main/MainScaffold.kt:263+` (`MainNavHost`, `MainRoutes`)
- `ui/groups/GroupsListViewModel.kt:~187` — `internal fun parseRoomCode`
- `MainActivity.kt`; `AndroidManifest.xml` — **`MainActivity` без `android:launchMode`** (standard), диплинков нет
- `app/build.gradle.kts:217-238` — **поимённый список `requiredSerializers`**: новые `@Serializable`-модели обязаны быть туда добавлены, иначе R8 их выкинет и релиз упадёт в рантайме

### Среда, сборка, тесты

```bash
# Go: работаем локальным 1.23.5 с GOTOOLCHAIN=local — тулчейн из go.mod не тянем
GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go build ./...
GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go test ./internal/...
GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go vet ./...
~/sdk/go1.23.5/bin/gofmt -w <files>

# Android
cd android && ./gradlew :app:testDebugUnitTest
cd android && ./gradlew :app:assembleDebug

# iOS (доступный симулятор: iPhone 17 Pro, OS 26.2 — сверить
# `xcodebuild -showdestinations -scheme Splitty` перед запуском)
cd ios && xcodegen generate
cd ios && xcodebuild -project Splitty.xcodeproj -scheme Splitty -configuration Debug \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' build
cd ios && xcodebuild test -project Splitty.xcodeproj -scheme Splitty \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro'
```

**Mongo для тестов доступен**: локально поднят контейнер `splitty-app-mongo-1` (образ `mongo:7`) на `localhost:27017`, образ уже скачан — сеть не нужна.

**Сеть доступна** (проверено 30.07.2026: github и `proxy.golang.org` отвечают). Новые зависимости (SPM `GoogleSignIn-iOS` в Task 16, `androidx.credentials`/`googleid` в Task 18) должны резолвиться штатно. Аварийный выход на случай, если резолв всё же падает по сети: агент помечает задачу `⚠️ заблокировано: нет сети` в этом файле, коммитит код без сборки и **переходит к следующей задаче**, не пытаясь обойти — но это исключение, а не ожидаемый сценарий. **Go-зависимости всё равно не добавляются**: всё нужное (`golang-jwt/jwt/v5`, `crypto/*`) уже есть, новых модулей план не требует.

## Development Approach

- **testing approach**: Regular (код, затем тесты в той же задаче)
- задачи строго по порядку, каждая завершается полностью
- **каждая задача обязана содержать тесты отдельными пунктами чеклиста**
- **все тесты зелёные до перехода к следующей задаче**
- **обратная совместимость на каждом шаге**: существующие telegram-пользователи и бот продолжают работать
- коммит на задачу, автор `AlmazNurmukhametov <zagirnur@gmail.com>`, без упоминаний Claude/Anthropic
- при изменении объёма — обновлять этот файл (➕ новая задача, ⚠️ блокер)

### Правило расширения интерфейсов

Добавление метода в `repository.UserRepository` или `repository.RoomRepository` **ломает компиляцию всего пакета `internal/rest`**, потому что `internal/rest/fakes_test.go` реализует эти интерфейсы целиком. Любая задача, трогающая интерфейс, **обязана** в том же коммите дописать метод в фейк. Это внесено в Files соответствующих задач.

## Testing Strategy

- **Чистая логика** (`HasTelegram`, `Snapshot`, парсеры, верификация JWT) — обычные unit-тесты, без mongo.
- **Репозиторий** — интеграционные тесты против живого mongo: URI из `MONGO_TEST_URI`, по умолчанию `mongodb://localhost:27017`, база `splitty_test_<random>` с очисткой в `t.Cleanup`. Если mongo недоступен — `t.Skip` с внятным сообщением (см. Task 1).
- **Go**: table-driven, по образцу `internal/rest/*_test.go`.
- **Android**: `testDebugUnitTest`; при изменении экранов перезаписать Roborazzi-эталоны.
- **iOS**: `SplittyTests` + `SplittyUITests`; **не менять лейблы dev-входа** (`LoginView.swift:195-217`).
- **Миграция**: обязателен прогон дважды подряд с проверкой, что второй — no-op.

## Solution Overview

### Модель личности

```
user {
  _id:         int        // НОМЕР ПОЛЬЗОВАТЕЛЯ SPLITTY (не telegram id!)
  telegram_id: int?       // nullable, unique sparse
  google_sub:  string?    // nullable, unique sparse
  apple_sub:   string?    // nullable, unique sparse
  email:       string?    // best-effort, НЕ идентификатор
  deleted_at:  time?      // tombstone: аккаунт удалён (см. Task 13)
  ... существующие поля без изменений
}
```

- **Бэкфилл**: существующим `telegram_id = _id`, **одноразово**, с маркером в коллекции `migration`.
- **Новые пользователи без Telegram** получают `_id` из счётчика от `1_000_000_000_000` (10¹²) — выше реальных telegram id (~10¹⁰) и много ниже 2⁵³.
- **Telegram-пользователи ищутся по `telegram_id`**, не по `_id`.
- **Отправка в Telegram** — по `user.TelegramID` канонического документа; пусто → канал пропускается (push работает).
- **Снимки в комнатах** пишутся через `User.Snapshot()`, который обнуляет поля личности.

### Потоки входа

1. `/auth/telegram`, `/auth/code` — как сейчас, но резолв по `telegram_id`; `sub` токена = `_id` **найденного** пользователя.
2. `/auth/google` — `{idToken}`; верификация по JWKS Google; поиск по `google_sub` → иначе новый с синтетическим `_id`.
3. `/auth/apple` — `{idToken, displayName, nonce}`; **email и имя приходят только при первом входе**.
4. `/me/link/{provider}` — привязка; занятая личность → `409 identity_taken`. Слияние аккаунтов вне объёма.
5. `DELETE /me` — tombstone + чистка PII + анонимизация снимков; долги сохраняются.

### Почему tombstone, а не удаление документа

Middleware `auth` не ходит в базу, а `currentUser` вызывается лишь в 7 хендлерах из ~25 — значит после удаления документа токен с TTL 90 дней продолжал бы работать. Хуже: пять методов репозитория с `SetUpsert(true)` **воскресили** бы пользователя пустым документом от первого же запроса.

Решение: документ остаётся с `deleted_at`, PII вычищается, поля личности освобождаются (можно зарегистрироваться заново), а middleware отвергает токены удалённых.

## Implementation Steps

### Task 1: Инфраструктура интеграционных тестов репозитория

**Files:**
- Create: `internal/repository/testsupport_test.go`
- Modify: `README.md`
- Modify: `docker-compose.yml`

- [x] создать хелпер `func testDB(t *testing.T) *mongo.Database`: читает `MONGO_TEST_URI` (по умолчанию `mongodb://localhost:27017`), подключается с таймаутом 3 с
- [x] при недоступном mongo — `t.Skip("mongo недоступен: задайте MONGO_TEST_URI или поднимите docker compose up mongo")`, **не** `t.Fatal`
- [x] база на каждый тест — уникальная (`splitty_test_<nanotime>_<counter>`), удаление в `t.Cleanup` через `db.Drop(ctx)`
- [x] добавить хелпер `seedUsers(t, db, users ...api.User)` — вставка документов напрямую, для подготовки состояния
- [x] написать самопроверку: тест, который создаёт базу, пишет документ, читает его и убеждается, что после `Cleanup` базы нет
- [x] **зафиксировать `image: mongo:7` в `docker-compose.yml:36`**: сейчас там `image: mongo`, то есть `latest`, а локально скачан только `mongo:7` (контейнер `splitty-app-mongo-1`). Без сети `docker compose up -d mongo` попытается стянуть `latest` и упадёт — инструкция из README не сработает ровно тогда, когда она нужна
- [x] описать в `README.md` раздел «Тесты репозитория»: как поднять mongo (`docker compose up -d mongo` после фиксации тега, либо `docker start splitty-app-mongo-1`, если контейнер уже создан), как задать `MONGO_TEST_URI`, что тесты скипаются без него
- [x] `GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go test ./internal/repository/...` — зелёные (или явный skip) перед Task 2

### Task 2: Поля личности в модели User + защита снимков

**Files:**
- Modify: `internal/api/tg.go`
- Create: `internal/api/user_test.go`

- [x] добавить в `User` поля: `TelegramID *int` (`bson:"telegram_id,omitempty" json:"-"`), `GoogleSub string` (`bson:"google_sub,omitempty" json:"-"`), `AppleSub string` (`bson:"apple_sub,omitempty" json:"-"`), `Email string` (`bson:"email,omitempty" json:"-"`), `DeletedAt *time.Time` (`bson:"deleted_at,omitempty" json:"-"`), `AppleRefreshToken string` (`bson:"apple_refresh_token,omitempty" json:"-"` — нужен для отзыва токенов при удалении аккаунта, см. Tasks 11 и 13)
- [x] у всех новых полей `json:"-"` — они не должны протечь в API-ответы
- [x] переписать комментарий к `User.ID`: это НОМЕР ПОЛЬЗОВАТЕЛЯ SPLITTY, telegram id живёт в `TelegramID`
- [x] добавить `func (u *User) HasTelegram() bool` — `u != nil && u.TelegramID != nil && *u.TelegramID != 0`
- [x] добавить `func (u *User) IsDeleted() bool` — `u != nil && u.DeletedAt != nil`
- [x] добавить **`func (u User) Snapshot() User`** — возвращает копию с обнулёнными `TelegramID`, `GoogleSub`, `AppleSub`, `Email`, `AppleRefreshToken`, `DeletedAt`, `PushTokens`, `Notify`, `Aliases`, `BankDetails`; предназначен для записи во встроенные снимки комнат
- [x] задокументировать в комментарии к `Snapshot`, зачем он: `JoinToRoom` (`repository.go:219`) и операции пишут `api.User` целиком, и без санитайза поля личности (включая `email`) осели бы в документах `room` навсегда
- [x] написать тесты `HasTelegram` (nil-получатель, nil-поле, ноль, валидное значение) и `IsDeleted`
- [x] написать тест `Snapshot`: все поля личности обнулены, а `ID`/`Username`/`DisplayName`/`UserLang` сохранены
- [x] написать тест-страж: рефлексией пройти по полям `User` и убедиться, что `Snapshot` обнуляет все поля из списка чувствительных — чтобы новое поле, добавленное позже, не утекло молча
- [x] написать bson-тест: `omitempty` не пишет пустые новые поля
- [x] `go test ./internal/...` — зелёные перед Task 3

### Task 3: Методы поиска по личностям, индексы и санитайз снимков в репозитории

**Files:**
- Modify: `internal/repository/repository.go`
- Modify: `internal/rest/fakes_test.go`
- Create: `internal/repository/user_identity_test.go`
- Modify: `cmd/splitty/main.go`

- [x] добавить в интерфейс `UserRepository` (`repository.go:19`): `FindByTelegramID(ctx, tgID int) (*api.User, error)`, `FindByGoogleSub(ctx, sub string) (*api.User, error)`, `FindByAppleSub(ctx, sub string) (*api.User, error)`
- [x] реализовать три метода по образцу `FindById` (`repository.go:495`), включая `mongo.ErrNoDocuments`
- [x] **все три обязаны исключать удалённых**: добавить в фильтр `deleted_at: {$exists: false}` — иначе tombstone заблокирует повторную регистрацию с той же личностью
- [x] **добавить метод создания `CreateIdentityUser(ctx, u api.User) error` через `InsertOne`** (именно insert, не upsert — на duplicate key строится retry в Tasks 6/10/11). Существующий `UpsertUser` (`repository.go:595-604`) пишет **только** `_id`, `user_lang`, `display_name`, `user_name` — записать через него `google_sub`/`apple_sub`/`email` невозможно, поэтому без этого метода Tasks 10/11 физически неисполнимы
- [x] побочный полезный эффект: `UpsertUser` — частичный `$set`, поэтому вход по `/auth/code` и апдейты бота **не затирают** поля личности
- [x] **применить санитайз снимка**: в `JoinToRoom` (`repository.go:208`) заменить `$push {users: u}` на `$push {users: u.Snapshot()}`; то же в `SaveRoom`, `CreateOperation`, `CreateOperationIfAbsent`, `UpdateOperation` для `Donor`, `Recipients`, `RecipientsWithSum[].User`
- [x] санитайз делать **на границе репозитория**, а не у вызывающих — так его нельзя забыть
- [x] добавить `MongoUserRepository.EnsureIndexes(ctx)` по образцу `repository.go:148`: **unique + sparse** индексы по `telegram_id`, `google_sub`, `apple_sub` (без `sparse` unique упадёт на документах, где поля нет)
- [x] вызвать `userRepository.EnsureIndexes(ctx)` в `cmd/splitty/main.go` рядом с `loginCodeRepository.EnsureIndexes` (:139), **но ошибку считать фатальной**, а не Warn: без unique-индексов возможны дубликаты личностей
- [x] **дописать все новые методы в `fakeUserRepo`** (`internal/rest/fakes_test.go:45-182`) — иначе пакет `rest` не скомпилируется. Учесть: `service.UserService` **встраивает** `repository.UserRepository`, поэтому расширение интерфейса протекает и в бота — проверить, что `internal/service` и `internal/bot` собираются
- [x] написать интеграционные тесты (через хелпер из Task 1): поиск по каждой из трёх личностей находит нужного; удалённый (с `deleted_at`) не находится; unique-индекс отвергает второго пользователя с тем же `google_sub`; два пользователя без `google_sub` сосуществуют (sparse работает); `CreateIdentityUser` на занятом `_id` возвращает duplicate key; `UpsertUser` не затирает `google_sub`
- [x] написать тест санитайза: после `JoinToRoom` документ комнаты не содержит `email`, `google_sub`, `apple_sub`, `telegram_id`
- [x] `go test ./internal/...` — зелёные перед Task 4

### Task 4: Аллокатор номеров пользователей Splitty

**Files:**
- Create: `internal/repository/sequence.go`
- Create: `internal/repository/sequence_test.go`
- Modify: `internal/repository/repository.go`
- Modify: `internal/rest/server.go`
- Modify: `cmd/splitty/main.go`

- [x] создать `MongoSequenceRepository` над коллекцией `sequence` с методом `NextUserID(ctx) (int, error)`
- [x] реализовать через `FindOneAndUpdate` с `$inc` и `SetUpsert(true)`, документ `{_id: "user_id", value: int}`, `ReturnDocument(options.After)`
- [x] константа `firstSyntheticUserID = 1_000_000_000_000`; при первом вызове вернуть именно её
- [x] **`$inc` и `$setOnInsert` на одно поле в Mongo конфликтуют** («updating the path ... would create a conflict») — наивная реализация упадёт. Использовать один из рецептов и записать выбранный комментарием: (а) хранить в документе **смещение**, начиная с 0, и возвращать `firstSyntheticUserID + value`; либо (б) до первого `$inc` сделать `InsertOne({_id:"user_id", value: firstSyntheticUserID-1})` с игнорированием duplicate key. Вариант (а) проще и не имеет гонки на старте
- [x] **⚠️ аллокатор нужен в ДВУХ графах, и главный из них — не REST.** `UpsertTelegramUser` (Task 6) — метод `repository.UserRepository`, и вызывается он из `internal/events/telegram.go:83`, то есть из графа **бота**, который к `rest.Server` отношения не имеет. Пробросить аллокатор только в `rest.Server` — значит сделать шаг «взять номер из аллокатора» в Task 6 неисполнимым
- [x] решение: аллокатор живёт **внутри `MongoUserRepository`**. `NewUserRepository(col *mongo.Database)` (`repository.go:123`) уже принимает базу — поднять коллекцию `sequence` прямо в конструкторе, **сигнатуру не менять**. Тогда оба вызова (`cmd/splitty/main.go:122` и `cmd/splitty/wire_gen.go:53`) остаются как есть
- [x] **если сигнатуру всё же менять** — правятся ОБА вызова, включая `wire_gen.go:53`, и утверждение из Context «wire для этого плана не нужен» перестаёт быть верным: `wire.go:30` придётся перегенерировать. Это дороже; выбирать только при веской причине и записать её комментарием
- [x] пробросить аллокатор ещё и в `rest.Server` — он нужен `handleAuthGoogle`/`handleAuthApple` (Tasks 10-11): добавить поле в структуру `Server` (`internal/rest/server.go:47`) и параметр в `NewServer`, обновив единственный вызов в `main.go:137` и вызовы в тестах
- [x] объявить в `internal/rest` узкий интерфейс `userIDAllocator interface { NextUserID(ctx) (int, error) }` — чтобы в тестах подставлялся фейк без mongo
- [x] написать интеграционные тесты: первый вызов возвращает `firstSyntheticUserID`; последовательные монотонно растут; 10 конкурентных горутин дают 10 различных значений
- [x] `go test ./internal/...` — зелёные перед Task 5

### Task 5: Одноразовый бэкфилл telegram_id = _id

**Files:**
- Create: `internal/repository/migrate.go`
- Create: `internal/repository/migrate_test.go`
- Modify: `cmd/splitty/main.go`

- [x] создать `func BackfillTelegramID(ctx, db *mongo.Database) (modified int64, err error)`
- [x] **одноразовость через маркер**: перед работой проверить коллекцию `migration` на документ `{_id: "backfill_telegram_id"}`; если он есть — вернуть `0, nil` без запросов к `user`; после успешного прогона — вставить маркер с временем
- [x] **сузить фильтр**: `{telegram_id: {$exists: false}, google_sub: {$exists: false}, apple_sub: {$exists: false}, deleted_at: {$exists: false}, _id: {$lt: firstSyntheticUserID}}` — маркер защищает от повторного прогона, а фильтр от катастрофы, если маркер потеряют
- [x] **`deleted_at` в фильтре обязателен**: у tombstone-документов (Task 13) поле `telegram_id` вычищено, и бэкфилл вернул бы им привязку — удалённому снова пошли бы telegram-уведомления, а повторная регистрация того же telegram-аккаунта упёрлась бы в unique-индекс
- [x] обновление — агрегационный pipeline-update: `[{$set: {telegram_id: "$_id"}}]`
- [x] **зачем двойная защита**: без неё каждый рестарт сервера проставлял бы `telegram_id = _id` (≥ 10¹²) google-пользователям — тогда нотифаер полез бы слать в Telegram на несуществующий chat_id, аватар пошёл бы в Telegram API, а отвязка telegram (Task 12) молча откатывалась бы при каждом рестарте
- [x] **фиксированный порядок в `main.go`**: (1) `userRepository.EnsureIndexes` — sparse-индексы, (2) `BackfillTelegramID`. Обе операции фатальны при ошибке. Sparse снимает конфликт с документами без поля, поэтому порядок именно такой и другого выбора нет
- [x] логировать число обновлённых документов на уровне Info
- [x] написать интеграционные тесты: документ без `telegram_id` получает его равным `_id`; документ с заданным `telegram_id` не меняется; **пользователь с `google_sub` и без `telegram_id` НЕ затрагивается**; **пользователь с `_id ≥ 10¹²` НЕ затрагивается**; **tombstone (`deleted_at`) НЕ затрагивается**; маркер создаётся ровно один раз
- [x] тест идемпотентности сформулировать проверяемо: после первого прогона вставить sentinel-пользователя без `telegram_id` и с `_id < 10¹²`, вызвать бэкфилл второй раз, убедиться что **sentinel остался без `telegram_id`** и вернулось `0`. Формулировка «не делает запросов к user» без command monitor непроверяема — не использовать её
- [x] `go test ./internal/...` — зелёные перед Task 6

### Task 6: Резолв telegram-личности через telegram_id

**Files:**
- Modify: `internal/repository/repository.go`
- Modify: `internal/events/telegram.go`
- Modify: `internal/rest/auth.go`
- Modify: `internal/rest/fakes_test.go`
- Create: `internal/repository/upsert_telegram_test.go`
- Modify: `internal/rest/auth_test.go`
- Modify: `internal/bot/all_room.go`, `internal/bot/debt_screen.go`, `internal/bot/setting_screen.go`, `internal/bot/statistic_screen.go`, `internal/bot/room_screen.go`, `internal/bot/operation_screen.go`, `internal/bot/room_creating.go` (замена `getFrom` — см. ниже)

- [x] добавить в `UserRepository` метод `UpsertTelegramUser(ctx, tgID int, username, displayName, userLang string) (*api.User, error)`
- [x] **параметр `userLang` обязателен**: сейчас `transformUser` (`internal/events/telegram.go:266-273`) передаёт `UserLang: i.LanguageCode`, и `UpsertUser` пишет его при каждом апдейте. Сигнатура без языка означала бы, что новые пользователи бота создаются с пустым `user_lang` и `I18n` отвечает русскоязычным по-английски (дефолт `DEFAULT_LANGUAGE=en`)
- [x] `user_lang` ставить при создании и когда в базе пусто; заполненный — не затирать (как в `finishAuth`)
- [x] логика: найти по `telegram_id` → **нашли: вернуть его, не трогая `_id`**; не нашли: создать через `CreateIdentityUser` с `_id = tgID` и `telegram_id = tgID`
- [x] **обработка duplicate key при создании — циклом, а не одной веткой**: (1) на E11000 сначала **повторить `FindByTelegramID`** — в 99% случаев это гонка двух апдейтов одного нового пользователя, и второй должен просто подобрать созданного первым; (2) только если по `telegram_id` по-прежнему пусто, а `_id = tgID` занят **другим** пользователем (у него `telegram_id != tgID`) — взять номер из аллокатора; (3) до 3 попыток
- [x] **почему не «занят → сразу аллокатор»**: при гонке двух апдейтов проигравший вставил бы второго пользователя с тем же `telegram_id`, получил E11000 уже по unique-индексу и потерял апдейт — вход бы сломался
- [x] **⚠️ привести `chat_state` к каноническому id — это НЕ редкий случай.** Сейчас `populateChatState` (`events/telegram.go:121-127`) ищет по **сырому** telegram id из апдейта, большинство мест сохраняют по `getChatID(u)` (telegram chat id), но **`operation_screen.go:1739` сохраняет по `u.User.ID`** — каноническому номеру. Сегодня всё это совпадает, потому что `_id == telegram id`. После Task 12 любой google-первый пользователь, привязавший telegram, получает `_id ≥ 10¹²` при telegram id порядка 10⁹ — и состояния, сохранённые по канонику, **никогда не найдутся**. Молча ломаются многошаговые сценарии бота (добавление файла, ввод суммы долга)
- [x] стандартизировать: `populateChatState` ищет по `upd.User.ID` (канонический после `UpsertTelegramUser`), все `api.ChatState{UserId: …}` в `internal/bot/` (`room_creating.go:41`, `setting_screen.go:658`, `operation_screen.go:186,258,779,1081,1739,1972,2026`) пишут `u.User.ID`
- [x] на переходный период добавить fallback: не нашли по каноническому — искать по сырому telegram id (у людей могут быть незавершённые сценарии в момент выкатки)
- [x] написать тест: пользователь с `_id ≥ 10¹²` и `telegram_id` порядка 10⁹ проходит многошаговый сценарий бота — состояние находится между шагами
- [x] **⚠️ `getFrom(u).ID` — тоже сырой telegram id, и он используется как доменный номер в 17 местах.** `getFrom` (`internal/bot/bot.go:176`) возвращает `From` прямо из апдейта. Сегодня это безобидно, потому что `_id == telegram id`. После Task 12 у google-первого пользователя с привязанным telegram `_id ≥ 10¹²`, а `getFrom(u).ID` ~10⁹ — и всё перечисленное ниже начинает работать по чужому/несуществующему номеру. **Самый тяжёлый случай — `operation_screen.go:2135`: `donor := getFrom(u)` кладётся в `Operation.Donor` (`:2138`) и уходит в `CreateOperation`, то есть сырой telegram id записывается в документ комнаты как участник расчёта.** Логика долгов при этом не меняется (инвариант 3 соблюдён формально), но её входные данные становятся невалидными — это порча данных, а не косметика
- [x] заменить `getFrom(u)` на канонического `u.User` во всех местах, где значение используется как номер Splitty, — список полный, свериться с ним построчно:
  - `all_room.go:41` (`userId := getFrom(u).ID`), `:112`, `:199` (`FindRoomsByUserId` / `FindArchivedRoomsByUserId`) — иначе пустой список комнат
  - `debt_screen.go:98`, `:189` (`userId := getFrom(u).ID`) — чужие или пустые долги
  - `setting_screen.go:55` (`isArchived(room, getFrom(u))`), `:184`, `:188` (`ArchiveRoom`/`UnArchiveRoom`) — архивация становится молчаливым no-op
  - `statistic_screen.go:60`, `:70` (`GetUserCostsSum`, `GetUserDebtAndLendSum`) — статистика по чужому номеру
  - `room_screen.go:126` (`containsUserId(room.Members, getFrom(u).ID)`) — участнику показывается «вы не участник»
  - `operation_screen.go:1289` (`opn.Donor.ID != getFrom(u).ID`), `:1304` (`recipientsWithSum.User.ID != getFrom(u).ID`) — проверка «автор ли это» отвечает неверно
  - `operation_screen.go:2135` (`donor := getFrom(u)`) — см. пункт выше, писать `u.User`
- [x] **`room_creating.go:94` — отдельный случай того же класса**: `Members: &[]api.User{u.Message.From}` берёт пользователя напрямую из сообщения, минуя `getFrom`. Заменить на `*u.User`, иначе создатель комнаты попадает в снимок с сырым telegram id, и санитайз из Task 3 сохранит неправильный `_id`
- [x] **где `getFrom` остаётся законным**: только там, где нужен именно telegram id входящего апдейта (см. `tg_helper.go:129`, исключение зафиксировано в Task 7). Пометить такие места комментарием
- [x] проверяемый чекбокс: `grep -rn "getFrom(u" internal/bot/ | grep -v _test` — в остатке допустимы только объявление функции (`bot.go:176`), места из Task 7, где `getFrom` передаётся в `userLink` для рендера, и явно закомментированные исключения. Все остальные строки из списка выше должны исчезнуть
- [x] написать тест: пользователь с `_id ≥ 10¹²` и `telegram_id` ~10⁹ открывает список комнат, экран комнаты и долги — везде видит СВОИ данные; созданная им операция возврата долга содержит в `donor.id` номер Splitty, а не telegram id
- [x] **расширить интерфейс `UserService`** на `internal/events/telegram.go:20` — добавить `UpsertTelegramUser`; затем заменить вызов на `:83`
- [x] в `handleAuthTelegram` (`auth.go:132`) резолвить через `telegram_id`; **`sub` токена = `_id` НАЙДЕННОГО пользователя, а не `req.Id`** — иначе у google-аккаунта с привязанным telegram создастся второй профиль
- [x] `handleAuthCode` (:196) не трогать: `lc.UserId` — уже номер Splitty, поиск по `_id` корректен
- [x] `handleAuthDev` (:279) оставить как есть; **дописать комментарий**, что dev-пользователи создаются без `telegram_id`, поэтому после Task 7 им не идут telegram-уведомления, а после Task 8 аватар отдаёт 404 — это ожидаемо, не баг
- [x] дописать `UpsertTelegramUser` в `fakeUserRepo`
- [x] написать тесты: новый telegram-пользователь создаётся с `_id == telegram_id`; **при `_id`, занятом ДРУГИМ пользователем, выдаётся синтетический номер, а `telegram_id` остаётся telegram-овским**; **гонка: два конкурентных вызова с одним `tgID` дают ОДНОГО пользователя, оба возврата успешны и с одинаковым `_id`**; существующий находится по `telegram_id` и не дублируется; `user_lang` ставится при создании и не затирается при повторном апдейте; смена username обновляется; вход по telegram в аккаунт с другим `_id` выдаёт токен на найденный `_id`
- [x] обновить существующие тесты `internal/rest/auth_test.go`
- [x] `go test ./internal/...` — зелёные перед Task 7

### Task 7: Отправка в Telegram только по TelegramID

**Files:**
- Modify: `internal/bot/notifier.go` (3 места)
- Modify: `internal/bot/operation_screen.go` (9 мест)
- Modify: `internal/bot/setting_screen.go` (1 место)
- Modify: `internal/bot/report_screen.go` (1 место)
- Modify: `internal/bot/tg_helper.go` (`userLink` :222; **`:129` — исключение, не трогать**)
- Create: `internal/bot/notifier_no_telegram_test.go`
- ➕ Modify: `cmd/splitty/wire_gen.go` — `ViewDonorOperation` и `DeleteDonorOperation` получили `us UserService` (резолвер канонических участников), поэтому конструкторы и порядок объявления `userService` в графе пришлось поправить
- ➕ Modify: `internal/bot/notifier_test.go`, `internal/bot/operation_items_test.go` — фикстуры теперь задают `TelegramID`

- [x] завести в `internal/bot` хелпер `telegramChatID(u *api.User) (int64, bool)` — возвращает `*u.TelegramID` и `true` только при `u.HasTelegram()`
- [x] заменить отправки в `notifier.go` (:134, :151, :248) на путь через `telegramChatID` с пропуском получателя при `false`
- [x] **chat id брать из КАНОНИЧЕСКОГО документа**, а не из встроенного снимка: в снимках `telegram_id` не будет никогда (они писались до этого плана и санитайзятся после Task 3). В `notifier.go` для этого уже есть `n.uf.FindById` (:63, :92); в `operation_screen.go` — поле `us UserService` с `FindById` (см. `internal/bot/tg_helper.go:17`). **Без этого telegram-уведомления перестанут ходить вообще**
- [x] `allowsTelegram` (`notifier.go:87`) дополнить: `false`, если у канонического пользователя нет telegram
- [x] `pushToUser` (:59) **не трогать** — push обязан работать для google-пользователей
- [x] `userLink` (`tg_helper.go:222`): без telegram возвращать экранированное имя без `<a href>`
- [x] **⚠️ главная ловушка задачи: `userLink` тоже обязан получать КАНОНИЧЕСКОГО пользователя.** Почти все его вызовы получают объекты, у которых `TelegramID` будет `nil` всегда: (а) встроенные снимки (`op.Donor`, `recipientsWithSum[].User`) — старые писались до плана, новые санитайзятся в Task 3; (б) автор через `getFrom` (`internal/bot/bot.go:176`) — это **сырой** пользователь из апдейта, а не канонический `upd.User`. Если этого не сделать, после задачи **все живые telegram-пользователи потеряют кликабельные упоминания** в уведомлениях и экранах бота
- [x] для автора использовать `upd.User` (канонический после Task 6), а не `getFrom`. **Task 6 уже вычистил `getFrom` из мест, где он служил доменным id** — здесь остаются только вызовы `userLink(getFrom(u))` (`operation_screen.go:1295`, `:1313`, `:1344`), их тоже перевести на `u.User`
- [x] пройти 9 мест в `operation_screen.go`, по одному в `setting_screen.go` (:575) и `report_screen.go` (:85, суперюзеры из `SUPER_USER`, резолвятся через `FindByUsername`)
- [x] **резолвер доступен не везде**: из 9 мест в `operation_screen.go` только 3 живут в структурах с полем `us UserService` (`OperationAdded`, `AddRecepientOperation`); места около :1393-1459 — свободные функции `notificationWhenUpdateOperation`/`buildUpdateOperationMessages`, куда резолвер придётся протащить параметром, изменив и вызов из `notifier.go:204`
- [x] **`tg_helper.go:129` (`int64(update.CallbackQuery.From.ID)`) не трогать** — это telegram id из самого входящего апдейта, он корректен по определению. Финальная проверка: `grep -rn "int64(.*\.ID)" --include="*.go" internal/ | grep -v _test` должен оставить **ровно одно** совпадение — эту строку
- [x] написать тесты: пользователь без telegram не получает telegram-сообщение, но получает push; с telegram — получает оба; chat id взят из канонического документа, а не из снимка (снимок содержит другой/пустой `telegram_id`); `userLink` без telegram не содержит `href`
- [x] **обязательный тест-антирегрессия**: у пользователя С telegram `userLink` содержит `tg://user?id=<telegram_id>` — и это проверяется на пути, где пользователь пришёл **из снимка комнаты**. Без такого теста «зелёный» прогон будет означать успешно сломанные упоминания
- [x] `go test ./internal/...` — зелёные перед Task 8

**Как сделано (для следующих задач):**
- в `tg_helper.go` появился `canonicalUsers` — резолвер канонических документов с кешем на время одной отрисовки; методы `link` (упоминание) и `chatID` (адрес отправки). Все пути уведомлений в `internal/bot` ходят через него, а `userLink` остался чистой функцией и требует УЖЕ канонического пользователя
- `NotifyOperationCreated` перестроен так, что push больше не гасится настройками telegram-канала (раньше `!allowsTelegram` делал `continue` до `pushToUser`, и google-пользователь не получил бы ничего). `NotificationSent` помечается при уведомлении по любому каналу
- ⚠️ **осталось вне объёма Task 7**: `all_room.go:280` (`createRoomInfoText`, список участников комнаты) и `statistic_screen.go:158` (история долгов) рендерят `userLink` по снимкам и теперь показывают имя без ссылки. Это списочные рендеры: поштучный резолв дал бы N запросов на экран, правильное решение — батч `FindByIds` в `UserRepository`. Вынесено в Task 7a

### Task 7a: ➕ Кликабельные упоминания в списочных экранах бота

**Files:**
- ~~Modify: `internal/repository/repository.go`, `internal/rest/fakes_test.go`~~ — `FindByIds` там уже был (добавлен под AI-парсинг чеков, `internal/rest/parse_handler.go:207`), правки не понадобились
- Modify: `internal/bot/all_room.go`, `internal/bot/room_screen.go`, `internal/bot/statistic_screen.go`
- ➕ Modify: `internal/bot/tg_helper.go` (батч-прогрев `canonicalUsers.warm` + `FindByIds` в `UserService`), `internal/bot/notifier.go` (`FindByIds` в `UserFinder`)
- ➕ Modify: `cmd/splitty/wire_gen.go` — четыре конструктора получили `userService`
- ➕ Create: `internal/bot/list_mentions_test.go`; Modify: `internal/bot/canonical_id_test.go`, `internal/bot/notifier_test.go` (фейки дополнены `FindByIds`)

- [x] добавить в `UserRepository` батч-метод `FindByIds(ctx, ids []int) ([]api.User, error)` (+ реализация, + фейк — см. «Правило расширения интерфейсов») — **уже существовал**: `repository.go:836` (`$in` по `_id`) и `fakeUserRepo.FindByIds` (`fakes_test.go:254`). Заводить второй батч не стали
- [x] прокинуть резолвер в `createRoomInfoText` (`all_room.go:261`, вызовы `all_room.go:69`, `room_screen.go:73,141`) и в `statistic_screen.go:158`, чтобы упоминания собирались по каноническим документам одним запросом на экран
- [x] тест: участник комнаты с telegram виден в списке как `tg://user?id=<telegram_id>`, участник без telegram — простым именем
- [x] `go test ./internal/...` — зелёные

**Как сделано:**
- переиспользован резолвер Task 7: у `canonicalUsers` появился `warm(ids []int)` — один `FindByIds` на экран, результат ложится в тот же кеш, из которого читают `link`/`chatID`. Второго механизма не заводили
- `warmed` — множество id, по которым батч уже отвечал: пользователя, которого батч не нашёл (снимок пережил владельца), `get()` больше не перечитывает поштучно, иначе один «мёртвый» участник вернул бы экрану N запросов
- `createRoomInfoText` теперь принимает `*canonicalUsers` первым параметром; `AllRoomInline`, `JoinRoom`, `ViewRoom`, `ViewAllDebtOperations` получили поле `us UserService`
- в inline-выдаче `AllRoomInline` резолвер один на все комнаты выдачи — участники пересекаются, кеш переживает цикл (тест `TestAllRoomInlineReusesResolverAcrossRooms`: 2 комнаты → 1 запрос)
- ⚠️ **обнаружено, вне объёма задачи**: `all_room.go:330` (`findRoomsByUpdate` ищет комнаты по `u.InlineQuery.From.ID`) и `room_screen.go:39` (`JoinToRoom(ctx, u.CallbackQuery.From, …)` кладёт в снимок комнаты сырого пользователя из апдейта) — тот же класс, что чинил Task 6, но в его построчный список они не попали. После Task 12 у google-первого пользователя с привязанным telegram это даст пустую inline-выдачу и участника с чужим `_id` в снимке. Вынесено в Task 7b

### Task 7b: ➕ Канонический id в inline-поиске и JoinToRoom

**Files:**
- Modify: `internal/bot/all_room.go` (`findRoomsByUpdate` :329)
- Modify: `internal/bot/room_screen.go` (`JoinRoom.OnMessage` :43)
- Modify: `internal/bot/canonical_id_test.go`, `internal/bot/list_mentions_test.go` (фейки запоминают, чем именно их спросили)

- [x] `all_room.go:330` — `FindRoomsByLikeName(ctx, u.InlineQuery.From.ID, …)` берёт сырой telegram id: комнаты хранят канонические номера участников, поэтому после Task 12 у google-первого пользователя inline-выдача будет ПУСТОЙ. Заменить на `u.User.ID`, той же правкой поправить лог на `:332`
- [x] `room_screen.go:43` — `JoinToRoom(ctx, u.CallbackQuery.From, roomId)` кладёт во встроенный снимок `room.users[]` сырого пользователя из апдейта. Это тот же класс порчи данных, что `Operation.Donor` в Task 6: участник с чужим `_id` навсегда оседает в документе комнаты и ломает расчёт долгов. Заменить на `*u.User`, пометить комментарием почему
- [x] проверить ОСТАЛЬНЫЕ прямые чтения `From` мимо `getFrom`: `grep -rn "\.Message\.From\|\.CallbackQuery\.From\|\.InlineQuery\.From" --include="*.go" internal/ | grep -v _test`. Законны и не трогаются: тело самого `getFrom` (`bot.go:189-194`), `tg_helper.go:137` (`getChatID` — нужен именно telegram chat id входящего апдейта, исключение зафиксировано в Task 7) и всё в `internal/events/telegram.go` (ингресс telegram-личности: там `From` — единственный источник telegram id). После правок доменных использований в остатке остаться не должно
- [x] тест на inline-поиск: комнаты ищутся номером Splitty, а не telegram id (фейк `RoomService` запоминает переданный `userId`)
- [x] тест на join: в `JoinToRoom` уходит пользователь с `_id` = номер Splitty, а не сырой telegram id
- [x] `go test ./internal/...` — зелёные

**Как сделано:**
- обе правки однострочные (`u.User.ID` и `*u.User`), санитайз снимка при join уже делает репозиторий (`repository.go:250`, `u.Snapshot()`) — трогать его не понадобилось
- аудит прямых чтений `From`: в остатке только тело `getFrom` (`bot.go:189-194`), `getChatID` (`tg_helper.go:137`) и `internal/events/telegram.go` — все три законны и уже помечены комментариями. Доменных использований `From` мимо `getFrom` в `internal/bot` больше нет
- фейки `inlineRoomService`/`joiningRoomService` из Task 7a дополнены записью аргументов (`askedUserIDs`, `joined`) — новых заглушек не заводили. Оба теста проверены на «падают до фикса»

### Task 8: Аватары через telegram_id

**Files:**
- Modify: `internal/rest/avatar.go`
- Modify: `internal/rest/avatar_test.go`

- [x] в `handleGetUserAvatar` (`avatar.go:136`) резолвить пользователя через `s.userRepo.FindById` и брать telegram id из `TelegramID`
- [x] у пользователя без telegram — `404 not_found` (клиенты рисуют инициалы), не 500 и не 503
- [x] ключ кеша (`avatar.go:155`) оставить по номеру Splitty — не путается при привязке/отвязке
- [x] сохранить проверку доступа (`avatar.go:145-149`)
- [x] написать тесты: 404 без telegram; успешный путь шлёт в Telegram `telegram_id`, а не `_id`; отказ в доступе к чужому аватару без общей комнаты
- [x] `go test ./internal/...` — зелёные перед Task 9

**Как сделано:**
- порядок в хендлере: проверка доступа → `TgToken` → кеш по номеру Splitty → `FindById` → `HasTelegram()`. Резолв стоит ПОСЛЕ кеша: попадание в кеш не требует telegram id вовсе, и лишнего запроса в mongo на каждый экран со списком аватаров не появляется
- `mongo.ErrNoDocuments` от `FindById` → `404 not_found` (снимок участника в комнате может пережить владельца), прочие ошибки → 500
- «нет telegram» намеренно НЕ кешируется: это один запрос в mongo, а не два в telegram, и после привязки telegram (Task 12) аватар обязан появиться сразу, а не через сутки
- фикстуры аватарных тестов получили хелперы `withTelegram`/`tgIDOf`; telegram id намеренно НЕ равен `_id`, иначе тест не отличил бы одно от другого, а `fakeTelegram` теперь сверяет пришедший `user_id` с ожидаемым telegram id

### Task 9: Верификация OIDC ID-токенов

**Files:**
- Create: `internal/oidc/verifier.go`
- Create: `internal/oidc/jwks.go`
- Create: `internal/oidc/verifier_test.go`

- [x] **не писать свой разбор JWT**: использовать уже имеющийся прямой `github.com/golang-jwt/jwt/v5` (`go.mod:11`) — `jwt.ParseWithClaims` c `jwt.WithValidMethods([]string{"RS256"})`, `jwt.WithIssuer`, `jwt.WithAudience`, `jwt.WithExpirationRequired`, `jwt.WithLeeway(5*time.Minute)`. Своими руками — только загрузка и парсинг JWKS
- [x] **не подключать `github.com/MicahParks/keyfunc v1.9.0`** (`go.mod:31`, indirect): он написан под `jwt/v4`, а мы на `v5` — несовместимо. Свой `jwt.Keyfunc` поверх кеша
- [x] `type Claims struct { Subject, Email, Nonce string }` + `jwt.RegisteredClaims`
- [x] `type Verifier interface { Verify(ctx, idToken string) (*Claims, error) }` — интерфейс нужен для фейков в Tasks 10-12
- [x] `JWKSCache`: загрузка по URL, парсинг **только RSA** (`n`/`e`), выбор по `kid`. **ES256 и EC-ключи не поддерживать** — ни Google, ни Apple их не используют (YAGNI)
- [x] обновление кеша по TTL **и** по промаху `kid`, но не чаще раза в 5 минут — иначе мусорный `kid` в токене становится DoS на провайдера
- [x] `http.Client` с таймаутом 10 с и `io.LimitReader` на 1 МБ — внешнему хосту не доверяем
- [x] конструкторы `NewGoogle(clientIDs []string)` (JWKS `https://www.googleapis.com/oauth2/v3/certs`, `iss` ∈ {`https://accounts.google.com`, `accounts.google.com`}) и `NewApple(clientIDs []string)` (JWKS `https://appleid.apple.com/auth/keys`, `iss` = `https://appleid.apple.com`)
- [x] написать тесты на локально сгенерированных RSA-ключах (`crypto/rsa`, без сети): валидный токен проходит; истёкший — ошибка; чужой `aud` — ошибка; чужой `iss` — ошибка; `alg: none` отвергается; HS256 отвергается; неизвестный `kid` триггерит **один** refresh и затем ошибку; повторный промах `kid` в пределах 5 минут в сеть не ходит
- [x] `go test ./internal/...` — зелёные перед Task 10

**Как сделано (для следующих задач):**
- `Verifier` — интерфейс, реализация `providerVerifier` (неэкспортируемая); в Tasks 10-12 подставляется фейк, `nil` в конфиге = провайдер выключен
- `Claims` встраивает `jwt.RegisteredClaims`, поэтому `sub` читается как `claims.Subject`; добавлены `Email` и `Nonce` (последний нужен Apple-потоку Task 11)
- [decision] `jwt.WithAudience(ids...)` в v5.3.1 имеет семантику ИЛИ (`WithAllAudiences` — это И), поэтому несколько client id проверяются штатной опцией. А вот `jwt.WithIssuer` принимает ровно одно значение, и у Google издателей два — опция ставится только когда издатель один (Apple), а проверка вхождения `iss` в список всё равно делается своей после разбора. Ветвление задокументировано комментарием у поля `issuers`
- [decision] троттлинг обновления считается от **последнего похода** за JWKS, успешного или нет: иначе лежащий провайдер получал бы запрос на каждый вход
- [decision] если TTL истёк, а обновление не удалось — работаем на устаревших ключах вместо отказа во входе всем (тест `TestStaleKeysUsedWhenRefreshFails`). Ошибка возвращается только когда кеш пуст вовсе
- [decision] мьютекс кеша держится и на время HTTP-запроса: параллельные входы ждут один поход за ключами, а не устраивают стадо запросов к провайдеру
- [decision] ключи короче 2048 бит, `use != "sig"`, `alg != RS256` и не-RSA записи из JWKS отбрасываются молча — одна негодная запись в наборе не должна ронять весь набор
- проверка `alg` (`WithValidMethods`) в jwt/v5 срабатывает **до** вызова `keyfunc`, поэтому `alg:none`/HS256-подделки не вызывают похода в сеть — это зафиксировано отдельным тестом (`TestVerifyRejectsAlgNoneAndHS256` считает обращения к JWKS)
- часы кеша (`jwksCache.now`) вынесены в поле — тесты двигают время, `time.Sleep` в тестах нет
- новых зависимостей не добавлено: `go.mod`/`go.sum` не изменились

### Task 10: POST /api/v1/auth/google

**Files:**
- Modify: `internal/rest/auth.go`
- Modify: `internal/rest/server.go`
- Modify: `cmd/splitty/config.go`
- Modify: `cmd/splitty/main.go`
- Modify: `internal/rest/auth_test.go`
- ➕ Modify: `internal/rest/throttle.go` (константа `oauthPerIPPerMin`), `internal/rest/fakes_test.go` (фейк verifier + честный `errDuplicateKey`), `internal/oidc/verifier.go` (claim `name`)
- ➕ Create: `cmd/splitty/config_test.go` (тест на фильтрацию пустых client id)

- [x] добавить в `cmd/splitty/config.go` поле `GoogleClientIds []string` (`env:"GOOGLE_CLIENT_IDS" envSeparator:":" envDefault:""`)
- [x] **отфильтровать пустые элементы в `main.go`** перед решением «verifier включён»: `envDefault:""` со сплитом даёт `[""]`, а не пустой срез — иначе google-вход считался бы сконфигурированным с невозможным audience и отвечал 401 вместо честного 503
- [x] добавить в `rest.Config` (`server.go:32`) поле `GoogleVerifier oidc.Verifier` (nil = выключено), собрать в `main.go`
- [x] реализовать `handleAuthGoogle`: тело `{"idToken":"…"}`; пусто → `400 validation`; verifier nil → `503 unavailable`; ошибка верификации → `401 unauthorized` **без деталей причины**
- [x] поиск по `google_sub` → нашли: выдать токен; не нашли: создать через `CreateIdentityUser` с `_id` из аллокатора
- [x] **retry при duplicate key начинается с повторного `FindByGoogleSub`**: дубль означает гонку двух первых входов одного человека (номер из `$inc` атомарен и не конфликтует), поэтому проигравший должен подобрать созданного победителем. Слепой retry «взять новый номер и вставить снова» упрётся в unique-индекс по `google_sub` все 3 раза и отдаст 500
- [x] **не склеивать аккаунты по email автоматически** — email не доверенный идентификатор (Apple relay, смена почты); только явная привязка (Task 12)
- [x] зарегистрировать `mux.HandleFunc("POST /api/v1/auth/google", s.handleAuthGoogle)` рядом с `server.go:152-154`
- [x] применить per-IP троттлинг **с собственным префиксом ключа**: `s.authThrottle.allow("google:"+clientIP(r), …)`. Использовать буквально `"ip:"+clientIP(r)` как у `/auth/code` (`auth.go:199`) **нельзя** — общий ключ означал бы, что вход через Google выжигает бюджет входа по коду с того же IP и наоборот
- [x] написать тесты (фейковый verifier): новый пользователь получает `_id ≥ 10¹²` и не имеет `telegram_id`; повторный вход находит того же по `google_sub`, дубля нет; невалидный токен → 401; нет конфига → 503; пустое тело → 400; троттлинг срабатывает
- [x] `go test ./internal/...` — зелёные перед Task 11

**Как сделано (для следующих задач):**
- `handleAuthGoogle` (`internal/rest/auth.go`) — порядок: nil-verifier → 503, троттлинг, разбор тела, верификация, резолв. Проверка конфига стоит ДО троттлинга: выключенная фича обязана отвечать 503 независимо от того, сколько раз клиент постучался
- резолв вынесен в `resolveGoogleUser` — цикл на `identityAuthAttempts = 3`, начинающийся с `FindByGoogleSub` на КАЖДОЙ итерации (тест `TestAuthGoogleDuplicateKeyPicksUpWinner` проверяет, что проигравший гонку подбирает документ победителя и второй вставки не делает)
- [decision] `oauthPerIPPerMin = 20` — свой лимит, а не `authCodePerIPPerMin`: перебирать тут нечего (подпись провайдера не подделать), лимит про стоимость разбора и походов за JWKS, а за одним адресом сидит NAT. Ключ `"google:"+clientIP(r)`; тест проверяет, что после исчерпания бюджета Google вход по коду с того же адреса всё ещё отвечает `invalid_code`, а не `rate_limited`
- [decision] ➕ в `oidc.Claims` добавлен claim `name`: клиент Google (Task 16) шлёт только `idToken`, имя больше взять неоткуда, а пользователь без `display_name` виден в комнатах пустой строкой. Пишется ТОЛЬКО при создании — переименовывать существующего провайдер права не имеет. Email в `display_name` не подставляется: это утекло бы в снимки комнат (`Snapshot()` чистит `email`, но не `display_name`)
- [deviation] `errDuplicateKey` в `fakes_test.go` был `errors.New(...)`, а боевой код узнаёт дубликат через `repository.IsDuplicateKey` (разбор типов драйвера) — на плоской ошибке retry молча превращался в 500. Заменён на `mongo.WriteException{Code: 11000}`; фейк `UpsertTelegramUser` от этого не пострадал
- [decision] `initGoogleVerifier` в `main.go` возвращает интерфейсный `nil` (никакого typed-nil): отдельного флага «включено» не нужно, `cfg.GoogleVerifier == nil` — единственный признак
- Google-пользователь создаётся с `google_sub`, `email`, `display_name` и БЕЗ `telegram_id`. Следствия те же, что у dev-входа: telegram-уведомления ему не идут, `/users/{id}/avatar` отдаёт 404 — клиенты рисуют инициалы

### Task 11: POST /api/v1/auth/apple

**Files:**
- Modify: `internal/rest/auth.go`
- Modify: `internal/rest/server.go`
- Modify: `cmd/splitty/config.go`
- Modify: `cmd/splitty/main.go`
- Modify: `internal/rest/auth_test.go`
- ➕ Create: `internal/oidc/apple_client_secret.go`, `internal/oidc/apple_client_secret_test.go`
- ➕ Modify: `internal/repository/repository.go` (`UpdateAppleProfile`), `internal/rest/fakes_test.go` (фейк репозитория + фейк обмена токенов), `internal/repository/user_identity_test.go`, `cmd/splitty/config_test.go`

- [x] добавить `AppleClientIds []string` (`env:"APPLE_CLIENT_IDS" envSeparator:":"`) и `AppleVerifier` по образцу Task 10, включая фильтрацию пустых элементов
- [x] троттлинг с собственным префиксом `"apple:"+clientIP(r)` (см. обоснование в Task 10)
- [x] создание — через `CreateIdentityUser`, retry при duplicate key начинается с повторного `FindByAppleSub` (см. Task 10)
- [x] тело запроса: `{"idToken":"…", "displayName":"…", "nonce":"…", "authorizationCode":"…"}` — Apple отдаёт имя не в токене, а отдельно на клиенте; `authorizationCode` нужен для отзыва при удалении аккаунта (пункт ниже)
- [x] **⚠️ отзыв токенов Apple обязателен по Guideline 5.1.1(v)**: приложение с Sign in with Apple, предлагающее удаление аккаунта, обязано вызывать `POST https://appleid.apple.com/auth/revoke`. Для этого нужен refresh token, а получить его можно **только** обменом `authorizationCode` в момент входа — позже он протухает (валиден ~5 минут и одноразовый). Значит обмен делается здесь, а не в Task 13
- [x] добавить в `cmd/splitty/config.go`: `AppleTeamId` (`env:"APPLE_TEAM_ID"`), `AppleKeyId` (`env:"APPLE_KEY_ID"`), `ApplePrivateKey` (`env:"APPLE_PRIVATE_KEY"` — содержимое `.p8`, **не путь**; ключ в git не коммитить)
- [x] создать `internal/oidc/apple_client_secret.go`: `client_secret` для Apple — это ES256-JWT (`iss`=TeamID, `sub`=bundle id, `aud`=`https://appleid.apple.com`, `kid`=KeyID, TTL ≤ 6 месяцев), подписанный ключом из `.p8`. Использовать `jwt/v5` + `crypto/ecdsa`; ключ парсить `x509.ParsePKCS8PrivateKey`
- [x] при первом входе обменять `authorizationCode` на refresh token (`POST https://appleid.apple.com/auth/token`, `grant_type=authorization_code`) и сохранить в `AppleRefreshToken`. Обмен — **best-effort**: сеть недоступна или Apple ответил ошибкой → залогировать Warn и **всё равно выдать токен входа**, вход не должен падать из-за этого
- [x] при пустом `ApplePrivateKey` обмен пропускать целиком, залогировав Info один раз на старте — локальная разработка не должна требовать ключа Apple
- [x] **проверка nonce обязательна**: клиент шлёт сырой nonce, в токене лежит его SHA256; сравнивать `claims.Nonce == hex(sha256(req.Nonce))` constant-time; несовпадение → `401`. Без этой проверки nonce на клиенте (Task 15) — бутафория
- [x] **Apple присылает email только при ПЕРВОМ входе**: при создании сохранить `email`; при последующих входах, если в базе email уже есть, а в токене пусто — **не затирать**
- [x] то же для `display_name` — заполнять только если пусто
- [x] комментарием отметить: адрес вида `@privaterelay.appleid.com` валиден, но не пригоден для склейки аккаунтов
- [x] зарегистрировать `POST /api/v1/auth/apple`, повесить тот же троттлинг
- [x] написать тесты: первый вход сохраняет email и имя; повторный с пустым email не затирает; повторный с пустым именем не затирает; поиск по `apple_sub` не плодит дублей; **несовпадение nonce → 401**; невалидный токен → 401; нет конфига → 503; **обмен `authorizationCode` сохраняет `apple_refresh_token`**; **ошибка обмена не валит вход** (токен всё равно выдан, поле пустое); генерация `client_secret` даёт валидный ES256-JWT с ожидаемыми `iss`/`sub`/`aud`/`kid` (на локально сгенерированном ключе, без сети)
- [x] `go test ./internal/...` — зелёные перед Task 12

**Как сделано (для следующих задач):**
- `handleAuthApple` (`internal/rest/auth.go`) повторяет порядок Google: nil-verifier → 503, троттлинг `"apple:"+clientIP`, разбор тела, верификация, **проверка nonce**, обмен кода, резолв. Пустой `nonce` в теле — `400 validation`: проверять было бы нечего, а молча пропустить значит вернуть бутафорию
- [decision] обмен `authorizationCode` делается на КАЖДОМ входе, а не только первом: Apple выдаёт новый код при каждом входе и (важнее) старый refresh token может быть отозван пользователем в настройках Apple ID. Тест `TestAuthAppleStoresRefreshToken` проверяет, что повторный вход перезаписывает токен
- [decision] обмен стоит ДО резолва пользователя, чтобы refresh token попал в документ сразу при вставке (`CreateIdentityUser`), а не вторым запросом
- [deviation] ➕ в `UserRepository` добавлен `UpdateAppleProfile(ctx, userId, email, displayName, refreshToken)`: записать `email`/`apple_refresh_token` существующему пользователю было физически нечем — `UpsertUser` пишет частичный `$set` из четырёх полей и личности не касается. Фильтр `{_id, deleted_at: {$exists:false}}`, без upsert (правило из Task 12 применено заранее: гонка «вход ↔ `DELETE /me`» иначе дописала бы живой refresh token в tombstone). Фейк в `internal/rest/fakes_test.go` дополнен тем же коммитом
- [decision] «не затирать» реализовано в ДВА слоя: хендлер (`fillAppleProfile`) пишет непустое значение только в ПУСТОЕ поле — провайдер вправе сообщить неизвестное нам имя, но не вправе переименовать человека, назвавшегося в Splitty сам; репозиторий вдобавок игнорирует пустые аргументы
- [decision] ошибка `UpdateAppleProfile` НЕ валит вход (Warn + вход с несохранённым дозаполнением): аккаунт уже найден, а дозаполнение профиля и машинерия отзыва — не повод отказать во входе. Тот же принцип, что у best-effort обмена
- [decision] TTL `client_secret` — 5 минут вместо разрешённых Apple шести месяцев: секрет собирается на каждый запрос (ES256 стоит микросекунды), кеш и ротация не нужны, а цена утечки из логов ниже
- [decision] `client_id` для `client_secret` — ПЕРВЫЙ элемент `APPLE_CLIENT_IDS`: секрет подписывается ровно под один `sub` (bundle id приложения). Services ID веб-потока, если появится, потребует отдельного секрета
- [decision] `APPLE_PRIVATE_KEY` нормализуется: экранированные `\n` превращаются в переводы строк — PEM многострочный, а в env его кладут одной строкой (docker-compose, секреты CI), и без нормализации обмен молча выключался бы при формально заданном ключе
- [decision] негодный ключ или отсутствие `APPLE_CLIENT_IDS` при заданном ключе НЕ роняют старт: `initAppleTokens` возвращает nil с Warn. Класть весь сервис из-за необязательной интеграции нельзя; вход через Apple при этом продолжает работать
- отзыв токенов (`POST /auth/revoke`) в этой задаче намеренно НЕ реализован — он нужен Task 13, а `AppleTokenClient.ClientSecret()` экспортирован именно для него

### Task 12: Привязка и отвязка способов входа

**Files:**
- Create: `internal/rest/identity.go`
- Modify: `internal/rest/server.go`
- Modify: `internal/rest/dto.go`
- Modify: `internal/repository/repository.go`
- Modify: `internal/rest/fakes_test.go`
- Create: `internal/rest/identity_test.go`
- ➕ Modify: `cmd/splitty/main.go` — проводка `chat_state` в REST (`server.SetChatStates`), нужна отвязке telegram
- ➕ Modify: `internal/repository/user_identity_test.go` — интеграционные тесты `SetIdentity`/`ClearIdentity` и сценария «отвязал telegram → апдейт бота»

- [x] новый файл `identity.go` — рядом с `auth.go`, где живут `checkTelegramHash` и работа с личностями (не в `handlers.go`)
- [x] добавить в `UserRepository` методы `SetIdentity(ctx, userId int, provider string, value any) error` и `ClearIdentity(ctx, userId int, provider string) error`; дописать их в `fakeUserRepo`
- [x] **оба метода обязаны фильтровать по `{_id: …, deleted_at: {$exists: false}}` и НЕ использовать upsert.** Гонка: медленный `/me/link/google` проходит middleware → параллельно приходит `DELETE /me`, ставит tombstone и вычищает `google_sub` → link дописывает `google_sub` обратно **на tombstone**. Поиск по личностям удалённых исключает, но unique sparse индекс значение уже занял — человек не сможет зарегистрироваться заново тем же Google-аккаунтом, а создание упадёт по duplicate key
- [x] то же правило распространить на профильные мутаторы, которые будут добавляться позже: фильтр по `_id` + отсутствие `deleted_at`, без upsert
- [x] реализовать `POST /api/v1/me/link/google` и `/me/link/apple` (тело `{"idToken":"…"}`) — верифицировать и записать `google_sub`/`apple_sub` текущему пользователю
- [x] реализовать `POST /api/v1/me/link/telegram` — тело как у `handleAuthTelegram`, с той же проверкой подписи (`auth.go:93`) и свежести `auth_date` (`auth.go:151`)
- [x] **конфликт**: личность уже привязана к ДРУГОМУ аккаунту → `409 identity_taken`, текст «Этот аккаунт уже связан с другим профилем Splitty. Войдите через него.» Слияние профилей — вне объёма (денормализованные снимки делают его отдельной большой задачей)
- [x] привязка того же провайдера к ТЕКУЩЕМУ аккаунту → `200` (идемпотентность), не ошибка
- [x] реализовать `DELETE /api/v1/me/link/{provider}`; **запретить отвязку последнего способа входа** → `409 last_identity`
- [x] **⚠️ отвязка telegram порождает второй профиль — обработать явно.** Сценарий: обычный telegram-пользователь (`_id == telegram_id == 304898122`) привязал Google и отвязал telegram, `telegram_id` вычищен. Следующий его `/start` в боте: `UpsertTelegramUser` → `FindByTelegramID` пусто → `CreateIdentityUser{_id: 304898122}` → E11000 (его же собственный документ занимает `_id`) → retry: по `telegram_id` по-прежнему пусто, `_id` занят «другим» → **выдаётся синтетический `_id ≥ 10¹²` и создаётся второй, пустой профиль**, а старые комнаты остаются на первом
- [x] выбрать поведение и записать его комментарием: (а) **рекомендуется** — отвязку telegram разрешать, но в ответе явно возвращать предупреждение, а при отвязке чистить `chat_state` пользователя (метод `DeleteByUserId` уже есть, `repository.go:712`), чтобы бот не подхватил чужое незавершённое состояние; либо (б) запретить отвязку telegram отдельным кодом `409 telegram_unlink_unsupported`, пока нет слияния профилей
- [x] написать тест на выбранное поведение: отвязали telegram → пришёл апдейт от бота → проверить число документов в `user` и то, что старые комнаты не потерялись
- [x] расширить `meDto` (`dto.go:37`) полем `linkedProviders []string`
- [x] зарегистрировать роуты под `s.auth(...)` рядом с `/me/*` (`server.go:156-161`)
- [x] написать тесты: успешная привязка каждого провайдера; повторная привязка → 200; занятая личность → 409 `identity_taken`; отвязка при двух способах — успех; отвязка последнего → 409 `last_identity`; `linkedProviders` отражает состояние; привязка telegram с протухшим `auth_date` → 401
- [x] **тест гонки**: задержать фейковый verifier, параллельно выполнить `DELETE /me`, отпустить verifier → на tombstone не появилось `google_sub`, и тот же `google_sub` успешно регистрируется заново
- [x] `go test ./internal/...` — зелёные перед Task 13

**Как сделано (для следующих задач):**
- `SetIdentity`/`ClearIdentity` живут в `repository.go` рядом с `UpdateAppleProfile` и повторяют его правило: фильтр `{_id, deleted_at: {$exists:false}}`, `UpdateOne` без upsert, `MatchedCount == 0` → `mongo.ErrNoDocuments`. Тихий no-op здесь недопустим: вызывающий обязан отличить «записали» от «записывать было некуда»
- имена провайдеров вынесены в `repository.IdentityTelegram/IdentityGoogle/IdentityApple` + `IsKnownIdentityProvider`: одни и те же строки едут в путь эндпоинта и в маппинг на поля документа, дублировать их литералами в двух пакетах нельзя
- `ClearIdentity` делает `$unset`, а не запись пустого значения — unique sparse индекс не должен видеть ни `null`, ни `""`, иначе освобождённая личность осталась бы занятой
- [decision] выбран рекомендованный планом вариант **(а)** для отвязки telegram: отвязка разрешена, ответ содержит `warning`, `chat_state` чистится (`rest.clearChatState` — по каноническому `_id` И по сырому `telegram_id`, состояния сохранялись по обоим). Вариант (б) оставил бы человека, уходящего из телеграма, привязанным к нему навсегда ради защиты от ситуации, в которую он попадает только если продолжит писать боту. Цена варианта зафиксирована тестом `TestUnlinkTelegramThenBotUpdate`: в `user` появляется второй профиль с синтетическим номером, но старые комнаты остаются на первом и открываются прежним способом входа
- [decision] `chat_state` подключён в REST узким интерфейсом `chatStateCleaner` (один метод) и опциональным сеттером `SetChatStates`, а не параметром `NewServer`: вызовы конструктора в тестах не ломаются, а Task 13, которому нужны ещё `bug_report` и `push_outbox`, получает готовый образец проводки
- [decision] `/me/link/apple` проверяет nonce **когда он есть в токене**: значение claim подписано Apple и подделать его нельзя, поэтому правило «есть claim → сверяем» защищает от replay токена нашего клиента (он всегда шлёт nonce), а токен, выпущенный без nonce вовсе, проверять просто нечем. У входа (`/auth/apple`) nonce остаётся строго обязательным
- [decision] привязка telegram НЕ обновляет `username`/`display_name`: у привязки ровно один эффект — личность, а профиль подтянет первый же апдейт бота (`UpsertTelegramUser`)
- [decision] привязка провайдера, который у аккаунта уже занят ДРУГИМ значением (сменил Google-аккаунт), перезаписывает личность: человек доказал владение новой, а старая освобождается. Запрет потребовал бы отдельного кода ошибки без выигрыша в безопасности
- отвязка непривязанного способа отвечает `200` (идемпотентность, симметрично привязке), неизвестный провайдер — `404`
- `meDto.linkedProviders` всегда массив (пустой срез, не `nil`) и отдаёт только ФАКТ привязки: сами `google_sub`/`apple_sub`/`telegram_id` наружу не уходят
- проверено, что тесты падают без защиты: временный upsert-вариант `SetIdentity` роняет `TestSetIdentitySkipsDeleted`, `TestSetIdentityDoesNotUpsert` и `TestLinkRaceWithAccountDeletion`

### Task 13: Удаление аккаунта — tombstone, чистка PII, анонимизация снимков

**Files:**
- Modify: `internal/repository/repository.go` (`AnonymizeUser`, `SoftDeleteUser`, `DeleteByUserId` для `login_code`; `MongoChatStateRepository.DeleteByUserId` уже есть на `:712`)
- Modify: `internal/repository/push_outbox.go` (удаление неотправленных записей пользователя)
- Modify: `internal/rest/auth.go` (middleware + кеш)
- Create: `internal/rest/delete_account.go`
- Modify: `internal/rest/server.go`
- Modify: `cmd/splitty/main.go` (**проводка chat_state / bug_report / push_outbox в REST — см. первый пункт**)
- Modify: `internal/rest/fakes_test.go`
- Create: `internal/rest/delete_account_test.go`
- Create: `internal/repository/anonymize_test.go`

- [x] **⚠️ начать с проводки: сейчас чистить побочные коллекции физически нечем.** `rest.Server` держит ровно три репозитория (`server.go:51-54`: `userRepo`, `roomRepo`, `loginCodeRepo`), и в `initRestServer` (`cmd/splitty/main.go:122-124`) создаются только они. `repository.NewPushOutboxRepository(db)` создаётся на `main.go:162` **внутри `if cfg.FirebaseCredentialsFile != ""`** и уходит только в `push.NewSender`. `NewChatStateRepository`/`NewBugReportRepository` в REST-графе не вызываются вовсе — только в `wire_gen.go:31,95` (граф бота). Без этого пункта три требования по PII ниже неисполнимы, и агент их молча пропустит
- [x] создать в `initRestServer` **безусловно** (push-outbox вынести из-под условия FCM — репозиторий сам по себе безвреден, условным остаётся только воркер) три репозитория, добавить поля в `rest.Server` и параметры/сеттеры в `NewServer`, дописать соответствующие фейки в `fakes_test.go`
- [x] **расширить `BugReportRepository`** (`repository.go:99-101`) — сейчас у него единственный метод `SaveBugReport`, удалять/анонимизировать нечем. Добавить `DeleteByUserId(ctx, userId int) error`
- [x] объявить в `internal/rest` **узкие** интерфейсы для этих трёх (только нужные методы), а не тянуть полные репозиторные — иначе фейки распухнут и правило расширения интерфейсов ударит по каждой будущей задаче
- [x] добавить в `RoomRepository` метод `AnonymizeUser(ctx, userId int, placeholder string) error`; дописать в `fakeRoomRepo` (`fakes_test.go:184-442`)
- [x] реализация через `UpdateMany` с `arrayFilters`: во **всех** встроенных снимках (`users[]`, `operations[].donor`, `operations[].recipients[]`, `operations[].recipients_with_sum[].user`) заменить `display_name` на placeholder, очистить `user_name` и **`$unset` полей личности** (`email`, `google_sub`, `apple_sub`, `telegram_id`) — у старых документов, записанных до Task 3, они там уже могут быть
- [x] **⚠️ `recipients` в реальных данных бывает `null` — наивные arrayFilters упадут.** REST намеренно не заполняет легаси-поле (`internal/rest/handlers.go:755` — «Легаси-поле Recipients не заполняется», `:872` — `operation.Recipients = nil`), а bson-тег **без `omitempty`** (`service_models.go:36`) → в документах лежит `recipients: null`. Обход `operations.$[].recipients.$[]` по null даёт ошибку Mongo «cannot apply array updates to non-array», весь `UpdateMany` падает и `DELETE /me` отдаёт 500 в проде
- [x] защититься: либо добавить в arrayFilters условие `{"op.recipients": {"$type": "array"}}` (и аналогично для `donor`), либо разбить на несколько независимых `UpdateMany` — по одному на путь
- [x] **ту же защиту распространить на `recipients_with_sum`**: `RecipientsWithSum []RecipientWithSum` (`service_models.go:37`) тоже объявлен **без `omitempty`**, поэтому у документов, где он не заполнялся, в базе лежит `recipients_with_sum: null` и arrayFilters по `operations.$[].recipients_with_sum.$[]` упадут ровно так же
- [x] **инвариант**: числовые id, суммы, доли и `item.shares[].user_id` не меняются
- [x] добавить в `UserRepository` метод `SoftDeleteUser(ctx, userId int) error`: выставляет `deleted_at`, делает `$unset` для `user_name`, `email`, `google_sub`, `apple_sub`, `telegram_id`, `apple_refresh_token`, `push_tokens`, `aliases`, `bank_details`, а `display_name` ставит в placeholder
- [x] **документ НЕ удаляется** — иначе пять методов с `SetUpsert(true)` (`repository.go:596,607,618,639,650`) воскресят пользователя пустым документом от первого же запроса со старым токеном
- [x] **инвалидация токена**: в middleware `auth` (`auth.go:58`) после разбора токена проверять, что пользователь существует и не удалён; при удалённом → `401`. Чтобы не ходить в базу на каждый запрос — кеш `userId → (exists, until)` с TTL 60 с и потолком размера
- [x] **обязательно: `handleDeleteMe` вычищает свой `userId` из кеша middleware.** Сам запрос `DELETE /me` этот кеш и прогревает, поэтому без явной инвалидации удалённый пользователь ещё 60 секунд ходил бы с рабочим токеном — а тест «токен удалённого → 401» падал бы. Инстанс сервера один, так что достаточно локальной чистки
- [x] обосновать комментарием: `currentUser` вызывается лишь в 7 хендлерах из ~25, поэтому без проверки в middleware токен удалённого продолжал бы открывать комнаты, операции и создание расходов ещё 90 дней
- [x] **запретить удаление демо-аккаунта ревьюеров**: если `userId == cfg.ReviewUserId` → `403` с текстом «Демонстрационный аккаунт удалить нельзя». Ревьюер Apple проверяет 5.1.1(v) именно нажатием кнопки; без запрета он поставит tombstone на демо-аккаунт, `REVIEW_LOGIN_CODE` продолжит выпускать токены (`auth.go:231-241` ищет через `FindById`, tombstone найдётся), а middleware будет их отвергать — демо-вход умрёт до ручной правки базы, и следующее ревью провалится
- [x] добавить в `LoginCodeRepository` метод `DeleteByUserId(ctx, userId int) error` и дописать его в `fakeLoginCodeRepo` (`fakes_test.go:14`) — правило расширения интерфейсов распространяется и на него
- [x] **⚠️ порядок обязан начинаться с tombstone, а не с анонимизации.** Если сначала `AnonymizeUser`, а затем `SoftDeleteUser` упадёт (транзиентная ошибка mongo, отмена контекста), `DELETE /me` вернёт 500, **аккаунт останется живым, но его имя во всех комнатах уже затёрто на «Удалённый пользователь», а username вычищен**. Снимки в комнатах не восстанавливаются из канонического документа — это необратимая порча живого аккаунта, а не «повторный вызов доделает»
- [x] **отзыв Apple-токенов — ПЕРЕД `SoftDeleteUser`**: `SoftDeleteUser` делает `$unset apple_refresh_token`, поэтому после него отзывать уже нечем. Прочитать пользователя, и если `AppleRefreshToken` не пуст — `POST https://appleid.apple.com/auth/revoke` с `client_secret` из Task 11 (`token`, `token_type_hint=refresh_token`). **Best-effort: ошибка или отсутствие сети НЕ должны блокировать удаление** — залогировать Warn и продолжить. Требование Apple Guideline 5.1.1(v); без него отказ ревью придёт ровно на той кнопке, ради которой делается эта задача
- [x] правильная последовательность: (0) отзыв Apple-токенов (best-effort, см. выше); (1) **`SoftDeleteUser`** — ставит `deleted_at`, чистит PII и личности; с этого момента middleware отвергает токен и аккаунт нельзя использовать; (2) инвалидация кеша middleware; (3) `AnonymizeUser` по комнатам; (4) чистка побочных коллекций; (5) `204`
- [x] сделать шаги 3-4 **повторяемыми**: если запрос упал после шага 1, любой повторный `DELETE /me` (или фоновая дочистка) доводит дело до конца, а состояние между шагами безопасно — аккаунт уже недоступен, а в комнатах пока видно старое имя
- [x] **чистка побочных коллекций обязательна — там реальный PII, а не технический мусор**:
  - `chat_state` — `CallbackData.ExternalData` хранит **текст расхода** пользователя (`operation_screen.go:256`: `ExternalData = purchaseText`). Удалять по каноническому `_id` и, если был, по сырому `telegram_id`. Метод `DeleteByUserId` в `MongoChatStateRepository` уже существует (`repository.go:712`)
  - `bug_report` — хранит `user_id`, `username`, `display_name` и **свободный текст** (`service_models.go:158-165`). Анонимизировать или удалять по `user_id`
  - `push_outbox` — хранит **отрендеренные `title`/`body`** (`repository/push_outbox.go:29-30`), а тексты содержат имя автора и описание расхода (`notifier.go:140,158`). Удалить неотправленные записи этого пользователя, иначе после анонимизации комнат ему всё равно доставится пуш со старым именем
- [x] **что осознанно НЕ чистится**: `button` — только id комнат/операций без PII; `ai_usage` — счётчики без содержимого. Записать это комментарием с обоснованием
- [x] placeholder — константа «Удалённый пользователь»
- [x] зарегистрировать `mux.Handle("DELETE /api/v1/me", s.auth(s.handleDeleteMe))`
- [x] написать **тест-инвариант**: комната с операциями и долгами → посчитать долги → удалить пользователя → пересчитать → результат идентичен, изменились только отображаемые имена
- [x] **в seed-данных теста анонимизации обязательны операции ОБОИХ видов** — но не так, как можно подумать: **`recipients` не заполняет ни один путь текущего кода.** Бот пишет только `RecipientsWithSum` (`operation_screen.go:355`, `:2140`), REST явно обнуляет (`handlers.go:872`), а `dto.go:374` читает `Recipients` лишь как легаси-вход для нормализации. То есть `recipients: null` — форма **всех** документов, которые пишутся сегодня, а заполненный массив бывает только в архаичных документах из ранней истории проекта
- [x] соответственно seed обязан содержать: (1) архаичный документ с `recipients: <массив>` и **отсутствующим/`null`** `recipients_with_sum`; (2) современный документ с `recipients: null` и заполненным `recipients_with_sum`. Иначе падение на null-поле не будет поймано и уедет в прод
- [x] написать тесты: повторный вызов не падает; **запрос с токеном удалённого → 401 сразу, в том же тесте, без ожидания TTL**; **`SetUserLang` со старым токеном не воскрешает пользователя**; личности освободились (можно зарегистрироваться заново с тем же `google_sub`); **`DELETE /me` под `ReviewUserId` → 403**; чужие данные не затронуты
- [x] **тест частичного отказа**: подсунуть репозиторий, у которого `AnonymizeUser` возвращает ошибку → убедиться, что аккаунт **уже недоступен** (401), а не «жив, но с затёртым именем»; повторный вызов доводит анонимизацию до конца
- [x] написать тесты чистки побочных коллекций: засеять `chat_state` с непустым `external_data`, `bug_report` и неотправленную запись `push_outbox` → после `DELETE /me` их нет
- [x] написать тесты отзыва Apple: у пользователя с `apple_refresh_token` вызывается revoke (фейковый http-клиент видит запрос с этим токеном); **ошибка revoke не мешает удалению** (аккаунт всё равно tombstone, ответ `204`); у пользователя без Apple revoke не вызывается вовсе
- [x] `go test ./internal/...` — зелёные перед Task 14

**Как сделано (для следующих задач):**
- порядок шагов зафиксирован в шапке `internal/rest/delete_account.go` и проверяется тестом `TestDeleteMePartialFailureLeavesAccountUnusable`: `AnonymizeUser` роняется одноразовой ошибкой, и тест требует, чтобы аккаунт был УЖЕ недоступен (401), а не «жив с затёртым именем». Без tombstone-первым тест падает
- [decision] `DELETE /api/v1/me` — единственный маршрут под новым `authDeleted` (пропускает удалённых). Обычный `auth` отверг бы 401 повторный вызов, а именно повторным вызовом доводится до конца запрос, упавший после шага 1. Требование «шаги 3-4 повторяемы» без этого недостижимо: довести чистку было бы некому
- [decision] `AnonymizeUser` — ЧЕТЫРЕ независимых `UpdateMany` (по одному на путь `users[]`, `donor`, `recipients[]`, `recipients_with_sum[].user`) плюс аррай-фильтр `{$type: "array"}`. Защита двойная намеренно: фильтр не даёт спуститься в `null`, а разбиение не даёт одному пути уронить остальные. Проверено, что защита несущая — наивный `operations.$[].recipients.$[r]` роняет тест ровно предсказанной планом ошибкой «Cannot apply array updates to non-array element recipients: null»
- seed `internal/repository/anonymize_test.go` держит обе формы в ОДНОЙ комнате: архаичная операция (`recipients` — массив, `recipients_with_sum` отсутствует вовсе) и современная (`recipients: null`, заполнен `recipients_with_sum`). В разных комнатах падение не воспроизводится — arrayFilters применяются в пределах документа
- [decision] `SoftDeleteUser` НЕ фильтрует по `deleted_at`: метод обязан быть идемпотентным (повторный `DELETE /me` зовёт его снова). Upsert'а при этом нет — несуществующий пользователь даёт `mongo.ErrNoDocuments`, а не пустой документ
- [decision] ошибка базы в `accountAlive` — 500, а не «считаем живым»: fail-open ничего бы не спас (хендлеры ходят в ту же mongo), а fail-closed не даёт лежащей базе стать обходом инвалидации токена
- [decision] переполнение `accountCache` сбрасывает карту целиком вместо LRU: запись стоит один поход в mongo, а LRU ради этого — лишняя машинерия
- [deviation] ➕ `login_code` чистится наравне с тремя коллекциями из плана: живой код иначе продолжал бы выпускать токен на tombstone, и вход выглядел бы сломанным вместо «аккаунта нет». `LoginCodeRepository.DeleteByUserId` план и так требовал — здесь он ещё и вызывается
- [decision] `chat_state` чистится по каноническому `_id` И по сырому `telegram_id` (состояния сохранялись по обоим, см. Task 6). На ПОВТОРНОМ вызове `telegram_id` уже вычищен шагом 1, поэтому по сырому id чистит только первый проход — осознанный остаток: класть чистку до tombstone нельзя, а хранить telegram id ради маловероятного ретрая дороже остатка
- [decision] ошибка чистки побочных коллекций — 500 (`TestDeleteMePurgeFailureReportsError`), а не тихое 204: клиент обязан повторить, а не считать, что PII убрана. Аккаунт при этом уже недоступен, так что 500 безопасен
- `oidc.AppleTokenExchanger` переименован в `oidc.AppleTokens` и получил `RevokeToken`; общий `postForm` дописывает `client_id`/`client_secret` в обе формы — обмен и отзыв отличаются только эндпоинтом и телом
- `button` и `ai_usage` НЕ чистятся: обоснование записано комментарием в `purgeUserData` (`button` — только id комнат и операций, `ai_usage` — счётчики без содержимого)
- инвариант долгов проверяется сквозным `TestDeleteMeKeepsDebtsIdentical`: `GET /rooms/{id}/debts` глазами соседа до и после удаления даёт те же суммы и те же id, меняется только `displayName` на «Удалённый пользователь»

### Task 14: Бэкенд диплинка — associated files и страница /join

**Files:**
- Create: `internal/rest/deeplink.go`
- Create: `internal/rest/deeplink_test.go`
- Modify: `internal/rest/server.go`
- Modify: `cmd/splitty/config.go`
- Modify: `cmd/splitty/main.go`

- [ ] добавить в конфиг: `PublicBaseUrl` (`env:"PUBLIC_BASE_URL"`), `IosAppId` (`env:"IOS_APP_ID"`, формат `K8922Y6R3M.com.zagir.splitty`), `AndroidPackage` (`env:"ANDROID_PACKAGE"`), `AndroidCertSha256` (`env:"ANDROID_CERT_SHA256"`)
- [ ] `GET /.well-known/apple-app-site-association`: JSON с `applinks.details[].appIDs=[IosAppId]` и `components` для пути `/join/*`; **`Content-Type: application/json`, без редиректов** — иначе iOS файл не примет
- [ ] `GET /.well-known/assetlinks.json`: `relation: ["delegate_permission/common.handle_all_urls"]`, `target.package_name`, `sha256_cert_fingerprints`
- [ ] `GET /join/{roomId}` — HTML без авторизации: **только** название группы, код моноширинно, кнопка «Скопировать», кнопка «Открыть в приложении» и ссылки на сторы. Лендинг не делать
- [ ] **не раскрывать приватное**: никаких участников, сумм, операций. Несуществующая комната → нейтральная «Приглашение не найдено», без различия «нет комнаты»/«нет доступа»
- [ ] экранировать имя комнаты (пользовательский ввод) при вставке в HTML
- [ ] **per-IP троттлинг** через `s.authThrottle.allow("join:"+clientIP(r), …)` со **своим** лимитом — публичный эндпоинт ходит в mongo на каждый вызов и служит оракулом существования комнаты по ObjectID. Префикс `join:` обязателен: страницу открывают браузеры по расшаренной ссылке, за NAT — толпой, и общий с `/auth/code` ключ выжигал бы людям вход по коду
- [ ] заголовки `X-Robots-Tag: noindex` и `Cache-Control: no-store` на `/join`
- [ ] при пустом `PublicBaseUrl` все три роута отдают `404`
- [ ] роуты регистрировать **без** `s.auth` — они публичные по определению
- [ ] написать тесты: AASA содержит корректный appID и path и отдаётся с `application/json`; assetlinks содержит package и отпечаток; `/join/<id>` рендерит код; **`<script>` в имени комнаты экранируется**; несуществующая комната → нейтральная страница; выключенная фича → 404 на всех трёх; троттлинг срабатывает
- [ ] `go test ./internal/...` — зелёные перед Task 15

### Task 15: iOS — Sign in with Apple

**Files:**
- Modify: `ios/project.yml`
- Modify: `ios/Splitty/Core/APIClient.swift`
- Modify: `ios/Splitty/Core/SessionStore.swift`
- Modify: `ios/Splitty/Features/Auth/LoginView.swift`
- Create: `ios/Splitty/Core/AppleNonce.swift`
- Create: `ios/SplittyTests/AppleNonceTests.swift`

- [ ] в `ios/project.yml` добавить в `entitlements.properties`: `com.apple.developer.applesignin: [Default]`
- [ ] создать `AppleNonce`: генерация криптостойкой случайной строки (`SecRandomCopyBytes`, алфавит из букв/цифр/`-._`) и её SHA256 в hex
- [ ] `APIClient.loginWithApple(idToken:displayName:nonce:authorizationCode:)` → `POST /api/v1/auth/apple`; **сырой nonce уходит в теле, хеш — внутри токена** (проверка на сервере в Task 11)
- [ ] **обязательно передавать `authorizationCode`** из `ASAuthorizationAppleIDCredential.authorizationCode` (это `Data`, декодировать в UTF-8 строку): сервер обменивает его на refresh token, без которого невозможен отзыв при удалении аккаунта — требование Apple 5.1.1(v), см. Tasks 11 и 13. Код одноразовый и живёт минуты, «добрать позже» нельзя
- [ ] `SessionStore.loginWithApple(...)` по образцу `loginWithCode` (`SessionStore.swift:202`)
- [ ] в `LoginView` добавить `SignInWithAppleButton` (`AuthenticationServices`) **над** карточкой «Вход через Telegram»; в `request.nonce` класть SHA256, сырой хранить для запроса
- [ ] забрать `fullName` из `ASAuthorizationAppleIDCredential` в `displayName`; при повторном входе поле пустое — это нормально
- [ ] `ASAuthorizationError.canceled` обрабатывать **тихо** — алерт показывать нельзя
- [ ] **не менять** лейблы «Telegram ID»/«Имя»/«Войти» (`LoginView.swift:195-217`) — на них завязан `DemoFlowUITests`
- [ ] написать тесты: nonce достаточной длины и из ожидаемого алфавита; два вызова дают разные значения; SHA256 детерминирован и совпадает с эталоном
- [ ] `xcodegen generate && xcodebuild … build` — успешно перед Task 16

### Task 16: iOS — вход через Google

**Files:**
- Modify: `ios/project.yml`
- Modify: `ios/Splitty/Core/APIClient.swift`
- Modify: `ios/Splitty/Core/SessionStore.swift`
- Modify: `ios/Splitty/Features/Auth/LoginView.swift`

- [ ] добавить в `ios/project.yml` в `packages:` `GoogleSignIn` (`https://github.com/google/GoogleSignIn-iOS`, from 8.0.0), подключить продукт к таргету
- [ ] **если SPM не резолвится из-за отсутствия сети** — пометить задачу `⚠️ заблокировано: нет сети` в этом файле, закоммитить остальной код и перейти к Task 17
- [ ] добавить в `info.properties` `CFBundleURLTypes` с reversed client id (плейсхолдер + `TODO`, значение из Post-Completion)
- [ ] `APIClient.loginWithGoogle(idToken:)` → `POST /api/v1/auth/google`, метод в `SessionStore`
- [ ] кнопка «Войти через Google» в `LoginView` **под** кнопкой Apple (Apple первой — требование визуального паритета), оформление существующими `.softChip`/`.primaryPill`
- [ ] отмену обрабатывать тихо
- [ ] написать/обновить тесты `SplittyTests` на новые методы `APIClient` (успех и 401) через существующую подмену транспорта
- [ ] `xcodebuild test` — зелёные перед Task 17

### Task 17: iOS — universal links и отложенный join-intent

**Files:**
- Modify: `ios/project.yml`
- Modify: `ios/Splitty/App/SplittyApp.swift`
- Modify: `ios/Splitty/App/RootView.swift`
- Modify: `ios/Splitty/Features/Groups/JoinGroupView.swift`
- Create: `ios/Splitty/Core/RoomCodeParser.swift`
- Create: `ios/Splitty/Core/PendingJoin.swift`
- Create: `ios/SplittyTests/RoomCodeParserTests.swift`

- [ ] **вынести парсер**: логика на `JoinGroupView.swift:22-34` — приватное свойство внутри View, переиспользовать нельзя. Создать `enum RoomCodeParser { static func roomId(from:) -> String? }`, перевести `JoinGroupView` на него
- [ ] парсер обязан принимать **оба** формата: старый `t.me/<bot>?start=room<hex>` и новый `https://<domain>/join/<roomId>`, плюс голый hex
- [ ] добавить в `entitlements.properties` `com.apple.developer.associated-domains: [applinks:<domain>]` (плейсхолдер + `TODO`)
- [ ] создать `PendingJoin` — хранение roomId в `UserDefaults`: `set`, `take` (читает и очищает), `clear`
- [ ] в `SplittyApp` обработать `onContinueUserActivity(NSUserActivityTypeBrowsingWeb)` и `onOpenURL`
- [ ] авторизован → выполнить join и открыть комнату; не авторизован → сохранить в `PendingJoin` и показать вход
- [ ] в `RootView` на переходе `isAuthenticated` false → true забрать `PendingJoin.take()` и выполнить join
- [ ] ошибки join по диплинку показывать человеческим текстом (комната удалена / нет доступа)
- [ ] **очищать `PendingJoin` при logout** (`SessionStore.swift:213`) — иначе следующий пользователь на устройстве вступит в чужую группу
- [ ] написать тесты парсера: новый URL; старый t.me-формат; голый hex; посторонний URL → nil; мусор → nil. И тесты `PendingJoin`: `take()` очищает; повторный `take()` → nil; `logout` очищает
- [ ] `xcodebuild test` — зелёные перед Task 18

### Task 18: Android — вход через Google (Credential Manager)

**Files:**
- Modify: `android/gradle/libs.versions.toml`
- Modify: `android/app/build.gradle.kts`
- Modify: `android/app/src/main/java/com/zagir/splitty/core/network/SplittyApi.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/core/model/Models.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/data/SplittyRepository.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/auth/LoginViewModel.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/auth/LoginScreen.kt`
- Modify: `android/app/src/main/res/values/strings.xml`

- [ ] добавить в version catalog `androidx.credentials:credentials`, `androidx.credentials:credentials-play-services-auth`, `com.google.android.libraries.identity.googleid:googleid`; подключить в `app/build.gradle.kts`
- [ ] **если Gradle не резолвит зависимости из-за отсутствия сети** — пометить `⚠️ заблокировано: нет сети`, закоммитить и перейти к Task 19
- [ ] добавить `@POST("api/v1/auth/google")` в `SplittyApi` и модель `GoogleLoginBody(idToken)` в `Models.kt`
- [ ] **добавить `GoogleLoginBody` (и любые другие новые `@Serializable`) в список `requiredSerializers`** в `app/build.gradle.kts:217-238` — иначе R8 их выкинет, задача `verifyReleaseShrinking` упадёт, а без неё релиз падал бы уже у тестера
- [ ] `SplittyRepository.loginWithGoogle(idToken)` по образцу входа по коду
- [ ] в `LoginViewModel` — запуск Credential Manager с `GetGoogleIdOption`, серверный client id из `BuildConfig` (плейсхолдер + `TODO`)
- [ ] `GetCredentialCancellationException` обрабатывать тихо
- [ ] кнопка «Войти через Google» в `LoginScreen` над блоком входа по коду; строки — в `strings.xml`
- [ ] **не добавлять Sign in with Apple на Android**: требует веб-редиректа и домена, а правило Apple 4.8 действует только на iOS
- [ ] написать unit-тесты `LoginViewModel`: успешный вход обновляет сессию; ошибка API кладёт `errorMessage`; отмена не показывает ошибку
- [ ] `./gradlew :app:testDebugUnitTest` — зелёные перед Task 19

### Task 19: Android — app links и отложенный join-intent

**Files:**
- Modify: `android/app/src/main/AndroidManifest.xml`
- Modify: `android/app/src/main/java/com/zagir/splitty/MainActivity.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/groups/GroupsListViewModel.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/groups/GroupsListScreen.kt` (второй вызов `parseRoomCode` — `:615`)
- Create: `android/app/src/main/java/com/zagir/splitty/core/session/PendingJoinStore.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/AppRoot.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/data/OfflineDataCleaner.kt`
- Create: `android/app/src/test/java/com/zagir/splitty/PendingJoinTest.kt`

- [ ] **добавить `android:launchMode="singleTop"`** к `MainActivity` в манифесте: сейчас launchMode не задан (standard), поэтому переход по app link при живой Activity создал бы **второй экземпляр** и вызвал `onCreate`, а не `onNewIntent`
- [ ] добавить intent-filter с `android:autoVerify="true"`: `VIEW` + `BROWSABLE` + `DEFAULT`, схема `https`, host — домен, `pathPrefix="/join"`
- [ ] расширить `parseRoomCode` (`ui/groups/GroupsListViewModel.kt:~187`) на новый формат `/join/<roomId>`, сохранив поддержку `start=room<hex>` и голого кода; **это общий парсер, второй не заводить**
- [ ] **зафиксировать сигнатуру перед правкой**: сейчас `parseRoomCode` возвращает non-null `String` (мусор → пустая строка), а тесты этой задачи требуют различать «не распознано». Выбрать одно: либо оставить `String` и проверять на пустоту, либо перевести на `String?` — во втором случае обязательно поправить **оба** существующих вызова: `GroupsListViewModel.kt:126` (`val roomId = parseRoomCode(codeInput)`) и `GroupsListScreen.kt:615` (`val canSubmit = parseRoomCode(code).isNotEmpty() && !isMutating`). Записать выбор комментарием
- [ ] создать `PendingJoinStore` на DataStore рядом с `SessionStore`: `set`, `take`, `clear`
- [ ] в `MainActivity` обработать `intent.data` в `onCreate` **и** `onNewIntent`
- [ ] авторизован → join и переход в `MainRoutes.room(roomId)`; не авторизован → сохранить и показать `LoginScreen`
- [ ] в `AppRoot` при появлении сессии забрать отложенный intent и выполнить join
- [ ] **очищать `PendingJoinStore` при logout** — точка уже есть в `OfflineDataCleaner`
- [ ] написать тесты: парсинг нового URL, старого t.me-формата, голого кода; посторонний URL → null; `take` очищает; повторный `take` → null
- [ ] `./gradlew :app:testDebugUnitTest && ./gradlew :app:assembleDebug` — зелёные перед Task 20

### Task 20: iOS — экраны «Способы входа» и «Удалить аккаунт»

**Files:**
- Modify: `ios/Splitty/Features/Account/AccountView.swift`
- Modify: `ios/Splitty/Core/APIClient.swift`
- Modify: `ios/Splitty/Core/SessionStore.swift`
- Modify: `ios/Splitty/Core/Models.swift`

- [ ] добавить `linkedProviders` в модель `Me` (`Models.swift`)
- [ ] **исправить устаревший комментарий на `Models.swift:5`** («id пользователей — Telegram user id») — после этого плана он ложь
- [ ] секция «Способы входа»: список привязанных, кнопки привязать/отвязать
- [ ] отвязку последнего способа блокировать **в UI** (кнопка недоступна с пояснением), не полагаясь только на 409
- [ ] `409 identity_taken` показывать текстом «Этот аккаунт уже связан с другим профилем Splitty», без кода ошибки
- [ ] «Удалить аккаунт» внизу экрана, деструктивным стилем, с обязательным подтверждением
- [ ] в тексте подтверждения честно сказать: профиль удаляется, **а расходы и долги в группах остаются** (снимки сохраняются — иначе это ложь)
- [ ] после успешного `DELETE /me` — полный `logout`, чистка Keychain, `PendingJoin` и офлайн-кеша, возврат на экран входа
- [ ] написать тесты: успешное удаление приводит к разлогину и очистке; ошибка сети не разлогинивает; отвязка последнего способа заблокирована
- [ ] `xcodebuild test` — зелёные перед Task 21

### Task 21: Android — экраны «Способы входа» и «Удалить аккаунт»

**Files:**
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/profile/ProfileScreen.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/profile/ProfileViewModel.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/core/network/SplittyApi.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/core/model/Models.kt`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/build.gradle.kts`

- [ ] **каталога `ui/account/` не существует** — работаем в `ui/profile/`; `logout()` уже есть в `ProfileViewModel.kt:132`
- [ ] добавить `linkedProviders` в `Me` (`Models.kt`)
- [ ] секция «Способы входа» и «Удалить аккаунт» — паритет с iOS (Task 20), включая текст подтверждения про сохранение долгов
- [ ] отвязку последнего способа блокировать в UI
- [ ] после удаления — `logout` + чистка DataStore, `PendingJoinStore`, офлайн-кеша
- [ ] строки — в `strings.xml`, не хардкодом
- [ ] **новые `@Serializable`-модели добавить в `requiredSerializers`** (`build.gradle.kts:217-238`)
- [ ] перезаписать Roborazzi-эталоны, если менялся вид экрана профиля
- [ ] написать тесты `ProfileViewModel`: успешное удаление разлогинивает; ошибка сети не разлогинивает; отвязка последнего способа заблокирована
- [ ] `./gradlew :app:testDebugUnitTest && ./gradlew :app:assembleRelease` — зелёные (включая `verifyReleaseShrinking`) перед Task 22

### Task 22: Проверка приёмочных критериев

**Автоматически проверяемое (агент обязан подтвердить прогоном тестов или кодом):**

- [ ] есть тест: telegram-пользователь входит по коду и получает свои комнаты
- [ ] есть тест: новый google-пользователь получает `_id ≥ 10¹²` и не имеет `telegram_id`
- [ ] есть тест: telegram-уведомления НЕ уходят google-пользователю, push — уходит
- [ ] есть тест: аватар пользователя без telegram отдаёт 404
- [ ] есть тест: привязка занятой личности → 409 `identity_taken`
- [ ] есть тест: после `DELETE /me` токен даёт 401 в том же тесте; долги пересчитываются идентично; личность освободилась; демо-аккаунт защищён 403
- [ ] есть тест: повторный бэкфилл не трогает sentinel-пользователя, google-пользователей и tombstone
- [ ] есть тест: документы `room` после `JoinToRoom` не содержат `email`, `google_sub`, `apple_sub`, `telegram_id`
- [ ] есть тест-антирегрессия: у telegram-пользователя из снимка `userLink` содержит `tg://user?id=`
- [ ] есть тест: пользователь с `_id ≥ 10¹²` и telegram-привязкой видит свои комнаты, долги и статистику в боте, а созданная им операция возврата долга содержит в `donor.id` номер Splitty
- [ ] `grep -rn "getFrom(u" internal/bot/ | grep -v _test` — в остатке только объявление и явно прокомментированные исключения (см. Task 6)
- [ ] есть тест: ошибка отзыва Apple-токена не мешает удалению аккаунта, а у пользователя без Apple revoke не вызывается
- [ ] `GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go test ./internal/...` — всё зелёное
- [ ] `GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go vet ./...` — чисто
- [ ] `cd android && ./gradlew :app:testDebugUnitTest && ./gradlew :app:assembleRelease` — успешно
- [ ] `cd ios && xcodebuild test …` — зелёное
- [ ] перечислить в этом файле все задачи, помеченные `⚠️ заблокировано`, — они уходят в Post-Completion

**Ручное — агент не может проверить, переносится в Post-Completion:**

*Не отмечать чекбоксами. Агент лишь фиксирует список ниже в отчёте.*

- Бот вживую: `/start`, `/login`, экраны комнат, приходящие уведомления
- Реальный вход через Google и Apple на устройствах (нужны настроенные консоли)
- Диплинк на устройстве: у авторизованного открывает комнату, у неавторизованного проводит через вход
- Сверка долгов на копии продовых данных до и после бэкфилла

### Task 23: [Финал] Документация

**Files:**
- Modify: `docs/API.md`
- Modify: `README.md`
- Modify: `ios/asc/INSTRUCTION.md`

- [ ] описать в `docs/API.md` новые эндпоинты: `/auth/google`, `/auth/apple`, `/me/link/{provider}`, `DELETE /me`, публичные `/join/{roomId}` и `.well-known/*`
- [ ] задокументировать новые переменные окружения рядом с существующими `API_JWT_SECRET`, `API_DEV_AUTH`
- [ ] описать в `README.md` модель личности: `_id` — номер Splitty, `telegram_id` — привязка; явно предупредить, что `_id ≠ telegram id` у новых пользователей
- [ ] зафиксировать, что слияние аккаунтов не поддерживается и почему (денормализованные снимки)
- [ ] **отметить смену семантики существующих настроек**: `REVIEW_USER_ID` (`config.go:28`) и `DAILY_EXPENSES_USERS` (`config.go:18`, дефолт из 6 telegram id) теперь означают номера Splitty — для существующих пользователей значения совпадают, но новых туда вписывать нужно по `_id`
- [ ] описать в `README.md` запуск тестов репозитория (`MONGO_TEST_URI`, `docker compose up -d mongo`)
- [ ] дополнить `ios/asc/INSTRUCTION.md`: capability Sign in with Apple, требования 4.8 и 5.1.1(v), **отзыв токенов через `auth/revoke` при удалении аккаунта и то, что для него нужен `.p8`-ключ Apple в `APPLE_PRIVATE_KEY`**
- [ ] **предупредить в `README.md`, что `.p8`-ключ Apple и его содержимое в git не коммитятся** — только в `.env` на сервере, как и остальные секреты
- [ ] перенести план в `docs/plans/completed/`

## Post-Completion

*Внешние действия — без чекбоксов*

**Консоли**

- Google Cloud Console: **✅ сделано 30.07.2026.** Проект `gen-lang-client-0912753294` (Splitty, номер 327021108128), аккаунт `zagamaza2025@gmail.com`. Созданы четыре OAuth-клиента:

  | Клиент | Тип | Client ID (префикс `327021108128-`) |
  |---|---|---|
  | Splitty Web (server audience) | Web | `rm91uurc2il489qnv8hn32o1kcakemnl.apps.googleusercontent.com` |
  | Splitty iOS | iOS | `v1psnu3utn5govgn3n30h2omhvb21t1o.apps.googleusercontent.com` |
  | Splitty Android (Play App Signing) | Android | `8fk676qqflrmiejmasgfa8vnq8j9e1b3.apps.googleusercontent.com` |
  | Splitty Android (debug keystore) | Android | `2tgkjnoeeq7q2669ojh0p8gnvcfan1sg.apps.googleusercontent.com` |

  iOS — bundle `com.zagir.splitty`, Team `K8922Y6R3M`. Android — package `com.zagir.splitty`; SHA-1 Play App Signing `18:BC:FD:81:BE:0A:40:1E:D8:93:91:8D:FA:5D:F0:D6:34:0C:AB:F6`, SHA-1 локального debug-ключа `8B:F8:FC:55:7B:4B:14:79:7C:93:7C:9A:2D:A6:7F:2D:4E:49:D1:E6` (второй клиент нужен, иначе вход в debug-сборке падает с `error 10`).

  **⚠️ в `GOOGLE_CLIENT_IDS` (Task 10) идут только ДВА — iOS и Web.** Это список допустимых `aud`, а Android-клиенты в `aud` не появляются никогда: Credential Manager выдаёт токен с `aud` = **серверный (Web) client id**, а Android-клиенты нужны Google лишь для сверки package+подписи. Класть их в `GOOGLE_CLIENT_IDS` — расширять множество принимаемых audience без причины:

  ```
  GOOGLE_CLIENT_IDS=327021108128-v1psnu3utn5govgn3n30h2omhvb21t1o.apps.googleusercontent.com:327021108128-rm91uurc2il489qnv8hn32o1kcakemnl.apps.googleusercontent.com
  ```

  Серверный client id для `GetGoogleIdOption` на Android (Task 18) и reversed client id для `CFBundleURLTypes` на iOS (Task 16) — оттуда же: Web-клиент и `com.googleusercontent.apps.327021108128-v1psnu3utn5govgn3n30h2omhvb21t1o` соответственно.

  **SHA-256 Play App Signing для `assetlinks.json`** (Task 14, `ANDROID_CERT_SHA256`): `E6:8C:8C:AF:20:18:20:2B:E3:93:BF:BE:AE:B9:DA:E6:AB:E7:BD:AE:AA:39:D2:20:9D:24:E4:75:B4:ED:E7:D0`.

  Осталось вручную: **Publishing status проекта = Testing** (Google Auth Platform → Audience). Пока так, войти смогут только добавленные тестировщики (сейчас 1 из 100). Перед выкаткой нажать «Publish app» — со скоупами `openid/email/profile` верификация Google не требуется.
- Apple Developer Portal: capability **Sign in with Apple** для App ID `com.zagir.splitty` (Team `K8922Y6R3M`); bundle id — в `APPLE_CLIENT_IDS`.
- Apple Developer Portal → Keys: создать ключ с включённым **Sign in with Apple**, скачать `.p8` (**отдаётся один раз**). Team ID → `APPLE_TEAM_ID`, Key ID → `APPLE_KEY_ID`, содержимое файла → `APPLE_PRIVATE_KEY`. Нужен для `auth/revoke` при удалении аккаунта (5.1.1(v)); **в git не коммитить**.
- Play Console → App integrity: SHA-256 сертификата Play App Signing для `assetlinks.json`.

**Домен и TLS** (блокирует Tasks 14, 17, 19 в проде)

- Домен на `138.124.18.189`, TLS (Let's Encrypt).
- Проверить, что `.well-known/apple-app-site-association` отдаётся по HTTPS **без редиректов** с `Content-Type: application/json`.
- App Links: `adb shell pm verify-app-links --re-verify com.zagir.splitty`, затем `adb shell pm get-app-links com.zagir.splitty` → `verified`.
- До появления домена задачи реализуются с плейсхолдером и `TODO`; включение — через `PUBLIC_BASE_URL`.

**Деплой**

- Новые переменные в `.env` на сервере (`/home/splitit/app/`), не коммитить.
- **Снимок базы до первого старта с бэкфиллом.**
- Выкатить бэкенд **до** публикации клиентов; проверить, что старые клиенты работают (`/auth/code`).
- **Сверка долгов на реальных данных**: после бэкфилла выборочно сравнить `GET /rooms/{id}/debts` для нескольких комнат со значениями до миграции. Автоматического скрипта в плане нет намеренно — в репозитории нет доступа к проду; unit-инвариант из Task 13 покрывает логику, а эта проверка подтверждает данные.

**Ревью в сторах**

- iOS: Sign in with Apple смотрят внимательнее; заранее проверить кнопку удаления аккаунта — 5.1.1(v) частая причина отказа. Отдельно проверить, что при удалении аккаунта, созданного через Apple, приложение действительно пропадает из **Настройки → Apple ID → Вход с Apple**: это видимое подтверждение, что `auth/revoke` отработал.
- Обновить заметки для ревьюеров: демо-вход по `REVIEW_LOGIN_CODE` работает, добавились Google/Apple.

**Осознанно вне объёма**

- **Слияние двух существующих аккаунтов** — требует переписывания встроенных снимков во всех комнатах; сейчас предотвращается ответом 409 при привязке.
- **Deferred deeplink** (установка из стора → сразу в группу) — Firebase Dynamic Links закрыт, без внешнего сервиса не делается. Замена — код на странице `/join` + кнопка «Вставить» в приложении.
- **Вход через Apple на Android** — требует веб-редиректа; правило 4.8 на Android не распространяется.
