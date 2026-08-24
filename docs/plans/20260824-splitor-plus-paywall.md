# Splitor Plus: платный тариф на AI-распознавание

> Ревизия 2. Первая версия плана прошла два независимых ревью (Opus-агент и
> Codex gpt-5.5, состязательное). Найдены 1 критическая и 6 высоких проблем,
> из них три — прямые ошибки в тексте плана. Все находки учтены ниже;
> в конце файла — таблица «находка → где закрыта».

## Overview

Сейчас AI-распознавание расхода (голос/фото чека) доступно всем и ограничено
одним общим лимитом `AI_PARSE_DAILY_QUOTA=50` в сутки — за Gemini платит
проект, отдачи нет. Вводим тариф:

- **Free** — 5 распознаваний в сутки. Всё остальное (группы, расходы, долги,
  расчёт, напоминания) остаётся бесплатным и без лимитов.
- **Splitor Plus** — суточный лимит снят. Подписка: месяц и год.

Требования владельца:
1. лимитом рулит **бэкенд**, клиент его не назначает;
2. остаток **виден в UI** до того, как упрёшься;
3. по исчерпании — **paywall**, дальше только через оплату;
4. paywall должен быть **хорошим**.

Ключевое решение: **сервер — единственный источник правды**. Клиент никогда не
решает, платный он; он присылает подписанный чек стора, сервер проверяет его у
Apple/Google и сам считает лимит.

**Версия: 1.7.** Сборка 1.6 сейчас в `WAITING_FOR_REVIEW` — не трогаем ни
номер, ни метаданные, ни submission.

**Объём: делаем целиком.** Без деления на «первую итерацию» — вебхуки,
фоновая доработка, привязка чеков и дизайн paywall входят в один заход.

## Context (from discovery)

- `internal/service/ratelimit.go` — лимитер: минутное окно + суточное, счётчики
  в Mongo с TTL. **Минутное проверяется первым** (ratelimit.go:36-42) — это
  ломает paywall, см. решение «приоритет суточного».
- `internal/rest/parse_handler.go:56` — 429 `rate_limited`, минутный и суточный
  неразличимы.
- `internal/rest/server.go:586` — `writeError` пишет **вложенный** конверт
  `{"error":{"code","message"}}`. Клиенты разбирают именно его:
  `ios/Splitty/Core/APIClient.swift:74`, `ApiException.kt:105`.
  Третьего поля рядом с `error` текущий writer не умеет.
- `internal/rest/server.go:362-366` — публичные страницы: только `/privacy` и
  `/account-deletion`. **`/terms` нет** — а paywall подписки обязан на него
  ссылаться.
- Ни iOS, ни Android **не шлют версию клиента**: `APIClient.swift:913-920`,
  `Interceptors.kt:44-52`. Версионного гейта сейчас не существует.
- `internal/rest/delete_account.go:129` — `purgeUserData`, нумерованный
  конвейер побочных коллекций с PII; подписки туда не попадут сами.
- iOS: `AddExpenseViewModel.swift:507` (`parse`), `AddExpenseView.swift`,
  `Core/Theme.swift` (токены, есть `receiptPaper`).
- Android: `AddExpenseViewModel.kt:1154`, `SplittyRepository.kt:234`.
- Покупок нет нигде — гринфилд.
- Прод: `PUBLIC_BASE_URL=https://splitor.zagirnur.dev` — HTTPS есть, вебхуки
  принимать можно. `AI_PARSE_DAILY_QUOTA` в прод-compose **не задан**
  (работает дефолт 50) — но полагаться на это нельзя, см. задачу 5.

## Development Approach

- **testing approach**: Regular (код, затем тесты) — как в репозитории.
- каждая задача доводится до конца перед следующей;
- **каждая задача обязана содержать новые/обновлённые тесты**;
- **все тесты зелёные перед началом следующей задачи** — и это значит, что
  задача обязана включать *все* файлы, которые ломает её изменение, включая
  вызывающий код и тестовые фейки;
- собирать бинарь на go1.22.12, гонять тесты на go1.24.6.

## Testing Strategy

- **unit-тесты** — обязательны в каждой задаче (`go test ./...`).
- **Тесты платёжного жизненного цикла пишутся до UI paywall** — покупка,
  продление, отмена, возврат, смена плана, потерянный вебхук, переупорядоченные
  уведомления. Ошибка здесь стоит денег, ошибка в вёрстке — нет.
- **Тесты совместимости с текущими клиентами**: ответ обязан разбираться
  моделями 1.6 (`APIError.server`, `ErrorEnvelope`).
- **iOS** — юнит-тесты на `SubscriptionStore` и маппинг; paywall прогоняется на
  симуляторе через `Splitty.storekit`.
- **Android** — юнит-тесты ViewModel; Billing проверяется живыми тестовыми
  покупками на внутреннем треке.

## Progress Tracking

- выполненное отмечать `[x]` сразу; новые задачи — с ➕; блокеры — с ⚠️;
- при изменении объёма править этот файл.

## Solution Overview

### Поток прав

```
клиент покупает в сторе, передав СВОЙ токен привязки
   ↓ подписанный чек (Apple JWS / Google purchaseToken)
POST /api/v1/me/subscription/{apple|google}
   ↓ сервер: подпись → окружение → productId из белого списка
   ↓         → токен привязки совпадает с userId?
   ↓         → Google: acknowledge (с записью pending_ack и ретраем)
запись в subscriptions (expiresAt, autoRenew, storeRef, bindingToken)
   ↓
Entitlements.Tier(userId) → free | plus   ← единственный источник правды
   ↓
лимит на /parse; quota в ответах API
```

Продление/отмена/возврат приходят вебхуками. Вебхук **не применяет payload как
есть** — он перезапрашивает актуальное состояние у стора и пишет его
(см. «переупорядочивание»). Плюс фоновый воркер: ретрай неподтверждённых
покупок и досинхронизация истекающих.

### Решения, вытекающие из ревью

**1. Приоритет суточного лимита над минутным.** Сейчас минутное окно
проверяется первым (ratelimit.go:36-42). При `rate_per_min=5` и `free=5`
человек, потративший 5 распознаваний за минуту, на шестом получит «слишком
часто», а не paywall — то есть **экран оплаты не откроется на главном
сценарии**. Новый порядок:

```
1. used := Get(dayKey)              // ЧТЕНИЕ, без инкремента
   если quota > 0 && used >= quota  → daily exceeded (paywall), ничего не инкрементируем
2. n := Incr(minKey)
   если n > ratePerMin              → minute throttle (суточный НЕ тронут)
3. d := Incr(dayKey)
   если d > quota                   → daily exceeded (страховка от гонки)
```

Побочно чинит и «`remaining` в минусе»: шаг 1 отбивает запрос до инкремента,
поэтому счётчик не убегает за лимит (гонку добивает clamp).

**2. Привязка чека к аккаунту.** Иначе действует правило «чей чек — того, кто
первый прислал»: утёкший JWS забирает быстрейший, а человек, удаливший аккаунт
и заведший новый, получает вечный 409. Решение: у пользователя есть стабильный
`purchaseBindingToken` (UUID, выдаётся сервером); клиент передаёт его в стор
(`Product.PurchaseOption.appAccountToken` / `setObfuscatedAccountId`), сервер
сверяет с тем, что приехало в чеке.

Несовпадение → `409 receipt_belongs_to_other_account` с внятным текстом и
описанным путём переноса. **Пока клиенты без токена не обновились** —
несовпадение логируем, но не отклоняем (чек без `appAccountToken` легален).

**3. Sandbox не даёт Plus на проде.** Sandbox-подписки бесплатны и продлеваются
каждые 5 минут — принимать их в проде значит раздавать вечный Plus. Явная
политика: `STORE_ALLOWED_ENVIRONMENT` (prod принимает только `Production`,
исключение — `PLUS_COMP_USER_IDS`). Хост App Store Server API выбирается по
`environment` записи, иначе получим 404 и снимем Plus у платящего.

**4. Вебхук сверяет состояние, а не применяет событие.** Идемпотентности мало:
задержавшийся `EXPIRED` может прийти после `DID_RENEW` и погасить активную
подписку. Поэтому обработчик берёт из уведомления только идентификатор, идёт в
стор за текущим статусом и пишет его. Дополнительно храним `lastNotifiedAt`
(signedDate) и не откатываемся на более старое событие.

**5. Google acknowledge — состояние, а не вызов.** Если сервер не подтвердит
покупку за 3 дня, Google вернёт деньги. Поэтому `ackState` (`pending`/`done`)
живёт в документе, ack делается в трёх местах (валидация, вебхук, фоновый тик)
и идемпотентен.

**6. Grace period — сторовый, не свой.** У обоих сторов billing retry и account
hold уже отражены в `expiryTime`/`gracePeriodExpiresDate`. Свои +72 ч поверх —
раздача бесплатного доступа и вранье на экране «до какого числа». Оставляем
буфер только на задержку доставки уведомления: `PLUS_DELIVERY_SLACK=2h`.

**7. Совместимость с уже отправленной 1.6 — заголовком версии.** Сейчас клиенты
версию не шлют, гейта нет. Вводим `X-Client-Version`; **отсутствие заголовка =
старый клиент**, который физически не умеет показать paywall, и он получает
`AI_LEGACY_DAILY_QUOTA` (дефолт 50).
Честно про цену решения: это **ramp совместимости, а не граница
безопасности** — пропатченный клиент может не слать заголовок и получить 50
вместо 5. Осознанно принимаем: потолок злоупотребления — 50 распознаваний, а
не безлимит; после раскатки 1.7 переменная опускается до 5, ветка удаляется.

**8. `-1` вместо `0` для безлимита.** `0 = безлимит` — fail-open: пустая или
битая переменная тихо раздаёт безлимит всем. Безлимит = `-1`, а `quota <= 0`
при старте — отказ стартовать.

**9. Время — UTC.** Ключ суток строится `now.Format("2006-01-02")` от локального
времени процесса (ratelimit.go:45). Нужен явный `.UTC()` в ключе и в
`ResetsAt`. В UI не «обновятся в 00:00» (для UTC+5 это враньё), а «через N ч».

**10. `quota` — понятие REST-слоя.** Успешный `/parse` сериализуется прямо из
`ai.ParseResult` (ai.go:69, parse_handler.go:108). Класть туда квоту нельзя —
нужна DTO-обёртка. Отдельно решается ветка 502 (parse_handler.go:88): попытка
уже списана, квоту вернуть надо.

**11. Формат ошибки — вложенный.** Канон: `{"error":{"code","message"},"quota":{…}}`.
Текущий `writeError` третьего поля не умеет — нужен отдельный writer.

**12. `go-iap` — решено, не развилка.** Берём `github.com/awa/go-iap`: своя
проверка цепочки x5c до Apple Root CA G3 плюс клиент Play Developer API — это
несколько сотен строк криптокода, который страшно писать самому.

### Дизайн paywall

Направление — **«чек, который не оторвали»**, из существующих токенов
(`Core/Theme.swift`), а не чужеродный премиум-градиент.

- **Материал**: карточка на токене `receiptPaper` (уже есть, «бумага чека») с
  рваным нижним краем. Фон — `bg`.
- **Герой — сам момент, а не список фич.** Строка распознанной речи
  («Ужин 3200, делим на четверых») превращается в готовый расход с аватарками и
  долями. Никаких «Unlock Premium ✨» и галочек.
- **Сигнатура**: счётчик лимита — **пять отрывных корешков чека**,
  израсходованные оторваны. Единственный акцент; всё остальное тихое.
- **Цвет**: изумруд `accent` только на CTA и бейдже скидки.
- **Тарифы**: год по умолчанию, месяц рядом. Цены **из стора**,
  локализованные. Бейдж скидки считается, **только если валюты обоих продуктов
  совпадают** — иначе цифра соврёт; при несовпадении бейдж скрыт.
- **Тексты по делу**: «Распознавания на сегодня кончились · обновятся через 7 ч».
  Рядом — спокойная ссылка «добавить вручную»: человек не должен думать, что
  приложение встало.
- **Обязательное по Guideline 3.1.2** (иначе реджект): цена, длительность
  периода, явное «продлевается автоматически», «Восстановить покупки», ссылки
  на **Условия** и Политику.
- Квалити-флор: адаптив, VoiceOver/TalkBack, `reduce motion`, тёмная тема.

### Индикатор остатка

Подпись под микрофоном появляется, **только когда осталось ≤ 2**, и никогда у
Plus. Пока распознаваний вдоволь — интерфейс молчит.

## Technical Details

### Коллекция `subscriptions`

```go
type Subscription struct {
    UserId    int    `bson:"user_id"`
    Store     string `bson:"store"`      // "apple" | "google"
    ProductId string `bson:"product_id"`
    // Ключ сопоставления с уведомлениями стора: Apple — originalTransactionId,
    // Google — purchaseToken (РОТИРУЕТСЯ при смене плана, см. LinkedRef).
    StoreRef string `bson:"store_ref"`
    // LinkedRef — предыдущий purchaseToken Google (linkedPurchaseToken).
    // Без него смена месяц↔год плодит вторую запись, и старая продолжает
    // «держать» Plus после возврата.
    LinkedRef string `bson:"linked_ref,omitempty"`
    // BindingToken — purchaseBindingToken пользователя из appAccountToken /
    // obfuscatedAccountId. Пусто — чек от клиента, который его ещё не слал.
    BindingToken string     `bson:"binding_token,omitempty"`
    ExpiresAt    time.Time  `bson:"expires_at"`
    AutoRenew    bool       `bson:"auto_renew"`
    Environment  string     `bson:"environment"` // "Sandbox" | "Production"
    AckState     string     `bson:"ack_state"`   // google: "pending" | "done" | "n/a"
    SupersededAt *time.Time `bson:"superseded_at,omitempty"` // заменена новым токеном
    RevokedAt    *time.Time `bson:"revoked_at,omitempty"`    // возврат/чарджбек
    // LastNotifiedAt — signedDate последнего применённого уведомления:
    // защита от переупорядоченной доставки (старый EXPIRED после DID_RENEW).
    LastNotifiedAt time.Time `bson:"last_notified_at"`
    CheckedAt      time.Time `bson:"checked_at"`
    UpdatedAt      time.Time `bson:"updated_at"`
}
```

Индексы: uniq `(store, store_ref)`, `user_id`, `expires_at`,
`ack_state` (частичный, для воркера), `linked_ref`.

### Конфиг

```
AI_FREE_DAILY_QUOTA=5          # было AI_PARSE_DAILY_QUOTA; читается с fallback на старое имя
AI_PLUS_DAILY_QUOTA=-1         # -1 = безлимит (0 запрещён — fail-open)
AI_LEGACY_DAILY_QUOTA=50       # клиентам без X-Client-Version (не умеют paywall)
AI_PARSE_RATE_PER_MIN=5        # anti-abuse, оба тарифа
PLUS_COMP_USER_IDS=            # ":" — тариф без покупки (я + демо-аккаунт ревьюера)
PLUS_DELIVERY_SLACK=2h         # только на задержку доставки уведомления
STORE_ALLOWED_ENVIRONMENT=Production
PLUS_PRODUCT_IDS=com.zagir.splitty.plus.monthly:com.zagir.splitty.plus.yearly

APPLE_IAP_ISSUER_ID=           # ключ In-App Purchase, ОТДЕЛЬНЫЙ от ключа выкладки
APPLE_IAP_KEY_ID=
APPLE_IAP_PRIVATE_KEY=         # содержимое .p8, НЕ путь
APPLE_IAP_BUNDLE_ID=com.zagir.splitty

GOOGLE_PLAY_SA_JSON=
GOOGLE_PLAY_PACKAGE=com.zagir.splitty
```

Пустые ключи стора = покупки выключены: `/subscription/*` отдаёт 503, все
считаются free. Та же политика, что у Gemini и FCM.

### API

**`GET /api/v1/me/ai-quota`**
```json
{"tier":"free","limit":5,"used":2,"remaining":3,
 "unlimited":false,"resetsAt":"2026-08-25T00:00:00Z"}
```

**`POST .../operations/parse`** — успешный ответ получает `quota` через
DTO-обёртку (не в `ai.ParseResult`).

**429 — канонический вложенный конверт:**
```json
{"error":{"code":"ai_quota_exceeded","message":"..."},
 "quota":{"tier":"free","limit":5,"used":5,"remaining":0,"resetsAt":"..."}}
```
Минутный лимит остаётся `rate_limited` — paywall на него не показываем.

**`POST /api/v1/me/subscription/apple`** `{"jws":"<JWSTransaction>"}`
**`POST /api/v1/me/subscription/google`** `{"purchaseToken":"…","productId":"…"}`
**`GET /api/v1/me/subscription`** — состояние для экрана управления
**`POST /api/v1/webhooks/apple`** — ASSN V2
**`POST /api/v1/webhooks/google`** — Pub/Sub push (RTDN + voidedPurchase)
**`GET /terms`** — публичная страница условий подписки

Вебхуки — без `s.auth` (аутентифицирует подпись стора), отвечают `200` быстро,
обработка асинхронная: Pub/Sub ретраит по таймауту.

### Лимитер

```go
type Decision struct {
    Allowed  bool
    Kind     string // "" | "minute" | "daily"
    Used     int64
    Limit    int
    ResetsAt time.Time
}
func (rl *RateLimiter) AllowParse(ctx context.Context, userId, dailyQuota int) (Decision, error)
```
`dailyQuota == -1` — суточное окно не считается вовсе. Квота приходит снаружи;
поле `dailyQuota` из структуры и конструктора **убирается** — иначе два
источника правды. `UsageCounter` получает `Get` (`ErrNoDocuments` → `0, nil`).

## What Goes Where

- **Implementation Steps** — всё, что делается в репозитории.
- **Post-Completion** — консоли сторов и банк; часть я физически сделать не могу.

## Implementation Steps

### Task 1: Модель подписки и токен привязки покупки

**Files:**
- Create: `internal/api/subscription.go`
- Modify: `internal/api/tg.go`
- Modify: `internal/repository/user.go`
- Modify: `internal/repository/user_test.go`

- [x] описать `api.Subscription` (см. Technical Details) и `api.Tier`
- [x] добавить `PurchaseBindingToken` в `api.User` (`bson:"purchase_binding_token,omitempty"`, из JSON исключён — наружу отдаётся только через `/me`)
- [x] `UserRepository.EnsureBindingToken(ctx, userId) (string, error)` — выдаёт UUID при первом обращении, идемпотентно
- [x] написать тесты: токен создаётся один раз, параллельные вызовы дают один и тот же
- [x] написать тесты на пользователя без токена (старые документы)
- [x] прогнать тесты — зелёные перед задачей 2

### Task 2: Репозиторий подписок

**Files:**
- Create: `internal/repository/subscription.go`
- Create: `internal/repository/subscription_test.go`

- [x] `MongoSubscriptionRepository`: `Upsert`, `ActiveByUser`, `ByStoreRef`, `Supersede`, `MarkRevoked`, `SetAckState`, `PendingAcks`, `ExpiringBefore`
- [x] `EnsureIndexes`: uniq `(store, store_ref)`, `user_id`, `expires_at`, частичный по `ack_state`, `linked_ref`
- [x] `Upsert` не откатывает состояние назад по `LastNotifiedAt` (защита от переупорядочивания)
- [x] написать тесты: upsert нового/существующего, поиск по storeRef и linkedRef, отзыв, supersede
- [x] написать тесты: старое уведомление не перезаписывает новое, `PendingAcks` возвращает только `pending`
- [x] прогнать тесты — зелёные перед задачей 3

### Task 3: Публичная страница условий подписки

**Files:**
- Create: `internal/rest/terms.go`
- Create: `internal/rest/terms_test.go`
- Modify: `internal/rest/server.go`

- [x] `GET /terms` рядом с `/privacy` (server.go:365) — публичная, читается без входа
- [x] текст условий подписки: что входит в Plus, период, автопродление, отмена через стор, возвраты
- [x] написать тесты: 200 без авторизации, корректный content-type, непустое тело
- [x] прогнать тесты — зелёные перед задачей 4

### Task 4: Лимитер — приоритет суточного, UTC, чтение без инкремента

**Files:**
- Modify: `internal/service/ratelimit.go`
- Modify: `internal/service/ratelimit_test.go`
- Modify: `internal/repository/ai_usage.go`
- Modify: `internal/rest/parse_handler.go`
- Modify: `internal/rest/parse_handler_test.go`
- Modify: `cmd/splitty/main.go`

- [x] ввести `service.Decision`; сменить сигнатуру на `AllowParse(ctx, userId, dailyQuota int)`, **убрать `dailyQuota` из структуры и конструктора**
- [x] реализовать порядок «читаем сутки → минута → инкремент суток» (см. Solution Overview, решение 1)
- [x] `dailyQuota == -1` — суточное окно не трогается вовсе
- [x] ключ суток и `ResetsAt` — явно в UTC; `Used` клампится к `Limit`
- [x] добавить `Get` в `UsageCounter` и `MongoAiUsageRepository` (`ErrNoDocuments` → `0, nil`)
- [x] адаптировать вызывающий код: `parse_handler.go:56`, `main.go:382`, фейки `fakeCounter` (ratelimit_test.go:16) и `unitCounter` (parse_handler_test.go:241)
- [x] написать тест: 5 распознаваний в одну минуту, 6-е → `daily`, а не `minute` (главный сценарий paywall)
- [x] написать тесты: минутный троттл не списывает суточную квоту, безлимит не пишет счётчик, `Used` не уходит за `Limit`, `ResetsAt` в UTC
- [x] прогнать тесты — зелёные перед задачей 5

### Task 5: Конфиг и сервис Entitlements

**Files:**
- Modify: `cmd/splitty/config.go`
- Modify: `cmd/splitty/config_test.go`
- Create: `internal/service/entitlements.go`
- Create: `internal/service/entitlements_test.go`

- [x] добавить переменные из раздела «Конфиг»; `AI_FREE_DAILY_QUOTA` читать **с fallback на `AI_PARSE_DAILY_QUOTA`** (не полагаться на то, что в проде оно не задано)
- [x] валидация на старте: `free/legacy quota <= 0` → отказ стартовать; безлимит только `-1`
- [x] `Entitlements.Tier(ctx, userId)`: plus при неотозванной, не-superseded подписке с `ExpiresAt + PLUS_DELIVERY_SLACK > now`; comp-список — до похода в базу
- [x] `QuotaFor(tier, clientKnowsPaywall bool) int` — free / plus / legacy
- [x] кеш тарифа на пользователя (TTL ~1 мин) — резолв висит на горячем пути `/parse`
- [x] ленивая досинхронизация: истёкшая запись с `autoRenew=true` и `CheckedAt` старше 15 мин помечается на обновление **в фоне**, синхронный поход в стор из `/parse` не делается
- [x] правило отказов: ошибка репозитория → free (fail closed); **недоступность стора → текущее состояние сохраняется** (не отбирать Plus у платящего из-за таймаута)
- [x] написать тесты: free без подписки, plus активная, plus в slack-окне, free после, free при отзыве/supersede, comp-юзер, legacy-клиент
- [x] написать тесты: ошибка репозитория → free, недоступность стора → прежний тариф, кеш не переживает TTL
- [x] прогнать тесты — зелёные перед задачей 6

### Task 6: Проверка чеков Apple

**Files:**
- Create: `internal/store/apple.go`
- Create: `internal/store/apple_test.go`
- Modify: `go.mod`

- [x] подключить `github.com/awa/go-iap`
- [x] `Verify(ctx, jws)`: подпись и цепочка, bundle id, `productId` из `PLUS_PRODUCT_IDS`, извлечь `originalTransactionId`, `expiresDate`, `environment`, `appAccountToken`
- [x] `Status(ctx, originalTransactionId, env)` — `getAllSubscriptionStatuses`, **хост выбирается по `environment`** (иначе 404 снимет Plus у платящего)
- [x] отказ при `environment`, не входящем в `STORE_ALLOWED_ENVIRONMENT` (кроме comp-юзеров)
- [x] написать тесты на фикстурах: валидный чек, протухший, чужой bundle id, битая подпись, чужой productId
- [x] написать тест: **sandbox-чек при `STORE_ALLOWED_ENVIRONMENT=Production` отвергается**
- [x] написать тест: недоступность API стора → ошибка, а не «не подписан»
- [x] прогнать тесты — зелёные перед задачей 7

### Task 7: Проверка чеков Google и подтверждение покупок

**Files:**
- Create: `internal/store/google.go`
- Create: `internal/store/google_test.go`

- [x] `Verify(ctx, token, productId)`: `purchases.subscriptionsv2.get` → `expiryTime`, `autoRenewing`, `subscriptionState`, `linkedPurchaseToken`, `externalAccountIdentifiers.obfuscatedExternalAccountId`, `acknowledgementState`
- [x] `Acknowledge(ctx, token)` — идемпотентно; уже подтверждённая покупка не ошибка
- [x] `Status(ctx, token)` для фоновой досинхронизации
- [x] проверка `productId` по белому списку и окружения (тестовые покупки лицензированных тестеров — как sandbox)
- [x] написать тесты: валидный/протухший/отозванный токен, уже подтверждённая покупка, `linkedPurchaseToken` присутствует
- [x] написать тесты: ack после успешной валидации упал → состояние остаётся `pending` (не теряется)
- [x] прогнать тесты — зелёные перед задачей 8

### Task 8: Эндпоинты подписки и привязка чека к аккаунту

**Files:**
- Create: `internal/rest/subscription.go`
- Create: `internal/rest/subscription_test.go`
- Modify: `internal/rest/server.go`
- Modify: `internal/rest/dto.go`

- [x] `POST /api/v1/me/subscription/{apple|google}`: проверить чек → сверить binding token → upsert → (Google) ack с записью `ackState` → вернуть тариф
- [x] несовпадение binding token → `409 receipt_belongs_to_other_account`; **пустой токен в чеке — принять и залогировать** (клиент ещё не обновился)
- [x] Google: при наличии `linkedPurchaseToken` — `Supersede` предшественника, а не вторая запись
- [x] `GET /api/v1/me/subscription` — тариф, продукт, до какого числа, автопродление, откуда управлять
- [x] отдавать `purchaseBindingToken` в `/me` (`toMeDto`, dto.go:278) — клиенту он нужен до покупки
- [x] 503 при пустых ключах стора
- [x] написать тесты: успех обоих сторов, битый/протухший чек, 503, 409 при чужом токене, приём чека без токена
- [x] написать тесты: смена плана по `linkedPurchaseToken` не плодит запись; ack записан
- [x] прогнать тесты — зелёные перед задачей 9

### Task 9: Квота в API, канонический конверт ошибки, версия клиента

**Files:**
- Modify: `internal/rest/server.go`
- Modify: `internal/rest/dto.go`
- Modify: `internal/rest/parse_handler.go`
- Modify: `internal/rest/parse_handler_test.go`
- Create: `internal/rest/quota.go`
- Create: `internal/rest/quota_test.go`

- [x] writer ошибки с третьим полем: `{"error":{"code","message"},"quota":{…}}` — расширить `errorResponse`, **не ломая** текущую форму (server.go:586)
- [x] читать `X-Client-Version`; отсутствие = legacy-клиент → `AI_LEGACY_DAILY_QUOTA`
- [x] резолвить тариф перед лимитером, передавать квоту в `AllowParse`
- [x] коды: `rate_limited` (минута) и `ai_quota_exceeded` (сутки, → paywall)
- [x] DTO-обёртка успешного `/parse` с полем `quota` (**не** в `ai.ParseResult`); ветка 502 (parse_handler.go:88) тоже возвращает квоту — попытка уже списана
- [x] `GET /api/v1/me/ai-quota` — чтение без инкремента
- [x] написать тест совместимости: тело 429 разбирается моделями 1.6 (`{"error":{"code","message"}}`), `message` непустой
- [x] написать тесты: free упирается на 6-м, plus не упирается, legacy-клиент получает 50, минутный лимит не даёт `ai_quota_exceeded`, `quota` в успехе и в 502
- [x] прогнать тесты — зелёные перед задачей 10

### Task 10: Вебхуки сторов

**Files:**
- Create: `internal/rest/store_webhooks.go`
- Create: `internal/rest/store_webhooks_test.go`
- Modify: `internal/rest/server.go`

- [x] `POST /api/v1/webhooks/apple`: проверить подпись `signedPayload`, взять идентификатор, **перезапросить статус у стора** и записать его
- [x] `POST /api/v1/webhooks/google`: проверить OIDC-токен Pub/Sub, обработать RTDN **и `voidedPurchaseNotification`** (возвраты приходят только там)
- [x] защита от переупорядочивания: не применять уведомление старше `LastNotifiedAt`
- [x] идемпотентность повторной доставки; ack для `pending` покупок прямо в вебхуке
- [x] отвечать `200` быстро, обработку вести асинхронно (Pub/Sub ретраит по таймауту); проверить, что тело проходит `maxBodyMiddleware` (server.go:436)
- [x] написать тесты на каждый тип уведомления, повторную доставку, подделанную подпись
- [x] написать тест: старый `EXPIRED` после `DID_RENEW` **не** гасит активную подписку
- [x] написать тест: `voidedPurchaseNotification` снимает Plus
- [x] прогнать тесты — зелёные перед задачей 11

### Task 11: Фоновый воркер подписок

**Files:**
- Create: `internal/service/subscription_worker.go`
- Create: `internal/service/subscription_worker_test.go`
- Modify: `cmd/splitty/main.go`

- [x] тик: ретрай `PendingAcks` (Google откатит покупку через 3 дня — вебхука может не быть)
- [x] тик: досинхронизация `ExpiringBefore(now + slack)` через `Status` стора
- [x] бэк-офф и предел попыток; недоступность стора не меняет тариф
- [x] запускать рядом с push-воркером (main.go, где `go worker.Run(ctx)`)
- [x] написать тесты: pending ack подтверждается, истекшая обновляется, стор недоступен → состояние сохранено
- [x] написать тест: воркер идемпотентен при повторном тике
- [x] прогнать тесты — зелёные перед задачей 12

### Task 12: Чистка подписок при удалении аккаунта

**Files:**
- Modify: `internal/rest/delete_account.go`
- Modify: `internal/rest/delete_account_test.go`

- [x] добавить `subscriptions` в `purgeUserData` (шаг 5, delete_account.go:129) — там `user_id` и идентификаторы покупок
- [x] снять привязку так, чтобы повторная покупка новым аккаунтом не упиралась в вечный 409
- [x] написать тесты: подписки удалены, повторная регистрация может купить снова
- [x] прогнать тесты — зелёные перед задачей 13

### Task 13: iOS — StoreKit 2

**Files:**
- Create: `ios/Splitty/Core/StoreKitService.swift`
- Create: `ios/Splitty/Core/SubscriptionStore.swift`
- Create: `ios/Splitty/Splitty.storekit`
- Modify: `ios/Splitty/Core/APIClient.swift`
- Modify: `ios/project.yml`

- [x] `StoreKitService`: продукты, `purchase(options: [.appAccountToken(bindingToken)])`, `Transaction.updates`, восстановление
- [x] **`transaction.finish()` только после подтверждения сервером**; до тех пор транзакция переотправляется из `Transaction.updates` (иначе теряется оплаченный доступ)
- [x] тариф берётся **из ответа сервера**, не из StoreKit
- [x] слать `X-Client-Version` во всех запросах (APIClient.swift:913-920)
- [x] методы квоты/подписки и разбор `ai_quota_exceeded` + `receipt_belongs_to_other_account`
- [x] `Splitty.storekit` для покупок в симуляторе, включить в схему
- [x] написать тесты на маппинг ответов, состояний и ошибок
- [x] прогнать тесты — зелёные перед задачей 14

### Task 14: iOS — экран paywall

**Files:**
- Create: `ios/Splitty/Features/Paywall/PaywallView.swift`
- Create: `ios/Splitty/Features/Paywall/ReceiptStubsView.swift`
- Create: `ios/Splitty/Features/Paywall/PaywallStrings.swift`

- [x] сверстать по разделу «Дизайн paywall»: `receiptPaper` с рваным краем, герой «речь → готовый расход»
- [x] `ReceiptStubsView` — счётчик из отрывных корешков
- [x] два тарифа, год по умолчанию, цены из `Product.displayPrice`; **бейдж скидки только при совпадении валют**
- [x] обязательное по 3.1.2: период, автопродление, «Восстановить покупки», ссылки на `/terms` и `/privacy`
- [x] состояния: загрузка цен, покупка идёт, ошибка, «уже подписан», «чек привязан к другому аккаунту»
- [x] «обновятся через N ч» (не «в 00:00»); ссылка «добавить вручную»
- [x] локализация строк экрана на 5 языков — здесь же, не отдельной задачей
- [x] адаптив, VoiceOver, `reduce motion`, тёмная тема
- [x] написать тесты на расчёт скидки (включая разные валюты) и подбор строк
- [x] прогнать тесты — зелёные перед задачей 15

### Task 15: iOS — остаток у микрофона и вызов paywall

**Files:**
- Modify: `ios/Splitty/Features/Expense/AddExpenseView.swift`
- Modify: `ios/Splitty/Features/Expense/AddExpenseViewModel.swift`
- Modify: `ios/Splitty/Features/Account/` (строка «Splitor Plus»)

- [x] подпись «осталось N из 5» под микрофоном — только при N ≤ 2 и только для free
- [x] обновлять остаток из поля `quota` ответа `/parse`, без доп. запроса
- [x] `ai_quota_exceeded` → paywall; **черновик и записанный звук не теряются**
- [x] `rate_limited` → прежний спокойный тост, без paywall
- [x] строка «Splitor Plus» в настройках: статус, вход в paywall, управление подпиской
- [x] написать тесты ViewModel: переход в paywall, сохранение черновика, порог показа подписи
- [x] прогнать тесты — зелёные перед задачей 16

### Task 16: Android — Play Billing

**Files:**
- Create: `android/app/src/main/java/com/zagir/splitty/billing/BillingService.kt`
- Create: `android/app/src/main/java/com/zagir/splitty/billing/SubscriptionRepository.kt`
- Modify: `android/app/build.gradle.kts`
- Modify: `android/app/src/main/java/com/zagir/splitty/core/network/Interceptors.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/data/SplittyRepository.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/core/network/ApiException.kt`

- [x] подключить Play Billing Library 7
- [x] `launchBillingFlow` с `setObfuscatedAccountId(bindingToken)`
- [x] `purchaseToken` уходит на бэк; **подтверждает покупку сервер** — клиент лишь дожидается успеха
- [x] восстановление через `queryPurchasesAsync` на старте
- [x] слать `X-Client-Version` (Interceptors.kt:44)
- [x] разбор `ai_quota_exceeded` и `receipt_belongs_to_other_account` в `ApiException`
- [x] написать тесты на маппинг состояний и ошибок
- [x] прогнать тесты — зелёные перед задачей 17

### Task 17: Android — paywall и остаток

**Files:**
- Create: `android/app/src/main/java/com/zagir/splitty/ui/paywall/PaywallSheet.kt`
- Create: `android/app/src/main/java/com/zagir/splitty/ui/paywall/ReceiptStubs.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/expense/AddExpenseScreen.kt`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/expense/AddExpenseViewModel.kt`
- Modify: `android/app/src/main/res/values{,-ru,-es,-de,-fr}/strings.xml`

- [x] `PaywallSheet` — тот же дизайн средствами Compose и токенов темы Android
- [x] `ReceiptStubs` — счётчик-корешки
- [x] подпись остатка по тому же правилу (≤ 2, только free)
- [x] `ai_quota_exceeded` → paywall, черновик не теряется
- [x] строка «Splitor Plus» в настройках
- [x] строки на 5 языков — здесь же
- [x] написать тесты ViewModel
- [x] прогнать тесты — зелёные перед задачей 18

### Task 18: Сверка полноты локализации

**Files:**
- Modify: `android/app/src/main/res/values*/strings.xml`
- Modify: `ios/Splitty/Resources/`

- [x] сверить, что ни один ключ paywall/квоты не потерян ни в одном из 5 языков
- [x] проверить подстановку цены и периода форматтером стора (порядок слов разный по языкам)
- [x] прогнать существующий тест полноты ключей (или скрипт-сверку)
- [x] прогнать тесты — зелёные перед задачей 19

### Task 19: Документация API и переменных

**Files:**
- Modify: `docs/API.md`
- Modify: `README.md`

- [x] описать `/me/ai-quota`, `/me/subscription/*`, вебхуки, `/terms`, код `ai_quota_exceeded` и форму конверта с `quota` (docs/API.md:84)
- [x] описать переменные тарифа и ключи сторов
- [x] описать `X-Client-Version` и правило legacy-квоты, включая срок жизни ветки

### Task 20: Verify acceptance criteria

Автотестами (все зелёные):

- [x] **5 распознаваний за одну минуту → шестое даёт суточный отказ**, а не «слишком часто» (`TestRateLimiterDailyBeatsMinuteWhenEqual`)
- [x] free упирается на 6-м за сутки; Plus не упирается; минутный anti-abuse работает для обоих
- [x] тариф нельзя получить подменой ответа клиента: чужой/битый чек отвергается (`TestSubscriptionRejectsReceiptOfOtherAccount`, `TestSubscriptionReceiptErrors`)
- [x] **sandbox-чек не даёт Plus на проде** (`TestAppleVerifyRejectsSandboxOnProduction`, `TestGoogleVerifyRejectsTestPurchaseOnProduction`)
- [x] возврат снимает Plus: Apple — через ASSN, Google — через `voidedPurchaseNotification` (`TestGoogleVoidedPurchaseRevokesPlus`)
- [x] смена месяц↔год не плодит вторую запись (`TestSubscriptionSupersedesPreviousTokenOnPlanChange`)
- [x] потерянный вебхук: фоновый воркер досинхронизирует (`TestWorkerResyncsExpiredSubscription`)
- [x] ack не потерян при падении сервера сразу после валидации (`TestSubscriptionKeepsPendingAckWhenAcknowledgeFails`, `TestWorkerKeepsPendingWhenAckFails`)
- [x] опоздавший `EXPIRED` после `DID_RENEW` не гасит подписку (`TestAppleWebhookIgnoresStaleNotification`)
- [x] сборка без `X-Client-Version` получает legacy-квоту (`TestParseLegacyClientGetsLegacyQuota`)
- [x] тело 429 разбирается моделями 1.6 (`TestQuotaErrorBodyStaysParsableByOldClients`)
- [x] полный прогон `go test ./...` зелёный; собираются обе клиентские сборки

На живом стенде (локальный бэкенд + симулятор):

- [x] `/terms` открывается без авторизации и содержит всё требуемое 3.1.2
- [x] квота в успешном ответе `/parse`, `429 ai_quota_exceeded` с вложенным конвертом и `quota` рядом
- [x] `purchaseBindingToken` приезжает в `/me`
- [x] счётчик «Осталось 1 распознавание» под микрофоном — с ПРАВИЛЬНЫМ склонением
- [x] тап по счётчику открывает экран оплаты (`PaywallShotUITests`, зелёный)
- [x] на paywall есть всё обязательное по 3.1.2: автопродление, «Восстановить покупки», «Условия», «Конфиденциальность»

⚠️ **Не проверено локально: строки тарифов с ценами.** `xcodebuild test` из
командной строки не применяет StoreKit-конфиг схемы — storekitd уходит за
ценами в настоящий App Store и получает пустой ответ (в логе
«Requesting via MediaAPI»). На продукт это не влияет: в проде цены приходят из
App Store Connect. Проверять запуском из Xcode (там Run-экшен конфиг применяет)
либо в сандбоксе после заведения продуктов.

➕ Попутно исправлено: xcodegen пишет путь к StoreKit-конфигу на уровень выше
нужного (`../../` вместо `../../../` от каталога схемы). Чинится
`ios/scripts/patch-test-storekit.py` из `options.postGenCommand` — правка
переживает регенерацию проекта.

### Task 22: Секция подписки в профиле ➕

Отмечена сделанной в Task 15 по ошибке — кода не было. Всплыло при подготовке к
ревью: ревьюеру негде найти покупку, кроме как истратив пять распознаваний.

**Files:**
- Modify: `ios/Splitty/Features/Account/AccountView.swift`

- [x] секция «Подписка» с строкой Splitor Plus и статусом тарифа
- [x] «Управлять подпиской» ведёт в магазин (отменяет он, не приложение)
- [x] проверено на симуляторе: «Бесплатный тариф · 5 распознаваний в день»
- ⚠️ Android: такой же строки в профиле ещё нет

### Task 23: Переименование Splitty → Splitor ➕

В сторе приложение называется Splitor, а на устройстве и в текстах было
Splitty. Это несоответствие метаданных (Guideline 2.3.8) и прямой риск отказа.

- [x] `CFBundleDisplayName`, вордмарк на экране входа, 3 ключа iOS-каталога
- [x] `app_name` и 11–12 строк в каждом из пяти `values*/strings.xml`
- [x] обе сборки собираются

### Task 21: [Final] Документация и закрытие

- [x] `docs/API.md`: переменные тарифа, `/me/ai-quota`, `/me/subscription/*`, вебхуки, `/terms`, коды отказов, `X-Client-Version`
- [ ] перенести план в `docs/plans/completed/` — после ручной проверки покупки в сандбоксе

## Ревью: находка → где закрыта

| Находка (источник) | Закрыта |
|---|---|
| Минутный лимит перехватывает суточный, paywall не открывается (Codex, critical-path) | Решение 1; Task 4 + тест в Task 20 |
| Плоский конверт ошибки вместо вложенного (оба) | Решение 11; Task 9 |
| Чек не привязан к аккаунту (Opus) | Решение 2; Task 1, 8 |
| Sandbox-чек даёт Plus на проде (оба) | Решение 3; Task 6, 20 |
| Нет метода «текущий статус подписки» (Opus) | Task 6 `Status`, Task 7 `Status` |
| Google ack без состояния и ретрая (оба) | Решение 5; Task 7, 10, 11 |
| Ротация `purchaseToken` при смене плана (оба) | `LinkedRef`; Task 2, 8 |
| Переупорядоченные вебхуки (Codex) | Решение 4; `LastNotifiedAt`; Task 10 |
| Возвраты Google — `voidedPurchaseNotification` (Opus) | Task 10 |
| Свой grace 72 ч поверх сторового (Opus) | Решение 6; `PLUS_DELIVERY_SLACK` |
| Task 3 оставляет репозиторий несобираемым (оба) | Task 4 включает все вызовы и фейки |
| 1.6 получит 5/день без кнопки «купить» (оба) | Решение 7; `X-Client-Version` + legacy-квота |
| `remaining` уходит в минус (Opus) | Решение 1 + clamp; Task 4 |
| Ключ суток от локального времени (Opus) | Решение 9; Task 4 |
| `0 = безлимит` — fail-open (Opus) | Решение 8; Task 5 |
| `quota` нельзя класть в `ai.ParseResult` (Opus) | Решение 10; Task 9 |
| Ветка 502 не отдаёт квоту (Opus) | Task 9 |
| Резолв тарифа на горячем пути (Opus) | Кеш + фоновая досинхронизация; Task 5 |
| Стор недоступен → отбираем Plus (Opus) | Правило отказов; Task 5 |
| `transaction.finish()` до подтверждения (Opus) | Task 13 |
| Удаление аккаунта не чистит подписки (Opus) | Task 12 |
| Нет страницы Terms (оба) | Task 3 |
| `docs/API.md` не обновляется (оба) | Task 19 |
| Бейдж скидки соврёт при разных валютах (Opus) | Task 14 |
| Локализация отдельной задачей в конце (Opus) | Строки заводятся в Task 14/17 |
| `go-iap` как развилка в чеклисте (Opus) | Решение 12 — выбрано до старта |
| `AI_PLUS_DAILY_QUOTA` — лишняя ручка (Opus) | Оставлена: требование «рулит бэк»; `-1` убирает fail-open |

**Сознательно не принято:** предложение вынести вебхуки и/или дизайн paywall во
«вторую итерацию» (Opus предлагал убрать вебхуки, Codex — отложить дизайн).
Владелец решил делать целиком за один заход.

## Post-Completion

*Руками, вне репозитория.*

**⚠️ Блокеры, которые я сделать не могу:**

- **Paid Applications Agreement** в App Store Connect (Agreements, Tax, and
  Banking) — без него платные продукты не создаются вовсе. Нужны банковские и
  налоговые данные владельца; подписывается только тобой. Всё остальное в
  сторах упирается в это — стоит начать параллельно с кодом.
- **Google Play**: merchant-профиль и платёжный аккаунт.

**Настройка в консолях:**

- ASC: subscription group «Splitor Plus», продукты
  `com.zagir.splitty.plus.monthly` и `.plus.yearly`, цены, локализации ×5,
  скриншот для ревью, **отдельный** In-App Purchase key (`.p8`).
- ASC: App Store Server Notifications V2 → `https://splitor.zagirnur.dev/api/v1/webhooks/apple`
  (production и sandbox URL задаются раздельно).
- Play Console: подписка с двумя base plans; service account с правом просмотра
  финансовых данных; RTDN через Pub/Sub → `.../webhooks/google`;
  **отдельно включить `voidedPurchaseNotification`** — иначе возвраты не придут.
- Прод: залить новые переменные и ключи в `docker-compose.yaml` (секреты
  инлайном, `.env` там нет). До деплоя грепнуть прод-compose на
  `AI_PARSE_DAILY_QUOTA`.

**Порядок раскатки квоты:**

1. деплой бэкенда с `AI_LEGACY_DAILY_QUOTA=50` — старые клиенты не страдают;
2. выпуск 1.7 в оба стора;
3. после раскатки — опустить legacy-квоту до 5 и удалить ветку.

**Проверить перед сабмитом 1.7:**

- App Privacy: покупки привязывают `originalTransactionId` к аккаунту — сверить,
  не нужен ли тип «Purchases». Метки уже опубликованы, правка = новая публикация.
- Guideline 3.1.2 — самая частая причина реджекта подписок.
- Демо-аккаунт ревьюера: внести в `PLUS_COMP_USER_IDS` **и** описать в заметках,
  как тестировать покупку; учесть, что sandbox-чеки на проде отвергаются.
- Google Play: продакшен по-прежнему за гейтом закрытого теста (12 тестировщиков
  × 14 дней).

**Ручная проверка:**

- покупка → отмена → истечение → повторная покупка;
- смена месяц ↔ год на обеих платформах;
- Apple sandbox-возврат делается **не из UI**, а через App Store Server API
  (Test Notifications / refund в ASC) — заложить время на разбирательство;
- покупка на одном устройстве видна на втором после входа в тот же аккаунт;
- Family Sharing / один Apple ID на двоих — убедиться, что 409 объясним и
  человек понимает, что делать.
