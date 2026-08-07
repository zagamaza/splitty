# Splitty (iOS) → App Store Connect / TestFlight

Полная инструкция по заливке. Всё headless, без GUI Xcode.

## Идентификаторы

| Что | Значение |
|---|---|
| Bundle ID | `com.zagir.splitty` (ASC id `WHJCASA6A5`) |
| Team ID | `K8922Y6R3M` |
| App в ASC | `Splitttty`, app id `6787746052` |
| Issuer ID | `a30d44ef-0dc4-4c01-bb7b-7235968f61f8` |
| ASC API Key | `T6PMYHX4T7` (рабочий для distribution), запасной `CZ62LFKG7N` |
| Internal-группа | `Internal`, id `30552ae6-0874-42da-a579-a1a80f8d3073` |
| Public-группа | `Public`, id `328fc762-514b-460c-ba01-5c4e9aed5faa`, ссылка `https://testflight.apple.com/join/pX8rSTns` |

Проект: XcodeGen (`ios/project.yml`), схема `Splitty`, конфиг `Release`.
Подпись — automatic + cloud signing через ASC API-ключ.

## Предустановка на новой машине

```bash
# 1. ASC API-ключи — этого достаточно для всего остального
mkdir -p ~/.appstoreconnect/private_keys
cp keys/AuthKey_*.p8 ~/.appstoreconnect/private_keys/
chmod 600 ~/.appstoreconnect/private_keys/*.p8

# 2. Provisioning-профили (опционально — cloud signing выпустит свои)
mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
cp profiles/*.mobileprovision ~/Library/MobileDevice/Provisioning\ Profiles/

# 3. Инструменты
brew install xcodegen        # + Xcode и Command Line Tools
```

### Про сертификаты подписи

**Отдельный .p12 не нужен.** Подпись идёт через *cloud signing*: `xcodebuild` с флагами
`-allowProvisioningUpdates -authenticationKeyPath/-KeyID/-KeyIssuerID` сам выпускает
distribution-сертификат и provisioning-профиль через ASC API. Именно так собраны все
билды 1.0(1) … 1.2(5). На чистой машине достаточно `.p8` из `keys/`.

Приватные ключи существующих сертификатов **невозможно выгрузить через CLI** — macOS
защищает их в keychain (`security export` отдаёт `The contents of this item cannot be
retrieved`). В `certs/certs-only.p12` лежат только *публичные* сертификаты — для подписи
они непригодны, оставлены для справки.

Если всё же нужен именно перенос существующего сертификата (а не выпуск нового):
Keychain Access → найти `Apple Distribution: Zagir Nurgaliev (K8922Y6R3M)` → правой кнопкой →
Export → `.p12` (потребует пароль от учётки Mac). Это единственный рабочий путь, и он ручной.

Проверка после сборки: `security find-identity -v -p codesigning` — должны появиться
`Apple Distribution: Zagir Nurgaliev (K8922Y6R3M)` и `Apple Development: …`.

## Заливка билда

Переменные (один раз в шелле):

```bash
export ASC_KEY=T6PMYHX4T7
export ASC_ISSUER=a30d44ef-0dc4-4c01-bb7b-7235968f61f8
export ASC_P8=~/.appstoreconnect/private_keys/AuthKey_$ASC_KEY.p8
cd /path/to/splitty/ios
```

Шаги 2–5 целиком делает `make ios-publish` из корня репозитория (xcodegen →
archive → export → altool, всё по API-ключу). Ниже — что происходит внутри и
что чинить, если упало.

### 1. Поднять номер сборки

Номер обязан строго расти и быть уникальным, иначе ASC отклонит («build already exists»).
Правится в `ios/project.yml` → `CFBundleVersion`. Последний залитый — **5** (версия 1.2).

```bash
grep -E "CFBundleShortVersionString|CFBundleVersion" project.yml
# поднять CFBundleVersion на следующий номер
```

### 2. Генерация проекта

```bash
xcodegen generate
```

### 3. Архив (Release)

```bash
rm -rf build/Splitty.xcarchive build/export
xcodebuild -project Splitty.xcodeproj -scheme Splitty -configuration Release \
  -archivePath build/Splitty.xcarchive -destination 'generic/platform=iOS' \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_P8" \
  -authenticationKeyID "$ASC_KEY" \
  -authenticationKeyIssuerID "$ASC_ISSUER" \
  archive
```

### 4. Экспорт IPA

`ExportOptions.plist` лежит в `ios/` (method: `app-store`, signingStyle: `automatic`).

```bash
xcodebuild -exportArchive -archivePath build/Splitty.xcarchive \
  -exportOptionsPlist ExportOptions.plist -exportPath build/export \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_P8" \
  -authenticationKeyID "$ASC_KEY" \
  -authenticationKeyIssuerID "$ASC_ISSUER"
# на выходе: build/export/Splitty.ipa
```

### 5. Загрузка

```bash
xcrun altool --upload-app -f build/export/Splitty.ipa -t ios \
  --apiKey "$ASC_KEY" --apiIssuer "$ASC_ISSUER"
```

Успех = `UPLOAD SUCCEEDED with no errors`. Обработка Apple ~5–15 мин.

### 6. Привязать билд к внутреннему тесту

Внутренний тест не требует Beta Review — тестеры получают билд сразу.
Скрипт `asc.py` (в архиве) уже настроен на нужный issuer/ключи.

```bash
python3 asc.py attach-build <BUILD_VERSION>   # напр. 6
```

Или вручную через API: `POST /v1/betaGroups/30552ae6-.../relationships/builds`
с телом `{"data":[{"type":"builds","id":"<build-uuid>"}]}`.

## Sign in with Apple (обязателен с появлением входа через Google)

### Что включить в консолях

**Кто может это сделать.** Достаточно роли **Admin** в команде `K8922Y6R3M` — Admin видит
Keys и Identifiers, включает capability и выпускает ключи. Account Holder нужен только для
состава команды и финансов, здесь он не требуется. Учётка обязана быть **членом команды**:
Apple ID, не состоящий в программе, получит на `/account/resources/identifiers/list`
«Access Unavailable», и никакие права тут не помогут — проверять надо именно тот Apple ID,
на который пришло приглашение, а не любой свой.

1. **Apple Developer Portal → Identifiers → App ID `com.zagir.splitty`** (Team `K8922Y6R3M`):
   включить capability **Sign in with Apple** → Save. Без неё Apple не выпустит ID-токен, а
   `xcodebuild` не подпишет билд с соответствующим entitlement.
   **⚠️ Порядок важен: capability включается ДО следующей сборки.** Профиль перевыпускать
   руками не нужно — сборка идёт через cloud signing (`-allowProvisioningUpdates` + ASC API,
   см. «Про сертификаты подписи» выше), и `xcodebuild` подтянет обновлённый профиль сам. Но
   если собрать раньше, чем включена capability, подпись упадёт на
   «provisioning profile doesn't support Sign in with Apple» (этот случай описан и в
   комментарии `ios/project.yml`).
2. **Apple Developer Portal → Keys**: `+` → имя (напр. `Splitty SignIn`) → отметить
   **Sign in with Apple** → рядом **Configure** → Primary App ID `com.zagir.splitty` → Save →
   Continue → Register → **Download**.
   **Файл отдаётся ровно один раз**, повторно скачать его нельзя — потерял, значит выпускай
   новый и отзывай старый.
   - Team ID (`Membership details`, вверху справа) → `APPLE_TEAM_ID` = `K8922Y6R3M`;
   - Key ID (10 символов, на странице ключа) → `APPLE_KEY_ID`;
   - **содержимое** `AuthKey_XXXX.p8` целиком, вместе со строками
     `-----BEGIN PRIVATE KEY-----` / `-----END PRIVATE KEY-----` (не путь к файлу) →
     `APPLE_PRIVATE_KEY`. Годится и однострочная форма с экранированными `\n` — парсер их
     нормализует;
   - bundle id `com.zagir.splitty` → `APPLE_CLIENT_IDS`.
   **⚠️ `APPLE_CLIENT_IDS` разделяется двоеточием, и порядок значим.** `client_secret` для
   `auth/token` и `auth/revoke` подписывается на **первый** элемент списка
   (`cmd/splitty/main.go`, `clientIDs[0]`). Пока значение одно — неважно; если когда-нибудь
   добавится Services ID для веба, **bundle id обязан остаться первым**, иначе отзыв токенов
   при удалении аккаунта молча перестанет работать — то самое требование 5.1.1(v), ради
   которого ключ и заводится.
3. Переменные едут в `.env` на сервере (проброс в контейнер уже прописан в
   `docker-compose.yml`). **`.p8` и его содержимое в git не коммитятся** — см.
   раздел «⚠️ Безопасность» ниже, правило то же, что для ключей ASC.

**Со стороны проекта всё готово:** entitlement `com.apple.developer.applesignin: [Default]`
уже лежит в `ios/project.yml` и `ios/Splitty/Splitty.entitlements`, править ничего не нужно.
Пустой `APPLE_PRIVATE_KEY` не ломает сборку и не ломает вход — выключается только обмен
`authorizationCode`, то есть отзыв токенов при удалении аккаунта.

### Требования App Store, из-за которых это всё делается

- **Guideline 4.8 (Login Services).** Приложение предлагает вход через Google — значит обязано
  предложить и равноценную альтернативу, ограничивающую сбор данных. Sign in with Apple ей
  является; без него ревью отклонит билд.
- **Guideline 5.1.1(v) (Account Deletion).** Приложение с регистрацией обязано давать удалить
  аккаунт **из самого приложения**, а не «напишите в поддержку». В Splitty это последняя карточка
  вкладки **Профиль** (`ios/Splitty/Features/Account/AccountView.swift`, `deleteAccountSection`):
  прокрутка вниз → «Удалить аккаунт» → подтверждение → `DELETE /api/v1/me`. Там же, выше по
  экрану, карточка **«Способы входа»** (`loginMethodsSection`) — привязка и отвязка Google и Apple.
  Telegram там **только отвязывается**, и строка появляется, лишь когда он уже привязан: привязка
  требует Telegram Login Widget (подписанные ботом `id`/`auth_date`/`hash`), которого в приложении
  нет, — рисовать неработающую кнопку «Привязать» значит обещать несуществующее.
- **Отзыв токенов при удалении — часть 5.1.1(v).** Для аккаунтов, созданных через Apple, недостаточно
  удалить свои данные: нужно отозвать выданные токены, иначе приложение так и останется висеть в
  **Настройки → Apple ID → Вход с Apple**. Бэкенд делает это сам:
  - клиент присылает `authorizationCode` и при ВХОДЕ, и при ПРИВЯЗКЕ Apple к существующему
    аккаунту (`POST /api/v1/me/link/apple`), сервер меняет его на refresh token
    (`POST https://appleid.apple.com/auth/token`) и хранит в профиле. Привязка без кода оставила
    бы `apple_sub` без refresh token — и отзывать при удалении было бы нечем;
  - при `DELETE /api/v1/me` сервер первым шагом зовёт
    **`POST https://appleid.apple.com/auth/revoke`** — строго до tombstone, потому что тот
    вычищает сам токен.
  - **Обе операции подписываются client_secret'ом на `.p8`-ключе Apple**, то есть требуют
    заполненных `APPLE_TEAM_ID` + `APPLE_KEY_ID` + `APPLE_PRIVATE_KEY`. Пустой ключ не ломает ни
    вход, ни удаление аккаунта (обе ветки best-effort, в лог уходит warning) — но отзыв **не
    произойдёт**, и ревьюер увидит приложение в списке «Вход с Apple» после удаления аккаунта.

### Что проверить перед отправкой на ревью

- Кнопка удаления аккаунта видна на вкладке «Профиль» без переходов по подэкранам и реально
  удаляет (не «деактивирует»).
- Создать тестовый аккаунт через Apple → удалить его в приложении → **Настройки → Apple ID → Вход
  с Apple**: Splitty из списка обязан пропасть. Это единственное видимое подтверждение, что
  `auth/revoke` отработал и ключ настроен верно.
### Что писать в App Review Information

Ревьюеру НЕ нужен ни Telegram, ни Google/Apple-аккаунт: он входит по email и паролю —
обычной формой на главном экране, без раскрытия секций. Вход по коду из бота с экрана
убран, вместе с ним ушёл и прежний путь ревью через `REVIEW_LOGIN_CODE`.

Заполнять так (Sign-in required: Yes):

| Поле     | Значение                          |
|----------|-----------------------------------|
| Username | `review@splitor.zagirnur.dev`     |
| Password | пароль демо-аккаунта              |

> Notes: Sign in with the email and password above — the form is on the main screen,
> no third-party account needed. The demo account already has groups and expenses.

#### Как завести демо-аккаунт (разово)

Демо-аккаунт **регистрируется заново**, а не переделывается из старого. Причина в том,
что адрес входа (`login_email`) заводится ТОЛЬКО регистрацией: `POST /me/password` кладёт
один хеш, а `hasPasswordLogin` требует и адрес, и хеш — пароль, поставленный аккаунту без
адреса, войти не даёт. Отдельного API «привязать email к существующему аккаунту» нет.

```bash
# 1. Регистрируем демо-аккаунт на проде и запоминаем его id
curl -s -X POST https://splitor.zagirnur.dev/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"review@splitor.zagirnur.dev","password":"<пароль>","displayName":"Demo"}'
```

2. Прописать полученный `user.id` в `REVIEW_USER_ID` на сервере и передеплоить. Это и есть
   защита от удаления: `DELETE /me` для него отвечает `403`, чтобы ревьюер не убил демо-вход
   следующему ревью (см. `delete_account.go`). `REVIEW_LOGIN_CODE` можно оставить пустым —
   без кода валидация конфига проходит, а вход по коду с экрана всё равно убран.

3. Наполнить аккаунт данными: войти под ним в приложении и создать пару групп с расходами —
   ревьюер должен увидеть не пустой экран. (`scripts/seed-local.py` для этого не годится:
   он ходит в `/auth/dev`, а на проде dev-вход выключен.)

Проверить перед отправкой (иначе ревью встанет на первом же шаге):

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://splitor.zagirnur.dev/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"review@splitor.zagirnur.dev","password":"<пароль>"}'
```

Ждём `200`. `401` — пара не сходится: сервер намеренно отвечает одинаково на неизвестный
адрес и на неверный пароль, так что проверять надо оба.

Google и Apple тоже доступны, но для проверки не обязательны.

## Грабли (важно)

- **Только altool + API-ключ.** Через Xcode-аккаунт/Organizer в headless НЕ работает.
- **Ключ для distribution — `T6PMYHX4T7`.** `CZ62LFKG7N` даёт `Cloud signing permission error`
  на экспорте app-store (прав не хватает), но годится для dev-подписи и регистрации устройств.
- **Иконка обязательна.** Без `ASSETCATALOG_COMPILER_APPICON_NAME: AppIcon` и
  `CFBundleIconName: AppIcon` (оба уже в `project.yml`) ASC отклоняет билд с ошибками 90022/90713.
- **Export compliance.** `ITSAppUsesNonExemptEncryption: false` прописан в `project.yml` —
  без него ASC будет спрашивать про шифрование при каждом билде.
- **Внешний (публичный) тест требует Beta App Review.** Пока не одобрен, публичная ссылка
  отдаёт «Beta not found». Билд нельзя привязать к внешней группе до одобрения.
- **Внутренний тест — мгновенный.** Тестеры должны быть пользователями команды ASC.
- iOS-сборки на этой машине: destination `iPhone 16 Pro, OS=18.5` (`OS=latest` падает).

## Установка на физическое устройство (dev)

```bash
xcrun devicectl list devices                 # найти id устройства
# зарегистрировать UDID в портале (asc.py register-device <UDID> <Name>)
xcodebuild -project Splitty.xcodeproj -scheme Splitty -configuration Debug \
  -destination "id=<DEVICE_ID>" -derivedDataPath build/DerivedData \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_P8" -authenticationKeyID "$ASC_KEY" \
  -authenticationKeyIssuerID "$ASC_ISSUER" build
xcrun devicectl device install app --device <DEVICE_ID> \
  build/DerivedData/Build/Products/Debug-iphoneos/Splitty.app
```

Требования на телефоне: **Developer Mode включён** (Настройки → Конфиденциальность и
безопасность → Режим разработчика, затем перезагрузка) и **телефон разблокирован**
в момент установки — иначе `developer disk image could not be mounted`.

Зарегистрированные устройства: `Maza` (`00008130-00160D283CD2001C`),
`iPhone Aleksander` (`00008110-001451A21AD9801E`), `imbir1`.

## Тестеры

Внутренняя группа `Internal`: `zagirnur@gmail.com`, `almazic91@gmail.com`.

Добавить нового: `python3 asc.py add-tester <email> <FirstName> <LastName>`

## ⚠️ Безопасность

`keys/` и `certs/` — приватные ключи, дающие полный доступ к Apple Developer аккаунту
(выпуск сертификатов, заливка билдов от твоего имени). Не коммитить в git,
не выкладывать. При компрометации — отозвать ключи в ASC → Users and Access →
Integrations и перевыпустить сертификаты.

То же и **строже** — про `.p8`-ключ Sign in with Apple (`APPLE_PRIVATE_KEY`): им подписывается
client_secret к token-эндпоинтам Apple. Ни файл, ни его содержимое **не коммитятся в git** — ни в
`docker-compose.yml`, ни «временно, чтобы проверить». Ключ живёт только в `.env` на сервере
(`$REMOTE_DIR`, по умолчанию `/root/splitty` — см. `Makefile`), как `TG_TOKEN` и `API_JWT_SECRET`. Apple отдаёт файл один раз; при
компрометации — Developer Portal → Keys → Revoke и выпуск нового.
