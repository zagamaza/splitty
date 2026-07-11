# Splitty Android

Android-клиент Splitty (клон iOS-приложения `ios/`, UX Splitwise,
«финтех-минимализм») поверх REST API — контракт `docs/API.md` в корне
репозитория. Язык интерфейса — русский. Дизайн-референс:
`ios/docs/UX_SPEC.md`, `ios/Splitty/Core/{Theme,DesignSystem}.swift`.

## Стек

- Kotlin 2.x, Jetpack Compose + Material 3 (dynamic color выключен — фиксированная палитра)
- Single-activity, Navigation Compose
- Hilt (DI), Retrofit + kotlinx.serialization, OkHttp
- DataStore Preferences (токен, адрес сервера)
- MVVM + UDF: `StateFlow`, sealed `UiState` (`core/UiState.kt`)
- Gradle version catalog (`gradle/libs.versions.toml`), wrapper 8.14
- compileSdk/targetSdk 35, minSdk 26, applicationId `com.zagir.splitty`

## Сборка

Требуется JDK 17+ и Android SDK (путь — в `local.properties`: `sdk.dir=...`).

```bash
./gradlew :app:assembleDebug          # APK: app/build/outputs/apk/debug/
./gradlew :app:testDebugUnitTest      # юнит-тесты (деньги, DTO, код входа)
```

## Структура

```
app/src/main/java/com/zagir/splitty/
├── SplittyApp.kt, MainActivity.kt      # Hilt-Application, единственная Activity
├── core/
│   ├── UiState.kt                      # Loading / Content / Error
│   ├── model/                          # DTO по docs/API.md + тела запросов
│   ├── money/Money.kt                  # money(), shares(), aggregateByCurrency()
│   ├── network/                        # Retrofit API, интерцепторы, ApiException,
│   │                                   # NetworkMonitor (StateFlow isOnline)
│   └── session/SessionStore.kt         # DataStore: токен, baseUrl, dataVersion
├── data/
│   ├── SplittyRepository.kt            # единая точка сети для ViewModel
│   ├── ApiCache.kt                     # офлайн-кеш GET (filesDir/cache-api)
│   ├── OutboxStore.kt                  # очередь офлайн-расходов (outbox.json)
│   ├── OutboxSyncer.kt                 # FIFO-досылка outbox (clientOpId)
│   └── OfflineDataCleaner.kt           # очистка кеша/outbox при разлогине
├── di/                                 # Hilt-модули (DataStore, OkHttp/Retrofit)
└── ui/
    ├── theme/Theme.kt                  # семантические токены + SplittyTheme
    ├── components/                     # SurfaceCard, PrimaryPillButton, SoftChip,
    │                                   # MoneyText, MoneyTotalsText, GradientAvatar
    ├── AppRoot.kt                      # сессия: логин ↔ главный экран
    ├── auth/                           # LoginScreen + LoginViewModel
    └── main/MainScaffold.kt            # bottom bar 5 позиций + офлайн-баннер
```

## Ключевые конвенции (обязательны для экранных агентов)

- **Цвета** — только токены `Splitty.colors.*` (сырые hex запрещены);
  правило денег: `>0` accent, `<0` negative, `0` inkSecondary.
- **Деньги** — только через `MoneyText`/`MoneyTotalsText`; суммы целые (`Int`),
  формат «1 234 567 ₽»; валюты никогда не складываются между собой
  (`aggregateByCurrency`).
- **Сеть** — только через `SplittyRepository`; ошибки — `ApiException`
  с русским `message` (показывать как есть). После каждой успешной мутации —
  `SessionStore.noteDataChanged()`; экраны-списки подписываются на
  `SessionStore.dataVersion` и перезагружаются.
- 401 любого запроса — глобальный разлогин (уже сделан в `AuthInterceptor`).
- Экраны: `ViewModel` + `StateFlow<UiState<T>>`, секции — `SurfaceCard`
  на фоне `Splitty.colors.bg`.

## Офлайн-режим (дизайн v1, паритет с iOS)

- **Read-кеш**: ключевые GET (`rooms`, `room/{id}`, `friends`, `activity`
  стр. 1, `statistics/{id}`, `currencies`, `me`) возвращают `Fetched<T>`
  (`value`, `fromCache`): успех сети пишется в `ApiCache`
  (filesDir/cache-api, атомарная запись), транспортная ошибка отдаёт кеш —
  экраны с кешем офлайн не показывают ошибку.
- **Outbox**: расход офлайн (или при обрыве POST) → `OutboxStore`
  (outbox.json); POST всегда шлёт `clientOpId` = localId (идемпотентность,
  docs/API.md). `OutboxSyncer` досылает FIFO по триггерам: сеть появилась,
  onStart, pull-to-refresh; 4xx (кроме 401) → `failed(текст)`, сеть/5xx →
  pending. Неотправленные операции — сверху экрана группы с бейджем
  «не отправлено», тап — правка локальной записи (роут-параметр `localId`).
- Правка синхронизированных операций офлайн недоступна (плашка в форме),
  погашения офлайн — алерт «Недоступно офлайн».
- Глобальный баннер в `MainScaffold`: «Офлайн…»/«Отправка…»
  (`NetworkMonitor.isOnline`, `OutboxSyncer.isSyncing`).
- Logout чистит кеш и outbox (`OfflineDataCleaner` по исчезновению токена);
  при непустом outbox подтверждение выхода предупреждает об удалении.

## Сервер

Дефолтный адрес — `http://138.124.18.189:18002` (меняется на экране входа,
поле «Сервер»; для локального бэкенда на эмуляторе — `http://10.0.2.2:7171`).
Cleartext HTTP разрешён через `res/xml/network_security_config.xml`.
Вход: код из Telegram-бота `@split_money_bot` (команда `/login`) либо
dev-вход (`POST /auth/dev`, работает только при `API_DEV_AUTH=true` на сервере).

## Что ещё не сделано (для следующих агентов)

Вкладки главного экрана — заглушки (см. `TODO(screens)` в `MainScaffold.kt`):

- Друзья: список балансов (`GET /friends`) + деталь друга
- Группы: список (`GET /rooms`), деталь группы, балансы/итоги, создание/join,
  настройки (валюта, архив)
- Форма добавления расхода (equally / by_exact_amount, предпросмотр долей `shares()`)
- Активность: лента (`GET /activity`, пагинация limit/offset)
- Профиль: `GET/PATCH /me`, выход
- Settle up: погашение долгов (`POST /rooms/{id}/repayments`)
