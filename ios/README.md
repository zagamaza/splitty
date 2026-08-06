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

3. Засеять локальный бэкенд (`make seed` из корня — нужен запущенный сервер с
   `API_DEV_AUTH=true`) и запустить на симуляторе (`⌘R`). Вход — по email и паролю:
   `ui-tests@splitty.test` / `20260806`, скрипт заводит эту учётку вместе
   с демо-группами. Остальные участники групп создаются через `POST /auth/dev`:
   он резолвит пользователя по `_id` (**номер Splitty**, не id в Telegram), такие
   аккаунты живут без `telegram_id` — telegram-уведомления им не идут, а
   `/users/{id}/avatar` отдаёт 404, и клиент рисует инициалы.

   Адрес сервера по умолчанию — прод. Локальный подставляется либо переменной
   окружения `SPLITTY_BASE_URL` (её же используют UI-тесты), либо руками в поле
   «Сервер»: оно **скрыто**, чтобы не мозолить глаза на экране входа, и
   появляется после пяти тапов по логотипу «Splitty» (только DEBUG).

### Что нужно за пределами Xcode

- **Sign in with Apple** — capability обязана быть включена для `com.zagir.splitty`
  в Apple Developer. Без неё подпись падает на «provisioning profile doesn't support
  Sign in with Apple», а без самого входа через Apple ревью отклонит билд
  (Guideline 4.8: вход через Google требует равноценной альтернативы).
- **URL-схема `splitty://`** (`CFBundleURLTypes` в `project.yml`) — по ней работает
  кнопка «Открыть в приложении» на странице приглашения `https://<домен>/join/<roomId>`
  (`internal/rest/deeplink.go`). Схема нужна потому, что тап по ссылке на ТОТ ЖЕ
  домен iOS в приложение не уводит — схема живёт рядом с universal links
  (`applinks:splitor.zagirnur.dev`), а не вместо них. Проверка на симуляторе:

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

UI-тестам нужен локальный бэкенд на `127.0.0.1:7171` с `API_DEV_AUTH=true` и данные,
которые они ищут по имени (группа «Поездка в Стамбул» с расходом «Ужин в ресторане»), —
всё это делает `make seed` (`scripts/seed-local.py`, повторный запуск ничего не дублирует).
Каждый класс логинится сам, по email и паролю из того же скрипта, — порядок прогона не важен.

Пароль тестовой учётки — **из одних цифр**, и это не лень: `typeText` набирает через
клавиатуру симулятора, а та берёт активную раскладку. На русской раскладке латиница
приезжает кириллицей (`splittytest` → `ыздшееуые`), сервер отвечает 401, и падение
выглядит как «неверная учётка» — хотя учётка верная. Цифры одинаковы в любой раскладке.
Поле Email от этого защищено само: у него `.keyboardType(.emailAddress)`, там всегда латиница.

`OfflineSmokeUITests` — исключение: его три теста запускаются по одному
(`-only-testing:SplittyUITests/OfflineSmokeUITests/<тест>`), а бэкенд между ними
останавливается и запускается снаружи — `testOnlineWarmup` (сервер жив) →
`testOfflineCacheAndQueue` (сервер остановлен) → `testAfterSync` (сервер снова жив).
Бэкенд при этом обязан стартовать с ФИКСИРОВАННЫМ `API_JWT_SECRET`: пустой секрет
при `API_DEV_AUTH=true` генерируется случайным на каждый старт, и после рестарта
токен из первого шага получает 401 — приложение разлогинивается, а выглядит это
как провал офлайн-режима.

## Структура

- `Splitty/App` — вход, RootView (логин/табы), таб-бар с центральной кнопкой «+».
- `Splitty/Core` — дизайн-токены (цвета Splitwise), деньги/даты, Codable-модели,
  APIClient (все эндпоинты), SessionStore (JWT в Keychain).
- `Splitty/Features` — экраны: Группы, Расход, Погашение долга, Друзья, Активность, Профиль, Логин.
- `docs/UX_SPEC.md` — спецификация UX и архитектурные конвенции.

Проект генерируется XcodeGen из `project.yml` — `Splitty.xcodeproj` не коммитится.
