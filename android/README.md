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
│   ├── auth/                           # Credential Manager (Google id-токен),
│   │                                   # Context.findActivity()
│   ├── model/                          # DTO по docs/API.md + тела запросов
│   ├── money/Money.kt                  # money(), shares(), aggregateByCurrency()
│   ├── network/                        # Retrofit API, интерцепторы, ApiException,
│   │                                   # NetworkMonitor (StateFlow isOnline)
│   └── session/
│       ├── SessionStore.kt             # DataStore: токен, baseUrl, dataVersion
│       ├── TokenCipher.kt              # AES-GCM поверх Keystore
│       └── PendingJoinStore.kt         # отложенное вступление по ссылке
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
    │                                   # MoneyText, GradientAvatar, Glossary,
    │                                   # FailedState, Haptics, AppToast,
    │                                   # NudgeHighlight, ReduceMotion, ZoomableImage
    ├── AppRoot.kt                      # сессия: логин ↔ главный экран
    ├── auth/                           # LoginScreen + LoginViewModel
    ├── groups/                         # список, деталь, дашборд, настройки,
    │                                   # создание, join, деталь операции
    ├── friends/, activity/, profile/, settleup/
    ├── expense/                        # форма расхода и весь AI-флоу:
    │   ├── AddExpenseScreen/ViewModel  # ручной + itemized-режим, undo, нуджи
    │   ├── ReceiptCard, ItemSheet,     # чек с позициями и правка позиций
    │   │   PersonBreakdownCard
    │   ├── AudioRecorderController,    # запись 16k/PCM16 → WAV
    │   │   MicTouchGesture,            # hold-to-talk (замок, отмена влево)
    │   │   RecordingOverlay
    │   ├── ReceiptCaptureController    # Photo Picker / камера, даунскейл
    │   └── transcribe/                 # караоке-транскрипт (за флагом)
    └── main/MainScaffold.kt            # bottom bar 5 позиций + офлайн-баннер
```

Ядро itemized-расчёта — `core/model/OperationItems.kt`: `derivedShares()`
зеркалит серверный `api.DeriveShares` (веса, фикс-суммы, surcharge,
канонизация), поэтому клиент и сервер сходятся до копейки; тесты портированы
из iOS и из серверного `internal/api/itemsplit_test.go`.

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
- 401 любого запроса — глобальный разлогин (уже сделан в `AuthInterceptor`);
  это `SessionEndReason.EXPIRED` — «сессия протухла», а не «человек вышел»
  (разница важна `OfflineDataCleaner`, см. «Вход и аккаунт»).
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

## Вход и аккаунт

Три способа попасть в аккаунт:

- **Google** (`POST /auth/google`) — системный лист выбора аккаунта через
  **Credential Manager** (`core/auth/GoogleIdTokenProvider.kt`, единственное
  место, где живёт SDK; наружу отдаётся голый id-токен). Кнопка — первой на
  экране входа: для человека без Telegram это единственный путь внутрь.
  Листу нужен контекст **активити** — `Context.findActivity()`
  (`core/auth/ActivityContext.kt`); если её не нашли, экран показывает ошибку,
  а не молчит.
- **Код из Telegram-бота** `@split_money_bot` (команда `/login`, код —
  8 символов, `POST /auth/code`).
- **dev-вход** (`POST /auth/dev`) — только в debug и при `API_DEV_AUTH=true`
  на сервере.

`BuildConfig.GOOGLE_SERVER_CLIENT_ID` (задан в `app/build.gradle.kts`) — это
**WEB**-клиент проекта Google Cloud, а не Android-клиент. Именно он попадает в
`aud` выданного id-токена и сверяется бэкендом (`GOOGLE_CLIENT_IDS`).
Android-клиенты (Play App Signing и локальный debug-keystore) в код не
попадают вовсе: Google сопоставляет приложение сам по package name + SHA-1
сертификата подписи. Отсюда типовая ошибка «вход работает в debug и падает в
release» — расходятся отпечатки подписи в Google Cloud, а не этот id.

### Способы входа и удаление аккаунта (вкладка «Профиль»)

- Секция **«Способы входа»** — по строке на провайдера, источник истины —
  `me.linkedProviders` **с сервера** (список не досочиняется на клиенте: каждая
  мутация приходит ответом `POST/DELETE /me/link/{provider}`). Google можно
  привязать и отвязать; Telegram показывается только уже привязанным (привязка
  требует Telegram Login Widget, которого в приложении нет); Apple на Android
  не показывается вовсе.
- Кнопка «Отвязать» гаснет **до** запроса, когда способ входа последний
  (сервер ответил бы `409 last_identity`, но узнавать о запрете из алерта
  после действия — плохо).
- Отвязка **Telegram необратима** (бот на следующее сообщение заведёт второй,
  пустой профиль; обратно привязать нельзя), поэтому у неё СВОЙ текст
  подтверждения, а серверный `warning` показывается отдельным диалогом
  «Внимание» — это не ошибка.
- Ошибки привязки переводит `identityErrorText` (`ui/components/HumanError.kt`):
  `identity_taken`, `identity_already_linked` (у аккаунта уже есть ДРУГАЯ личность
  этого провайдера — сервер её молча не подменяет), `last_identity`,
  `provider_rejected` (400 — сервер не принял id-токен провайдера; разлогина не
  вызывает, в отличие от 401).
- **«Удалить аккаунт»** — последним пунктом экрана (требование Apple Guideline
  5.1.1(v) и Google Play): `DELETE /me`, разлогин ТОЛЬКО при успехе — при
  сетевой ошибке аккаунт жив, и выбрасывать человека на экран входа значило бы
  соврать ему. FCM-токен отвязывается ДО удаления, пока JWT валиден.
- Разлогин по 401 (`AuthInterceptor`) — это **протухшая сессия**, а не выход:
  `SessionEndReason.EXPIRED`. `OfflineDataCleaner` в этом случае НЕ стирает
  отложенное вступление по ссылке (человек вернётся тем же аккаунтом);
  явный выход (`LOGOUT`) стирает всё. Что вернулся ИМЕННО ТОТ ЖЕ, проверяется по
  владельцу намерения: `PendingJoinStore` пишет рядом с кодом комнаты
  `pending_join_owner_id` (кто был в аккаунте, когда открыли ссылку), и на входе
  чужое намерение выбрасывается (`reconcileOwner`, порт iOS `adoptOwner`).
  Признак в памяти для этого не годится: между протуханием сессии и следующим
  входом процесс штатно умирает — вход уводит в системный лист.

## Ссылки-приглашения (диплинки)

- Форматы: app link `https://<domain>/join/<roomId>` (страницу отдаёт бэкенд,
  `internal/rest/deeplink.go`), кастомная схема `splitty://join/<roomId>`,
  легаси-ссылка бота `t.me/split_money_bot?start=room<roomId>` и голый код.
  Все четыре разбирает один парсер — `parseRoomCode` (`ui/groups/GroupsListViewModel.kt`).
- **Домен `splitty.app` в манифесте — плейсхолдер, он ещё не куплен.**
  До покупки `autoVerify` не пройдёт (нужен `/.well-known/assetlinks.json` с
  SHA-256 подписи), и реально работает только схема `splitty://`. Домен обязан
  совпасть с `PUBLIC_BASE_URL` бэкенда и с `applinks:` в `ios/project.yml`.
- **`android:launchMode="singleTop"`** обязателен: при `standard` переход по
  ссылке в ЖИВОЕ приложение создал бы второй экземпляр `MainActivity` и позвал
  `onCreate` вместо `onNewIntent` — два приложения в стеке, состояние первого
  потеряно.
- `MainActivity` намерение только **запоминает** (`PendingJoinStore`,
  DataStore — переживает выгрузку процесса; запись идёт в application-скоуп,
  не в `lifecycleScope`), интент помечается израсходованным (`setIntent` с
  очищенной `data` + пропуск `FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY`), иначе
  запуск из лаунчера повторно открывал бы ту же группу.
- Исполняет вступление `AppRootViewModel` (`ui/AppRoot.kt`): гость сначала
  видит экран входа, вступление доезжает само после входа. Намерение стирается
  ТОЛЬКО при успехе или терминальном отказе (404/403) — оффлайн и 5xx его не
  сжигают, а 401 оставляет его до переавторизации.
- Ссылку для «Поделиться» даёт сервер полем `inviteUrl` в
  `GET /rooms/{roomId}`; пока публичный домен не настроен, поле пустое и экран
  приглашения откатывается на легаси-ссылку бота.

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
