# Splitty iOS

SwiftUI-клиент (iOS 17+) для Splitty с UX в стиле Splitwise. Работает поверх REST API бэкенда (см. `../docs/API.md`).

## Быстрый старт

1. Поднять MongoDB и бэкенд с dev-авторизацией:

   ```bash
   docker compose up -d mongo
   API_DEV_AUTH=true TG_TOKEN= go run ./cmd/splitty
   ```

   REST API слушает `localhost:7171` (env `LISTEN`).

2. Сгенерировать проект и открыть в Xcode:

   ```bash
   cd ios
   xcodegen generate        # brew install xcodegen
   open Splitty.xcodeproj
   ```

   На чистом клоне первая генерация и первая сборка требуют **сети**: SPM тянет
   `firebase-ios-sdk` (пуши) и `GoogleSignIn-iOS` (вход через Google — SDK нужен
   ровно ради подписанного id-токена, проверяет его наш бэкенд).

3. Запустить на симуляторе (`⌘R`). На экране входа использовать «Вход для разработки»:
   любое положительное число и имя. Поле подписано «Telegram ID» исторически, но
   `POST /auth/dev` резолвит пользователя по `_id` — это **номер Splitty**, а не id
   в Telegram, и dev-пользователи заводятся вообще без `telegram_id` (следствия
   ожидаемые: telegram-уведомления им не идут, `/users/{id}/avatar` отдаёт 404 и
   клиент рисует инициалы). Адрес сервера по умолчанию — `http://127.0.0.1:7171`
   (меняется в DisclosureGroup «Сервер» на экране входа).

### Что нужно за пределами Xcode

- **Sign in with Apple** — capability обязана быть включена для `com.zagir.splitty`
  в Apple Developer. Без неё подпись падает на «provisioning profile doesn't support
  Sign in with Apple», а без самого входа через Apple ревью отклонит билд
  (Guideline 4.8: вход через Google требует равноценной альтернативы).
- **URL-схема `splitty://`** (`CFBundleURLTypes` в `project.yml`) — по ней работает
  кнопка «Открыть в приложении» на странице приглашения `https://<домен>/join/<roomId>`
  (`internal/rest/deeplink.go`). Схема нужна потому, что тап по ссылке на ТОТ ЖЕ
  домен iOS в приложение не уводит; и она единственное, что работает, пока домен
  под universal links не куплен. Проверка на симуляторе:

  ```bash
  xcrun simctl openurl booted splitty://join/<roomId>
  ```

## Сборка и тесты из CLI

```bash
cd ios && xcodegen generate
xcodebuild -project Splitty.xcodeproj -scheme Splitty \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' build
xcodebuild test -project Splitty.xcodeproj -scheme Splitty \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro'
```

`SplittyTests` — юнит-тесты (деньги и деление долей, черновики позиций, статистика,
офлайн-кеш и outbox, AI-флоу формы расхода) плюс `SnapshotRenderTests`, который рендерит
ключевые экраны в PNG в `/tmp/splitty-snapshots/` для ручной визуальной сверки — эталонов
у него нет, ассерты минимальные. `SplittyUITests` — сквозной
демо-прогон по всем экранам против локального бэкенда с seed-данными
(снимает скриншоты каждого шага в result bundle).

## Структура

- `Splitty/App` — вход, RootView (логин/табы), таб-бар с центральной кнопкой «+».
- `Splitty/Core` — дизайн-токены (цвета Splitwise), деньги/даты, Codable-модели,
  APIClient (все эндпоинты), SessionStore (JWT в Keychain).
- `Splitty/Features` — экраны: Группы, Расход, Погашение долга, Друзья, Активность, Профиль, Логин.
- `docs/UX_SPEC.md` — спецификация UX и архитектурные конвенции.

Проект генерируется XcodeGen из `project.yml` — `Splitty.xcodeproj` не коммитится.
