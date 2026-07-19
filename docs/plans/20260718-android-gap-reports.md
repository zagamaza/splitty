# Gap-отчёты: Android vs iOS (2026-07-18)

Справочный материал к плану `20260718-android-app.md`. Три независимых
анализа: AI-флоу, остальные экраны, техсостояние. Сокращено до actionable.

## 1. Техсостояние android/ (аудит)

- Стек: AGP 8.7.3, Kotlin/KSP 2.0.21, Compose BOM 2024.12.01, compileSdk/
  targetSdk **35** (Play требует 36 к 31.08.2026), minSdk 26, wrapper 8.14.
- Hilt 2.52 полноценный; Navigation Compose 2.8.5 СТРОКОВЫЕ роуты.
- JWT — **plain text в DataStore** (`core/session/SessionStore.kt`, KEY_TOKEN,
  файл session), `allowBackup=true` БЕЗ exclusion rules.
- Сеть: Retrofit 2.11 + OkHttp 4.12 + kotlinx.serialization; base URL —
  плейсхолдер `http://placeholder.invalid/` + `BaseUrlInterceptor`;
  singleton OkHttp c **readTimeout 30с** (`di/NetworkModule.kt:35`);
  401 → глобальный разлогин в AuthInterceptor. **Parse-эндпоинта и
  multipart НЕТ вообще. Coil нет** — картинки через Retrofit (auth ок),
  аватары in-memory без диска.
- Cleartext разрешён ГЛОБАЛЬНО (`network_security_config.xml`), дефолтный
  сервер — `http://138.124.18.189:18002` (SessionStore.DEFAULT_BASE_URL,
  светится и в placeholder логина).
- Outbox: самодельные корутины (НЕ WorkManager), семантика = iOS
  (успех→remove; 4xx≠401→failed и дальше; сеть/5xx→pending+стоп; 401→стоп),
  FIFO, Mutex, clientOpId=localId, атомарная запись `files/outbox.json`;
  триггеры: сеть/onStart/refresh. В outbox только CREATE (UPDATE/DELETE
  отвергается OutboxSyncer:82). Room НЕТ: ApiCache — JSON-файлы
  `files/cache-api/`, сессия — DataStore.
- Тесты: 48 @Test в 6 файлах (Models/Money/Charts/OfflinePolicy/Avatar/
  LoginCode + ApiCache/OutboxStore). Нет тестов OutboxSyncer и Repository.
- R8 ВЫКЛЮЧЕН; proguard-rules — заготовка; CI для Android НЕТ (только Go);
  подпись release через keystore.properties (в git не закоммичены).
- `enableEdgeToEdge()` есть; predictive back НЕТ; ручной `configChanges`.
- LoginViewModel.kt:21 валидирует код от 6 символов — а бот генерирует 8.

## 2. AI-флоу (дельта)

**Ручная плоская форма ≈90% паритета** (офлайн-политика, clientOpId,
«33–34 ₽», OfflineEditPolicyTest). Различия: донор — DropdownMenu vs iOS-шиты,
нет подтверждения удаления локальной записи, нет автоскролла чипов группы,
молчаливый disabled «Сохранить» (iOS — живой тап с причиной).

**Весь AI-слой — 0%** (~75% кодовой массы iOS-фичи, ~4500 строк Swift):
- Нет моделей `OperationItem`/`ItemShare`/`ParseDraft`/`ParseResponse`,
  у `Operation` (Models.kt:136) НЕТ поля items, у `OperationBody`
  (Requests.kt:61) тоже → **плоский PUT затирает чек на сервере — живой баг**.
- Нет: hold-to-talk (жест/оверлей/замок/отмена/волна/таймер/кольцо/статусы/
  караоке), AudioRecorder (WAV 16k/PCM16 + RMS; нет RECORD_AUDIO в манифесте),
  экрана «Записано», parsing-оверлея, retry с lastAudio, parse-обгона
  поколениями, multipart parse + alias POST, фото чека (камера/пикер/
  даунскейл 1024px JPEG q0.7), ReceiptView (перфорация/чипы/бейджи/
  «Итого ≥»), PersonBreakdown, ItemSheet, unknown-резолва, undo-снапшотов,
  подсветки изменённых строк, нуджей/тостов/NudgeHighlight, saveBlockedReason,
  голосовой правки черновика, missingInfoHints, parseQuestions.
- Эталоны iOS: AddExpenseView.swift (запись/оверлей ~903–1009, 1707–2142;
  ItemSheet 2180–2578), AddExpenseViewModel.swift (parse 419–472, undo
  358–372, hints 474–498), AudioRecorder.swift, ReceiptView.swift,
  APIClient.swift 368–459 (multipart + alias), ReceiptCapture.swift.

## 3. Остальные экраны (дельта)

**Уже 1:1**: токены темы (кроме receiptPaper), dark mode, SplitMix64-аватары
побитово, DateFmt/RelativeTime, SurfaceCard/SoftChip/PrimaryPill/MoneyText,
sp-шрифты (Dynamic Type ок), edge-to-edge, outbox-семантика, ошибки архива
видимы, валюты-retry, дашборд по dataVersion, meId==null-состояние,
409-обработка платежа (полнее iOS), предупреждение outbox при выходе.

**Фиксы ревью 2026-07-17, отсутствующие в Android** (детали и файлы —
в задачах плана): dev-вход и «Сервер» не за debug (LoginScreen, Profile);
дубль «Уведомления» + нет master-toggle; empty state операций «кнопкой
„+ Расход"» без action; нет «Открыть бота» с ?start=login; нет подсказки
длины кода (и валидация от 6 вместо 8); нет CTA «Погасить» у друга;
empty states без кнопок (Группы/Друзья/Активность-фильтр); join в меню
из одного пункта; нет подтверждения смены валюты; иконки плиток дашборда
палитрой участников; «Я заплатил»; вложения/зум/позиции чека в детали
операции ОТСУТСТВУЮТ ЦЕЛИКОМ; таб Активность BarChart; glossary-ветки
(нулевая/«взаимные долги») нет; «туса» в футере уведомлений; фильтры без
stateDescription; кнопки «Погасить»/«Повторить» по 2 вида; **хептики
отсутствуют полностью**; reduce motion не гейтится; офлайн-гейт SettleUp
старым паттерном (алерт по тапу); «без учёта N неотправленных» без слова
«операции»; бейджа outbox на карточках групп нет; ошибка PATCH уведомлений
глотается; preselectedDebt не передаётся; мёртвый GroupBalancesSheet;
placeholder шапки профиля; DTO `debtsUnavailable` игнорируется.
