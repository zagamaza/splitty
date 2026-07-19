# Приёмка Android-клиента (Task 15) — 2026-07-19

Отчёт по `docs/plans/20260718-android-app.md`, Task 15. Проверка велась по трём
gap-отчётам (`20260718-android-gap-reports.md`), трём независимым проходам
дизайн-ревью и автоматической валидации.

## 1. Таблица паритета по gap-отчётам

### Техсостояние (отчёт §1) — закрыто полностью

| Пункт отчёта | Статус | Где |
| --- | --- | --- |
| targetSdk 35 → 36, AGP/Kotlin/KSP | закрыт | `app/build.gradle.kts:17,22` |
| JWT plain в DataStore | закрыт | `core/session/TokenCipher.kt` (Keystore AES-GCM, dual-read миграция) |
| `allowBackup` без exclusion | закрыт | `res/xml/{data_extraction_rules,backup_rules}.xml` |
| cleartext глобально | закрыт | `src/release/res/xml/network_security_config.xml` (`false`), debug — отдельный |
| боевой IP в дефолтах | закрыт | `SessionStore.kt:60` — https-домен в release, IP только в debug |
| parse-эндпоинт и multipart отсутствуют | закрыт | `core/network/ParseApi.kt` (свой клиент, 90 с) |
| R8 выключен | закрыт | `isMinifyEnabled` + `verifyReleaseShrinking` |
| CI для Android нет | закрыт | `.github/workflows/android.yml` |
| predictive back нет | закрыт | `AndroidManifest.xml:17`, ручной `configChanges` убран |
| нет тестов OutboxSyncer/Repository | закрыт | `OutboxSyncerTest.kt`, `SplittyRepositoryCacheTest.kt` |
| валидация кода от 6 (бот шлёт 8) | закрыт | `LoginCode.MIN_LENGTH = 8` |

### AI-флоу (отчёт §2) — закрыто полностью

Все перечисленные в отчёте отсутствующие сущности реализованы: модели
`OperationItem`/`ItemShare`/`ParseDraft`/`ParseResponse`, `Operation.items` и
`OperationBody.items` (снимает баг «плоский PUT затирает чек»), multipart parse
+ alias POST, `AudioRecorderController` (WAV 16k/PCM16), hold-to-talk
(`MicTouchGesture` + `RecordingOverlay`), экран «Записано», parsing-оверлей,
retry с lastAudio, обгон поколениями, фото чека с даунскейлом 1024px/q0.7,
`ReceiptCard`, `PersonBreakdownCard`, `ItemSheet`, unknown-резолв,
undo-снапшоты, `NudgeHighlight`, `saveBlockedReason`, `missingInfoHints`,
`parseQuestions`, токен `receiptPaper`, `RECORD_AUDIO`/`CAMERA` в манифесте.

Сверх отчёта: караоке-транскрипт за флагом `KARAOKE_TRANSCRIPT` (выключен).

### Фиксы дизайн-ревью 2026-07-17 (отчёт §3) — 27 из 27 закрыты

Проверены поимённо все 27 пунктов списка «отсутствующие в Android»; каждый
подтверждён кодом. Отдельно отмечу два места, которые при беглом чтении
выглядят как недоделки, но являются осознанным паритетом с iOS:

- Офлайн-гейт «Погасить» на **входах** (`FriendDetailScreen.kt:84`,
  `GroupDetailViewModel.kt:160`) — алерт по тапу. Это ровно поведение iOS
  (`FriendDetailView.swift:123-126`); disabled-CTA с подписью относится к
  экрану SettleUp, и там он есть.
- Ручная секционная ошибка в пикере валют (`GroupDetailScreen.kt:1221`)
  зеркалит такую же ручную в `GroupSettingsView.swift:158`.

## 2. Инварианты

| Инвариант | Статус | Чем подтверждён |
| --- | --- | --- |
| itemized-операции iOS↔Android взаимно не портятся | **проверен** | `ItemizedCrossClientTest.kt` — полная петля сервер→декод→PUT-тело→энкод на реальной фикстуре + сходимость `derivedShares` с плоскими `recipients` |
| Расчёт долей = серверный `DeriveShares` | **проверен** | `OperationItemsTest.kt` (29 тестов, порт `itemsplit_test.go` + iOS, включая края) |
| process death не теряет черновик/диктовку | **структурно проверен** | черновик, `KEY_AUDIO_PATH`, `KEY_RECEIPT_PATH` в `SavedStateHandle` (`AddExpenseViewModel.kt:1177-1183`), round-trip в `ExpenseDraftSnapshotTest`. Прогон `am kill` на устройстве — не выполнен |
| латентность мика < 50 мс | **не проверен** | требует замера на живом железе (лог `SystemClock.uptimeMillis`) |

### Найдено и исправлено в ходе приёмки

1. **Пороги жеста мика сравнивались в пикселях с dp-порогами** (`MicTouchGesture.kt`).
   iOS-овские `-70` — это points; на 3x-экране замок и отмена срабатывали через
   ~23 dp хода вместо 70, а прогресс-кольцо насыщалось почти сразу. Смещение
   приводится к dp (`micDragPxToDp`), регрессия закрыта тестами. Это
   единственная находка приёмки с функциональными последствиями — и она
   напрямую бьёт по инварианту отклика мика.
2. **Семь снапшотов оверлея были невоспроизводимы**: фаза волны считалась от
   живого `SystemClock`, поэтому `verifyRoborazziDebug` падал сразу после
   `recordRoborazziDebug` в зависимости от порядка тестов. Добавлен пин времени
   `frozenNowMs` (в проде `null`); воспроизводимость подтверждена тремя
   подряд прогонами verify.
3. Четыре диаграммы дашборда были не видны TalkBack — добавлены подписи
   (тексты = iOS), строка баланса участника склеена в один элемент.
4. Отсутствовавшие хептики: фильтр «Только мои», успех удаления операции,
   центральная кнопка «+» (соседние табы уже вибрировали).
5. Parsing-оверлей: тёмный скрим вместо светлого фона темы, заголовок
   «Распознаю…» вместо «Распознаю чек…» (оверлей висит и на голосовом пути,
   где чека нет), добавлен пропавший подзаголовок.
6. Копия: «Вы платите — X» (em-dash, как iOS), подсказка баннера
   «Не то? Зажмите микрофон внизу или добавьте фото чека» — про голосовую
   правку, а не только фото.
7. `android/README.md` утверждал, что эталоны Roborazzi лежат в `build/` и не
   версионируются. Фактически это 23 закоммиченных PNG в
   `app/src/test/snapshots/`, и сверка попиксельная — описание исправлено.

## 3. Дизайн-ревью паритета (три независимых прохода)

Проходы: A — экраны расхода/AI, B — группы/друзья/активность/погашение,
C — авторизация/профиль/оболочка/дизайн-система.

Итог: **токены 1:1 с iOS** (все цвета, радиусы, типошкала, палитра аватаров и
вывод индекса SplitMix64), сырых hex вне `Theme.kt` нет, все 13 состояний
прототипа присутствуют, reduce-motion гейтится везде, где в iOS есть
`accessibilityReduceMotion`. Блокеров после исправлений выше не осталось.

**Осознанно отложено** (косметика «ощущения», тюнингуется глазами на
устройстве — как и предусмотрено планом):

- rounded-начертание шрифта (iOS `design: .rounded`) не портировано;
- `MoneyText` анимирует цифры только в opt-in `AnimatedMoneyText`, в iOS — везде;
- `SoftChip` усекает текст многоточием, iOS — не усекает никогда;
- скримы оверлеев — плоский чёрный вместо `.ultraThinMaterial` + и заметно
  темнее iOS (0.55/0.72 против 0.35);
- нет тени у тоста, иконок `paperplane` у двух строк, разделителя в
  уведомлениях; подсветка изменённых строк гаснет резко, без фейда;
- мелкие расхождения паддингов (строки уведомлений, языка), подъём FAB
  −12 dp против −18, chevron у неведущих строк профиля;
- анимация баннера связи.

Сверка Roborazzi с iOS-снапшотами **бок-о-бок не выполнена**: iOS-эталоны в
репозитории не хранятся и требуют прогона `xcodebuild` на симуляторе.

## 4. Матрица устройств

| Строка матрицы | Статус |
| --- | --- |
| API 26 (minSdk) | статически чисто: lint не даёт ни одного `NewApi`; версионные ветки только в `AudioRecorderController`, `Haptics`, `Transcriber` |
| API 33 (permissions/transcriber) | код готов, флаг караоке выключен; прогон не выполнен |
| API 35/36 (edge-to-edge, predictive back) | конфигурация на месте; прогон не выполнен |
| Слабый OEM | не выполнен |
| Обе темы | покрыто снапшотами (light/dark у FailedState, тоста, чека, разбивки) |
| TalkBack smoke | не выполнен; семантика приведена в порядок статически (см. §2) |
| 200% шрифт | не выполнен; шрифты в `sp`, Dynamic Type «бесплатно» |

Прогон на эмуляторах и живом железе в этой сессии невозможен — устройство не
подключено. Матрица остаётся открытой; это единственный незакрытый пункт
Task 15.

## 5. Автоматическая валидация

```
./gradlew testDebugUnitTest verifyRoborazziDebug lintDebug --rerun-tasks   BUILD SUCCESSFUL
./gradlew assembleRelease verifyReleaseShrinking                            BUILD SUCCESSFUL
```

- **306 юнит-тестов**, 0 падений, 0 ошибок (40 классов).
- **23 снапшота Roborazzi** сверены попиксельно; воспроизводимость проверена
  тремя подряд прогонами.
- **lint**: блокеров нет. `InsecureBaseConfiguration` — это debug-вариант
  network config, то есть ровно задуманное поведение.
- **R8-smoke**: 40 сериализаторов, интерфейсы API и точки входа на месте.

Известный некритичный долг: `ReceiptCaptureController` использует
`android.media.ExifInterface` вместо `androidx.exifinterface` (lint-warning);
не трогал — добавление зависимости выходит за рамки приёмочной задачи.
