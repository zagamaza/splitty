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
- compileSdk/targetSdk 36, minSdk 26, applicationId `com.zagir.splitty`

## Сборка

Требуется JDK 17+ и Android SDK (путь — в `local.properties`: `sdk.dir=...`).

```bash
./gradlew :app:assembleDebug          # APK: app/build/outputs/apk/debug/
./gradlew :app:testDebugUnitTest      # юнит-тесты (обязательны перед коммитом)
./gradlew :app:verifyRoborazziDebug   # скриншот-тесты дизайн-системы
./gradlew :app:recordRoborazziDebug   # перезаписать эталоны после правок UI
./gradlew :app:lintDebug              # Android Lint
./gradlew :app:assembleRelease        # R8-сборка + verifyReleaseShrinking
```

Эталоны Roborazzi — 23 PNG в `app/src/test/snapshots/`, **закоммичены**:
`verifyRoborazziDebug` сверяет попиксельно, то есть ловит и падение рендера
Compose, и визуальную регрессию. Рендер прибит к Robolectric SDK 34 +
`Pixel5`-qualifiers, поэтому результат воспроизводим между машинами; после
осознанной правки UI эталоны перезаписываются `recordRoborazziDebug`.

Чего снапшоты НЕ покрывают (проверяется руками на устройстве): сверка
бок-о-бок с iOS-снапшотами и `docs/prototype/splitty-ai-proto.html`,
латентность жеста мика, TalkBack, 200% шрифт, слабый OEM — см. отчёт
приёмки `docs/plans/20260719-android-acceptance.md`.

## Релиз и раздача тестерам

- **R8 включён** в release (`isMinifyEnabled` + `isShrinkResources`).
  Всё рефлексивное (сериализаторы kotlinx, Retrofit-интерфейсы, Vosk)
  держится правилами в `app/proguard-rules.pro`. После `assembleRelease`
  автоматически идёт `verifyReleaseShrinking` — читает `mapping.txt` и
  падает, если R8 выкинул сериализаторы или точки входа (иначе поломка
  всплыла бы только в рантайме у тестера).
- **Подпись**: `keystore.properties` в корне `android/` (в .gitignore) —
  `storeFile`, `storePassword`, `keyAlias`, `keyPassword`. Без файла
  release собирается неподписанным.
- **Firebase App Distribution**: `firebase.properties` в корне `android/`
  (в .gitignore) — `appId`, `groups` (по умолчанию `testers`),
  `serviceCredentialsFile` (путь к service-account json). Раздача:

  ```bash
  ./gradlew :app:assembleRelease :app:appDistributionUploadRelease
  ```

  Заметки к сборке — `app/release-notes.txt`.
- **CI**: `.github/workflows/android.yml` (только на изменения в `android/`) —
  юнит-тесты, Roborazzi, lint, `assembleDebug` и `assembleRelease` с R8-smoke.

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

Адрес по умолчанию зависит от варианта (`SessionStore.DEFAULT_BASE_URL`):
в debug — дев-сервер по HTTP, в release — HTTPS-плейсхолдер прод-домена.
Поле «Сервер» на экране входа доступно только в debug (для локального
бэкенда на эмуляторе — `http://10.0.2.2:7171`).

Cleartext HTTP разрешён **только в debug**: конфиги разные по вариантам —
`src/debug/res/xml/network_security_config.xml` и
`src/release/res/xml/network_security_config.xml` (release cleartext
запрещает, боевой сервер обязан быть на HTTPS).

Вход: код из Telegram-бота `@split_money_bot` (команда `/login`, код —
8 символов) либо dev-вход (`POST /auth/dev`, только в debug и при
`API_DEV_AUTH=true` на сервере).

## Состояние

Все экраны реализованы: группы (список, деталь, дашборд, настройки, создание,
join), друзья (список + деталь), активность, профиль и уведомления,
погашение долгов, форма расхода (equally / by_exact_amount / itemized).

AI-флоу на паритете с iOS: hold-to-talk с оверлеем записи, фото чека,
`POST /rooms/{id}/operations/parse`, чек с позициями (`ReceiptCard`),
правка позиций (`ItemSheet`), undo и нуджи. Караоке-транскрипт готов, но
выключен флагом `BuildConfig.KARAOKE_TRANSCRIPT` — ждёт прогона на железе.

Осознанно отложено: WorkManager для outbox (сейчас досылка только при живом
приложении), type-safe навигация, Room вместо файловых сторов, Coil,
дисковый кеш аватаров, публикация в Play Console.
