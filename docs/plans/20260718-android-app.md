# Splitty Android: догнать iOS (доводка существующего клиента)

> Ревизия 4 (2026-07-18). Рамка: в репо УЖЕ есть качественный
> Android-клиент (`android/`, ~30 файлов main + 48 unit-тестов, Hilt,
> UiState, токены 1:1 с iOS, outbox с той же семантикой). План — ДЕЛЬТА
> до паритета с iOS, по трём gap-отчётам (AI-флоу, экраны, техаудит).
> Ревизия 4 вшивает второе адверсариальное ревью Codex (по коду android/):
> точные DTO-контракты Task 1, миграция старого outbox.json у тестеров,
> dual-read миграция токена, variant-specific network config (debug-overrides
> НЕ делает cleartext debug-only), код бота — 8 символов (не 6; в iOS
> та же ошибка исправлена отдельно), отдельный OkHttp-клиент для parse
> (singleton readTimeout 30с), members в OperationDetailViewModel,
> auth-загрузка файлов через repository (не сырой URL), DTO debtsUnavailable,
> порт серверных itemsplit_test.go, дробление крупных задач.
> Визуальный спек: `docs/prototype/splitty-ai-proto.html` (эталон iOS
> 2026-07-18) + Roborazzi. Детальные дельты: `20260718-android-gap-reports.md`.

## Overview

Довести существующий Android-клиент до паритета с iOS (feature/ios-app):
1. **Срочный фикс потери данных**: Android не знает про `items` — правка
   itemized-операции плоским PUT затирает чек на сервере. Чинится первым.
2. **Весь AI-флоу** (~75% кодовой массы iOS-фичи, в Android 0%): запись
   голоса hold-to-talk, фото чека, parse, чек с позициями, правки, undo.
3. **Фиксы дизайн-ревью 2026-07-17**, не портированные в Android (~22 шт).
4. **Платформенная гигиена**: targetSdk 36, токен, backup, cleartext, R8, CI.

Инварианты (те же, что iOS): AI заполняет форму, а не создаёт операцию;
отклик мика < 50 мс; ошибки и process death не теряют работу; блокировки
объясняются; AI не добавляет людей; Glossary/plurals/humanErrorText.

## Что УЖЕ есть в Android (не трогаем, опираемся)

- Ручная форма расхода ≈90% паритета (вкл. офлайн-политику, clientOpId,
  «33–34 ₽»-диапазоны, OfflineEditPolicyTest).
- Все основные экраны; токены темы 1:1; SplitMix64-аватары побитово 1:1;
  DateFmt/RelativeTime; SurfaceCard/SoftChip/PrimaryPill/MoneyText.
- Outbox: семантика = iOS (успех→remove; 4xx→failed и дальше; сеть/5xx→стоп;
  401→стоп), FIFO, mutex, атомарная запись; триггеры сеть/onStart/refresh.
- Из ревью-фиксов уже есть: ошибки архива видимы, валюты-retry, дашборд
  по dataVersion, meId==null-состояние, 409 в погашении, edge-to-edge,
  sp-шрифты (Dynamic Type «бесплатно»).

## Технические решения (из ревизии 2, остаются в силе)

- Parse: **POST `/api/v1/rooms/{roomId}/operations/parse`** multipart —
  audio (audio/wav ≤3 МБ, файл), image (jpeg ≤8 МБ), text (≤8 КБ),
  draft (json ≤64 КБ); read timeout 90с; 413/415/429/503 → humanErrorText.
- Аудио: AudioRecord PCM16 mono 16 кГц → WAV (кап 2.8 МБ); source
  VOICE_RECOGNITION vs MIC — решить тестом; `getMinBufferSize` +
  STATE_INITIALIZED + fallback-rate с ресемплом; `setPrivacySensitive`
  (API 30+); AudioRecordingCallback; стоп по lifecycle. НИКОГДА не два
  захвата микрофона.
- Транскрипт-караоке — лестница за BuildConfig-флагом: v1 без караоке →
  PoC SpeechRecognizer + `EXTRA_AUDIO_SOURCE` (API 33+, кормим своим PCM
  через ParcelFileDescriptor) → Vosk ru-small on-demand (fallback).
- Жест мика: `pointerInput` + `awaitEachGesture`/`awaitFirstDown(
  requireUnconsumed=false, pass=Initial)`, сырой цикл для move за границами,
  consume, CANCEL-ветка; замер `SystemClock.uptimeMillis()-down.uptimeMillis`;
  панель вне скролла; отступ от зоны системных жестов.
- Process death: черновик (json) в SavedStateHandle; WAV/фото — файлы в
  cacheDir, в state — пути; восстановление в init VM; тест recreate.
- Оверлей записи — постоянно в композиции (alpha 0, анимации гейтятся).
- Токен: Keystore AES-GCM + шифротекст в DataStore (EncryptedSharedPreferences
  deprecated); `dataExtractionRules` исключают session/cache/outbox.

## Контекст для исполнителя (план выполняется в НОВОЙ сессии)

- Репо: `/Users/almaznurmuhametov/projects/Архив/go/splitty-app`. Оба клиента
  здесь: `ios/` (эталон паритета) и `android/` (доводим). Сервер — Go в
  `internal/`, НЕ менять (контракты смотреть можно: `docs/API.md`,
  `internal/rest/`, `internal/api/`).
- Детальные gap-отчёты (дельта по файлам/строкам): **`docs/plans/
  20260718-android-gap-reports.md`** — читать перед стартом.
- Сборка/тесты Android: из `android/` — `./gradlew testDebugUnitTest`
  (48 тестов должны быть зелёными до и после каждой задачи),
  `./gradlew assembleDebug`. Gradle-конфиги: `android/app/build.gradle.kts`.
- Визуальный эталон: `docs/prototype/splitty-ai-proto.html` (13 состояний,
  токены = iOS). iOS-снапшоты НЕ хранятся в репо — регенерируются командой
  из `ios/`: `xcodebuild -project Splitty.xcodeproj -scheme Splitty
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro'
  -only-testing:SplittyTests test` → PNG в `/tmp/splitty-snapshots/`.
- Коммиты: автор AlmazNurmukhametov, БЕЗ упоминаний Claude/Anthropic,
  по задаче на коммит, русские сообщения в стиле `git log android/`.
- Конвенции кода: русские KDoc-комментарии («почему», не «что»), паттерны
  проекта — см. `android/README.md` и существующие экраны; сырые hex-цвета
  запрещены (только `Splitty.colors.*`).
- Дев-стенд (если нужен ручной прогон): Go-сервер собирается
  `GOTOOLCHAIN=local ~/sdk/go1.23.5/bin/go build ./...`; env-переменные
  дев-запуска — в истории `/tmp/splitty-server` (GEMINI_API_KEY, API_DEV_AUTH,
  LISTEN 0.0.0.0:7171); dev-auth: `POST /api/v1/auth/dev {"userId":101}`.
- Незакоммиченное состояние: iOS-изменения последней недели могут быть ещё
  не в git (проверить `git status`) — Android-задачи их НЕ трогают.

## Development Approach

- Regular (код → тесты в той же задаче); русские комментарии («почему»);
  коммит на задачу, автор AlmazNurmukhametov.
- **CRITICAL: тесты в каждой задаче; `./gradlew testDebugUnitTest` зелёный
  перед следующей** (+ `verifyRoborazziDebug` начиная с Task 3, где
  добавляется Roborazzi-стек: Robolectric + Compose UI test deps,
  версии пиннить в каталоге).
- Чистая логика — на JVM-тесты (паттерн проекта: сторы не Android-зависимы).

## Implementation Steps

### Task 1: HOTFIX — items в Operation (потеря данных)

**Files:**
- Modify: `core/model/Models.kt`, `data/OutboxStore.kt`, `data/SplittyRepository.kt`, `ui/expense/AddExpenseViewModel.kt` (минимально), тесты

- [x] `Operation.items: List<OperationItem>? = null` (+ `OperationItem`/`ItemShare` DTO — только декод/энкод, без логики долей); `OperationBody.items` тоже (Requests.kt) — иначе passthrough физически невозможен
- [x] Сигнатуры `SplittyRepository.addOperation/updateOperation` — optional items; `updateOperation` проносит items оригинала НЕТРОНУТЫМИ
- [x] OutboxPayload.items nullable c default null — **старые `outbox.json` на устройствах тестеров обязаны читаться** (тест на старый JSON: текущая стратегия corrupted→empty молча стёрла бы очередь). Скоуп passthrough — только offline create/local edit (UPDATE/DELETE в outbox не бывает, OutboxSyncer их отвергает)
- [x] Правка itemized-операции в UI: сохранение ЗАПРЕЩЕНО с подписью «операция по позициям чека — правится там, где создана» (временно, до Task 10 — itemized-режим формы); просмотр — как раньше
- [x] unit: декод операции с items (фикстура из iOS-теста), PUT-passthrough, outbox: старый JSON без items + round-trip с items
- [x] run tests — must pass before next task

### Task 2a: Бамп стека (SDK 36)

**Files:**
- Modify: gradle-файлы, `AndroidManifest.xml`

- [x] AGP 8.9.1+ (минимум для API 36; wrapper 8.14 совместим), Kotlin/KSP 2.2.x, compileSdk/targetSdk **36**; Compose BOM — НЕ слепо свежий, а совместимый с compileSdk 36 (новейшие могут требовать 37/AGP 9 — проверить release notes)
- [x] `android:enableOnBackInvokedCallback="true"` (predictive back), убрать ручной `configChanges`
- [x] Полный прогон существующих 48 тестов после бампа
- [x] run tests — must pass before next task

### Task 2b: Токен в Keystore (миграция без разлогина)

**Files:**
- Create: `core/session/TokenCipher.kt`; Modify: `core/session/SessionStore.kt`

- [x] TokenCipher: Keystore AES-GCM; **dual-read миграция**: читаем старый plain `KEY_TOKEN` из DataStore → шифруем → пишем новый ключ → удаляем старый (тестеры НЕ разлогиниваются)
- [x] Очистка на logout (ключ + шифротекст + Coil/файловые кеши)
- [x] Тесты: Robolectric (или fake-cipher интерфейс) — JVM-unit с реальным Keystore невозможен; сценарии: чистая установка, миграция plain→encrypted, logout
- [x] run tests — must pass before next task

### Task 2c: Backup, cleartext, дефолтный сервер

**Files:**
- Modify: `AndroidManifest.xml`, res/xml (network config ×2), `core/session/SessionStore.kt`

- [x] `dataExtractionRules` + `fullBackupContent`: исключить ТОЧНЫЕ пути — `files/datastore/session.preferences_pb`, `files/outbox.json`, `files/cache-api/` (шифротекст без Keystore-ключа не восстановим — бэкапить бессмысленно и вредно)
- [x] cleartext: РАЗНЫЕ network_security_config на variant (src/debug/res/xml и src/release/res/xml) — `debug-overrides` это про debug-CA, cleartext он НЕ гейтит
- [x] `SessionStore.DEFAULT_BASE_URL`: в release — https-плейсхолдер прод-домена, HTTP-IP только в debug (BuildConfig); боевой IP из placeholder логина убрать
- [x] Тест: release-конфиг не допускает cleartext (unit на парс конфига или ручная проверка в чек-лист Task 15)
- [x] run tests — must pass before next task

### Task 3: Порт Components + хептики + унификация

**Files:**
- Create: `ui/components/{Glossary,FailedState,Haptics,AppToast,NudgeHighlight}.kt`
- Modify: экраны с копиями ErrorView, обе кнопки «Погасить», MoneyText

- [x] Порт `Core/Components.swift`: Glossary (settled/settledHero/balanceCaption с нулевой веткой и «взаимные долги»), FailedState единый (заменить 5 копий двух стилей), humanErrorText-доводка (таймаут отдельно)
- [x] Haptics (tap/success/warning через HapticFeedback/Vibrator) + применить по контракту: выбор чипов/фильтров, успех сохранения/платежа, warning на нуджах
- [x] AppToast (галочка, автогашение 2.8с, гасится выполнением подсказки), NudgeHighlight (встряска+рамка; гейт по animator duration scale = 0 — как reduce motion)
- [x] Reduce-motion гейт на spring/scale в DS; numericText-аналог MoneyText (AnimatedContent)
- [x] «Погасить» → единый SoftChip(isSelected) в hero и балансах
- [x] unit: balanceCaption (+/0/−/мульти), Glossary; Roborazzi-инфраструктура + снапшоты FailedState/Toast
- [x] run tests — must pass before next task

### Task 4: Фиксы дизайн-ревью — логин, профиль, уведомления

**Files:**
- Modify: `ui/auth/LoginScreen.kt`, `ui/profile/{ProfileScreen,NotificationSettingsScreen}.kt` (+VM), `res/values/strings.xml`

- [x] Логин: кнопка «Открыть бота» (`tg://resolve?domain=split_money_bot&start=login`, fallback ACTION_VIEW https при ActivityNotFoundException); подсказка «Код из бота — 8 символов» (бот генерирует 8, docs/API.md; заодно исправить валидацию LoginViewModel ≥6 → ≥8 и её тест; в iOS та же ошибка уже исправлена); dev-вход и поле «Сервер» — только `BuildConfig.DEBUG`
- [x] Профиль: убрать дубль «Уведомления» (Switch), master-toggle → верх экрана уведомлений (категории disabled при выключенном); секция «Сервер» за debug; caption у языка; placeholder шапки при незагруженном me
- [x] Уведомления: «в вашей тусе»→«в вашей группе»; ошибка PATCH — алерт (не молчаливый откат); isSaving против гонки; a11y-hint на «скоро»
- [x] unit VM: master-toggle каскад, откат с алертом; run tests — must pass

### Task 5: Фиксы дизайн-ревью — группы, друзья, активность, погашение

**Files:**
- Modify: `ui/groups/{GroupsListScreen,GroupDetailScreen,GroupDashboardScreen}.kt`, `ui/friends/{FriendsListScreen,FriendDetailScreen}.kt`, `ui/activity/ActivityScreen.kt`, `ui/settleup/SettleUpScreen.kt`(+VM), `ui/main/MainScaffold.kt`, strings

- [x] Группы-список: empty state с кнопками «Создать»/«Присоединиться», прямая кнопка join вместо меню-из-одного, скрыть «Архив» в пустом состоянии, бейдж «не отправлено» на карточках, «Разархивировать» вне clickable-зоны карточки
- [x] Группа: empty state операций — честный текст + кнопка «Добавить расход»; «без учёта N неотправленных операций» (полное слово); сегмент «Все/Со мной» — isSelected-семантика + haptic; título чужого долга «X должен(на) — Y»; текст офлайн-гейта = iOS; a11y-объединение строки долга; удалить мёртвый GroupBalancesSheet
- [x] Дашборд: иконки плиток → accent; «Я заплатил»→«Заплачено мной»; minimumScaleFactor на суммах плиток
- [x] Настройки группы: подтверждение смены валюты («Суммы не пересчитываются — …, у всех участников»); «Архивировать» → нейтральный ink
- [x] Друзья: подписи через Glossary (нулевая ветка + «взаимные долги»); empty → «Создать группу»; деталь друга: **CTA «Погасить»** (одна группа → сразу, несколько → выбор; офлайн-гейт); «Все долги погашены» унифицировать
- [x] Активность: empty фильтра → «Показать все»; stateDescription фильтра; chevron + merge semantics карточки; «Расчёт»→Glossary.settled
- [x] SettleUp: `preselectedDebt` из строки балансов/друга; офлайн — disabled CTA + подпись-причина (вместо алерта по тапу); Haptics.success
- [x] Таб «Активность»: BarChart → clock (согласовать с empty state)
- [x] DTO-гэп: `debtsUnavailable` из REST-контракта добавить в Android-модели и учитывать в балансах (сервер его шлёт, клиент игнорирует)
- [x] unit VM: preselect, glossary-ветки; Roborazzi: empty states
- [x] run tests — must pass before next task

### Task 6: Модели позиций + derivedShares (ядро itemized)

**Files:**
- Create: `core/model/OperationItems.kt` (логика поверх DTO из Task 1), тесты

- [x] Порт `Models.swift` 80–350: kind/split/percent/unknown, `derivedShares()` — ТОЧНОЕ зеркало серверного расчёта (веса, фикс-суммы, surcharge proportional/equally, канонизация)
- [x] Порт iOS SharesTests + ItemDraftTests полностью (JVM)
- [x] Порт кейсов серверного `internal/api/itemsplit_test.go` (включая overflow/error-ветки) — derivedShares должен сходиться с сервером и на краях
- [x] run tests — must pass before next task

### Task 7: Parse-эндпоинт + фото чека (AI без голоса — уже фича)

**Files:**
- Create: `ui/expense/ReceiptCaptureController.kt`
- Modify: `core/network/SplittyApi.kt`, `data/SplittyRepository.kt`, `ui/expense/AddExpenseViewModel.kt`, `AndroidManifest.xml`, тесты

- [x] `@Multipart POST api/v1/rooms/{roomId}/operations/parse`: части draft/text/audio/image (имена и MIME как iOS APIClient 368–448); маппинг 413/415/429/503
- [x] Таймаут 90с: ОТДЕЛЬНЫЙ OkHttp-клиент/Retrofit для parse (singleton имеет readTimeout 30с в NetworkModule.kt:35) или per-call `withTimeout`-обёртка над call.timeout()
- [x] `POST users/{userId}/aliases` (best-effort)
- [x] Фото: Photo Picker + CameraX/TakePicture (CAMERA + rationale), EXIF, даунскейл 1024px JPEG q0.7, файл в cacheDir (process death), путь в SavedStateHandle
- [x] VM: isParsing/parseGeneration/cancelParse (обгон запросов), apply(parse) → заполнение формы, parseRetryMessage («Повторить» — данные сохранены), parsing-оверлей (спиннер + «Отмена» через 2.5с)
- [x] Черновик формы в SavedStateHandle (json) — восстановление после process death
- [x] unit (MockWebServer): multipart-части, коды ошибок, supersede, retry; recreate-тест VM
- [x] run tests — must pass before next task

### Task 8: Чек — ReceiptCard/PersonBreakdown (read-only + интерактив)

**Files:**
- Create: `ui/expense/{ReceiptCard,PersonBreakdownCard}.kt`
- Modify: `ui/theme/Theme.kt` (receiptPaper), тесты + Roborazzi

- [x] ReceiptCard: перфорация (Canvas), пунктир, «ПОЗИЦИИ · N», моно-суммы, «цена?»/unknown-чипы (пульс с гейтом), бейджи ×N/замок, правило сбора, «Итого/Итого ≥», подсветка изменённых строк; режимы read-only и интерактивный (колбэки)
- [x] PersonBreakdownCard («С кого сколько», «+N ₽ сбор», галка по факту сходимости)
- [x] Roborazzi: interactive/priceless/unknown/read-only — сверка с iOS-снапшотами и docs/prototype
- [x] run tests — must pass before next task

### Task 9: Деталь операции — позиции и вложения

**Files:**
- Modify: `ui/groups/{OperationDetailScreen,OperationDetailViewModel}.kt`; Create: `ui/components/ZoomableImage.kt`

- [x] OperationDetailViewModel: добавить `room.members` в state (сейчас отдаёт только operation+currency — ReceiptCard без members не отрисовать)
- [x] Секция «Позиции чека» (read-only ReceiptCard); секции «Кто платил»/«Кто участвует»
- [x] Вложения: список с типами («Фото/Видео/Документ»), просмотр фото через `repository.fileData(fileId)` (auth-заголовок; НЕ сырой URL в Coil), ZoomableImage (transformable 1–4x, double-tap, downsample больших)
- [x] Текст удаления погашений («не редактируются — удалите и запишите заново»)
- [x] unit VM (members/вложения); run tests — must pass

### Task 10: Форма расхода — itemized-режим, ItemSheet, нуджи

**Files:**
- Create: `ui/expense/{ItemSheet,UnknownPickerSheet}.kt`
- Modify: `ui/expense/{AddExpenseScreen,AddExpenseViewModel}.kt`, тесты

- [x] VM: draftItems-режим, saveBlockedReason (живой «Сохранить» с причиной-тостом), undo-снапшоты (undoParse/collapseToEqualSplit + баннер «Правка применена/Отменить»), resetItems при ручной правке суммы, itemized-сохранение (items в POST/outbox) — снимает запрет Task 1
- [x] ItemSheet (Долями/Суммами, живая математика, удалить с confirm); «+ Добавить позицию»; unknown-резолв (пикер участников + alias + тост «Запомнил»)
- [x] Нуджи: тап по заблокированным кнопкам → встряска поля группы + тост (гаснет при выборе); подтверждение удаления локальной записи; автоскролл чипов группы
- [x] Roborazzi: item-sheet weights/amounts — эталон: `docs/prototype/splitty-ai-proto.html` (состояния «Чек собран», «Позиция без цены», «Саня — кто это?», «Правка голосом», «Нудж без группы»); unit: порт AddExpenseAIFlowTests + DistributionTests (расчётные)
- [x] run tests — must pass before next task

### Task 11: AudioRecorder

**Files:**
- Create: `ui/expense/AudioRecorderController.kt`, тесты

- [x] AudioRecord по решениям выше (16k/PCM16/WAV, кап 2.8 МБ, RMS→level по формуле iOS, source-выбор тестом, fallback-rate+ресемпл, privacySensitive, lifecycle-cancel)
- [x] RECORD_AUDIO: rationale, «навсегда отказано» → алерт с переходом в настройки
- [x] unit: WAV-заголовок, кап, нормализация уровня, ресемпл на синтетике
- [x] run tests — must pass before next task

### Task 12: Hold-to-talk + RecordingOverlay

**Files:**
- Create: `ui/expense/{MicTouchGesture,RecordingOverlay}.kt`
- Modify: `ui/expense/AddExpenseScreen.kt` (композер: «Надиктуйте расход», микрофон в thumb-zone, нижняя панель правки — мик/камера/сохранить), тесты + Roborazzi

- [x] Жест по решениям выше (латентность в лог; TalkBack — tap-toggle; гонка короткого тапа; тап <0.7с → обучающий тост)
- [x] Оверлей: постоянный в композиции, fade+pop из фрейма кнопки (onGloballyPositioned), squash кнопки, замок (прогресс/защёлка/Готово/Отмена), отмена влево (красный кроссфейд, ✕-зона гасится), волна от level, таймер+кольцо 60с+«осталось N с»+автостоп, статусы, два хептика; predictive back в locked = «Отмена» с confirm
- [x] Экран «Записано»: «Распознать» (primary) → «Добавить фото чека» → «Отменить»; lastAudio в cacheDir; фото уходит ВМЕСТЕ с голосом
- [x] Голосовая правка черновика (draft с голосом), подсказки «Осталось уточнить» в оверлее, parseQuestions под чеком, баннер «Распознано голосом» для плоского результата
- [x] Roborazzi: оверлей обычный/hints/cancel/locked (fake-recorder с фиксированными level/startedAt) — эталон: `docs/prototype/splitty-ai-proto.html` (состояния «Запись», «Осталось уточнить», «Замок», «Свайп влево», «Записано», «Parsing»)
- [x] unit: пороги свайпов, автостоп, CANCEL; run tests — must pass

### Task 13: Караоке-транскрипт (лестница, за флагом)

**Files:**
- Create: `ui/expense/transcribe/{Transcriber,PlatformTranscriber,VoskTranscriber}.kt`, тесты

- [x] Интерфейс + karaoke-окно (хвост снизу, градиент-маска)
- [x] PlatformTranscriber (API 33+ EXTRA_AUDIO_SOURCE с нашим PCM) — PoC на устройстве
  (код готов; прогон на живом железе — в матрице Task 15, флаг пока выключен)
- [x] VoskTranscriber fallback: модель on-demand (не в APK), RAM-профилирование
  (движок за рефлексивным мостом — библиотека и модель приезжают с докачкой,
  распознаватель живёт только на время записи)
- [x] Флаг выключен → оверлей без караоке; unit: аккумуляция сегментов
- [x] run tests — must pass before next task

### Task 14: Релизная готовность

**Files:**
- Modify: `app/build.gradle.kts`, `proguard-rules.pro`; Create: `.github/workflows/android.yml`

- [x] R8: isMinifyEnabled + shrinkResources, keep-правила Retrofit/OkHttp (+Vosk если включён), **smoke minified-сборки**
  (smoke автоматизирован задачей `verifyReleaseShrinking`: читает mapping.txt
  и падает, если R8 выкинул сериализаторы или точки входа)
- [x] CI: assembleDebug + testDebugUnitTest + lint (+ verifyRoborazziDebug)
  (`.github/workflows/android.yml`, плюс assembleRelease как R8-smoke)
- [x] Тесты ядра: OutboxSyncer (4xx/5xx/401-ветвление), SplittyRepository (fallback на кеш)
  (в OutboxSyncer заведён шов: признак сети приходит StateFlow'ом, а не
  Android-зависимым NetworkMonitor — ветвление гоняется без Robolectric)
- [x] Firebase App Distribution: release-сборка, группа тестеров
  (плагин + `firebase.properties` вне git; команда — в android/README)
- [x] Актуализировать android/README («не сделано» уже сделано; новые команды)
- [x] run tests — must pass before next task

### Task 15: Verify acceptance criteria

- [ ] Таблица паритета по трём gap-отчётам: каждый пункт «Отсутствует» закрыт или осознанно отложен (с пометкой)
- [ ] Инварианты: латентность мика <50 мс (лог на устройстве), process death не теряет диктовку/черновик (recreate + am kill), itemized-операции iOS↔Android взаимно не портятся
- [ ] Дизайн-ревью паритета (как 2026-07-17): три независимых прохода по группам экранов + сверка Roborazzi с iOS-снапшотами и docs/prototype бок-о-бок; «ощущение» на устройстве (шрифт/хептики/кривые) тюнингуется глазами
- [ ] Матрица: API 26, 33 (permissions/transcriber), 35/36 (edge/back), слабый OEM; обе темы; TalkBack smoke; 200% шрифт
- [ ] `testDebugUnitTest + verifyRoborazziDebug + lint` + R8-smoke зелёные

### Task 16: Документация

- [ ] android/README финализировать; корневой README/CLAUDE.md — android-команды
- [ ] Переместить план в docs/plans/completed/

## Отложено осознанно (не в этом плане)

- WorkManager для outbox (сейчас корутины — досылка только при живом
  приложении; терпимо, триггер onStart есть), type-safe навигация,
  Room вместо файловых сторов, Coil, дисковый кеш аватаров.
- Play Console-публикация (Firebase App Distribution покрывает тестеров).

## Post-Completion

- Деплой Go-сервера с `/start login` (иначе «Открыть бота» шлёт голый /start).
- Прод-сервер на HTTPS с доменом (cleartext уходит из релиза в Task 2 —
  боевой сервер должен быть https к релизу).
- Ручное тестирование на слабом OEM-устройстве.
- Отозвать засвеченный GEMINI_API_KEY.
