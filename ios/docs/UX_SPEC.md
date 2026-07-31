# Splitty iOS — спецификация UX (клон Splitwise) и архитектурный контракт

Приложение: **Splitty**, SwiftUI, iOS 17+, bundle id `com.zagir.splitty`, проект генерируется XcodeGen (`ios/project.yml`).
Язык интерфейса: **русский**. Суммы — целые (`Int`), формат `1 200 ₽` / `1 200 $`
(пробел — разделитель тысяч, символ валюты после суммы). У каждой группы своя
валюта (`RUB`/`USD`/`EUR`/`IDR`) — см. раздел «Валюта группы».
Бэкенд: `docs/API.md` в корне репозитория. Базовый URL по умолчанию `http://127.0.0.1:7171`.

## Премиум дизайн-система (Theme.swift + DesignSystem.swift)

Стиль: «финтех-минимализм» (Apple Wallet / Revolut). Много воздуха, крупная
типографика, мягкие карточки, безупречная тёмная тема. 90% интерфейса —
нейтральные цвета; accent/negative — только смысловые.

### Семантические токены (`Core/Theme.swift`)

Все токены адаптивные (light/dark через `UIColor { trait in … }`).
**В экранах запрещены сырые hex и `Color(red:…)` — только токены.**

| Токен            | Light     | Dark      | Использование                                        |
|------------------|-----------|-----------|------------------------------------------------------|
| `.bg`            | `#F6F7F9` | `#0C0F13` | фон всех экранов (`.background(Color.bg)`)           |
| `.surface`       | `#FFFFFF` | `#171C23` | фон карточек (через `.surfaceCard()`)                |
| `.ink`           | `#101828` | `#F2F4F7` | основной текст                                       |
| `.inkSecondary`  | `#667085` | `#98A2B3` | вторичный текст, даты, «расчёт», нулевые суммы       |
| `.accent`        | `#0E9F6E` | `#34D399` | CTA, позитивные суммы («вам должны»), активный таб   |
| `.negative`      | `#DC5A2E` | `#FB923C` | долги, «вы должны»                                   |
| `.hairline`      | `#EAECF0` | `#232A33` | тонкие разделители внутри карточек, бордеры в dark   |
| `.accentPressed` | `#0B7C56` | `#2BB985` | pressed-состояние акцента, градиенты                 |
| `.chartAccent`   | `#0E9F6E` | `#0EA97A` | ТОЛЬКО заливка баров графиков (дашборд «Итоги»)      |

`.chartAccent` — цвет данных, отдельный от UI-акцента: dark-значение `#0EA97A`
валидировано для заливок на тёмной поверхности; UI-accent `#34D399` в тёмной
теме для заливки баров использовать нельзя (слишком яркий для площадных марок).

`Color.chartCategorical: [Color]` — категориальная палитра УЧАСТНИКОВ дашборда
«Итоги»: 6 адаптивных пар light/dark, валидированных на цветослепоту и контраст
к поверхностям, ПОРЯДОК ФИКСИРОВАН:

| № | Light     | Dark      |
|---|-----------|-----------|
| 1 | `#0E9F6E` | `#0EA97A` |
| 2 | `#D97706` | `#C77D08` |
| 3 | `#2F6FE4` | `#4478DB` |
| 4 | `#DB2777` | `#C94E7F` |
| 5 | `#0891B2` | `#0E8FA8` |
| 6 | `#8B5CF6` | `#8E6BE0` |

Правило назначения (`MemberPalette.colorIndices`, `GroupTotalsLogic.swift`):
участники комнаты сортируются по `user.id` ASC → индекс цвета; один человек —
один и тот же цвет во ВСЕХ графиках дашборда. Больше 6 участников: 7-й и дальше —
`inkSecondary` (в донате сворачиваются в сегмент «Прочие»). Палитра НИКОГДА
не циклится.

Легаси-алиасы (для ещё не мигрированных экранов, в новом коде не использовать):
`.swGreen` = `.accent`, `.swGreenDark` = `.accentPressed`, `.swOrange` = `.negative`,
`.swGrayText` = `.inkSecondary`, `.swDark` = `.ink`; `SWPrimaryButtonStyle`/`.swPrimary`
рисуется как `PrimaryPillButtonStyle`.

Правило цвета денег (везде!): положительный баланс/«вам должны» — `.accent`;
отрицательный/«вы должны» — `.negative`; ноль/«расчёт» — `.inkSecondary`.

### Компоненты (`Core/DesignSystem.swift`)

- **`.surfaceCard(padding: CGFloat = 16)`** — модификатор-карточка: фон `surface`,
  скругление 20pt (`.continuous`); light — тень `black 6%, radius 14, y 6`;
  dark — без тени, hairline-бордер 1pt. Все секции экранов — такие карточки
  на фоне `Color.bg` (вместо системных Form/List-фонов); внутри карточек —
  разделители `Color.hairline` высотой 1/HairlineWidth, между карточками — воздух 16–20pt.
- **`Button(...) { }.buttonStyle(.primaryPill)`** — основной CTA: pill во всю
  ширину, высота 54pt, фон `accent`, белый semibold rounded 17pt, pressed
  scale 0.98 (spring). Для «Сохранить», «Записать платёж», «Войти», «Создать».
- **`.buttonStyle(.softChip)` / `.softChip(isSelected: Bool)`** — вторичные
  кнопки-чипы: мягкая серая pill (`ink` 6%), semibold rounded 15pt;
  `isSelected` — заливка `accent` 14% + акцентный текст. Для «Балансы»,
  «Итоги», выбора группы, фильтров.
- **`MoneyText(_ amount: Int, role: .auto|.positive|.negative|.neutral, size: CGFloat = 17, weight: Font.Weight = .semibold, currency: String = "RUB")`** —
  единственный способ показать сумму: rounded + `monospacedDigit`, семантическая
  окраска, `.contentTransition(.numericText())` при изменении. Всегда рисует
  модуль суммы (знак — цветом/контекстом). `role: .auto` — цвет по знаку
  (`>0` accent, `<0` negative, `0` inkSecondary); `.positive`/`.negative` —
  принудительно; `.neutral` — `ink` (суммы без семантики долга, напр. «Всего
  потрачено»). `currency` — код валюты комнаты (символ как у рублей: «1 234 $»).
  Hero-суммы (шапки экранов): `size: 38...44, weight: .semibold`.
- **`MoneyTotalsText(totals: [CurrencySum], primarySize: 40, secondarySize: 15, alignment: .leading)`** —
  итог, где могут встретиться РАЗНЫЕ валюты (общий баланс вкладок, нетто друга):
  принимает уже агрегированные `aggregateByCurrency` суммы; основная валюта
  (наибольший |суммы|) — крупным `MoneyText`, остальные — вторичной строкой
  мельче через `·` (`inkSecondary`); пустой список — «0 ₽» серым (полный расчёт).
- **`Text("…").sectionHeaderStyle()`** — заголовок секции: 13pt semibold rounded,
  `inkSecondary`, лёгкий kerning; регистр НЕ меняет (важно для UI-тестов).
- **`Haptics.success()`** — после успешного сохранения/платежа/создания;
  **`Haptics.tap()`** — лёгкий отклик на выбор (чипы, radio, чекбоксы).

### Правила использования

- Деньги — ВСЕГДА через `MoneyText` (или минимум rounded + monospacedDigit).
- Spring-анимации по умолчанию; изменяющиеся суммы — `.contentTransition(.numericText())`
  (в `MoneyText` уже встроено).
- Списки: щедрые отступы 16–20pt, секции-«карточки», тонкие разделители только
  внутри карточек.
- Тёмная тема обязана выглядеть безупречно: не хардкодить `.white`/`.black`
  для текста и фонов — только токены.

## Валюта группы (контракт валют)

У каждой комнаты своя валюта; **все суммы комнаты показываются в её валюте**,
конвертации нет, суммы РАЗНЫХ валют никогда не складываются между собой.

Контракт:

- `RoomSummary.currency`, `RoomDetail.currency` — `"RUB" | "USD" | "EUR" | "IDR"`;
- `ActivityItem.roomCurrency` — валюта комнаты операции (для строк ленты);
- `FriendRoomBalance.currency` — валюта комнаты в разбивке друга;
- `FriendBalance.totalsByCurrency: [{"currency","sum"}]` — нетто друга ПО ВАЛЮТАМ
  (поля `total` больше НЕТ);
- `PUT /api/v1/rooms/{id}/currency` тело `{"currency":"USD"}` → 204 — смена валюты;
- `GET /api/v1/currencies` → `[{"code":"RUB","symbol":"₽","flag":"🇷🇺"}, …]` — справочник для пикера.

Форматирование (`Core/Money.swift`):

```swift
func currencySymbol(_ currency: String) -> String        // RUB ₽, USD $, EUR €, IDR Rp; иначе сам код
func money(_ sum: Int, currency: String) -> String       // «1 234 $» — формат как у rubles()
func rubles(_ sum: Int) -> String                        // обёртка money(_, "RUB")
func moneyRange(_ min: Int, _ max: Int, currency: String) -> String   // «333–334 $»
func aggregateByCurrency(_ amounts: [CurrencySum]) -> [CurrencySum]
// aggregateByCurrency: складывает повалютно, убирает нули, сортирует по
// убыванию |суммы| (первая — «основная»), при равенстве — по коду валюты.
```

Правила показа:

- Экран группы (hero, строки операций, балансы, погашение, карточка операции),
  форма расхода (символ у поля суммы, подсказки деления/распределения) —
  валюта комнаты (`RoomDetail.currency`; в форме — `AddExpenseViewModel.currency`
  выбранной группы).
- Лента активности — `roomCurrency` каждого элемента; друзья — разбивка
  «По группам» в валютах комнат.
- «Общий баланс» (Друзья/Группы) и нетто друга — `MoneyTotalsText`:
  по валютам, основная крупно, остальные вторичной строкой; подпись
  («Вам должны»/«Вы должны») — по знаку основной валюты. Если валюта одна —
  выглядит как раньше.
- Настройки группы: секция «Валюта» — строки из `GET /currencies` (флаг + код +
  символ), чекмарк у текущей; тап → `PUT currency`, затем
  `session.noteDataChanged()` (все экраны перечитают суммы в новой валюте).

## Режимы деления расхода (контракт API v2)

Операция несёт получателей с ГОТОВЫМИ долями и способ деления:

```swift
struct OperationRecipient: Codable { let user: User; let sum: Int }  // доля в целых рублях
enum SplitType: String, Codable { case equally; case byExactAmount = "by_exact_amount" }
// Operation.recipients: [OperationRecipient], Operation.splitType: SplitType?
```

- **`equally`** («Поровну») — сервер раскладывает доли канонически:
  `base = S / n` (целочисленно), `r = S % n`; получатель с индексом `i`
  в порядке массива платит `base+1` при `i < r`, иначе `base`. Σ долей == `S`.
- **`by_exact_amount`** («По суммам») — доли введены вручную при создании;
  сервер валидирует Σ == `S` (иначе `400`). В `recipients[].sum` — хранимые значения.
- `splitType` может отсутствовать (погашения); незнакомое значение клиент
  лениво читает как `equally` (не роняя декодирование ленты).

**Позиции пользователя — ТОЛЬКО из хранимых сумм** (`Core/Models.swift`), никакого
пересчёта поровну (при «По суммам» он дал бы неверные цифры):

```swift
Operation.recipientSum(of userId: Int) -> Int?   // моя доля = recipients[me].sum, nil — не участвует
Operation.netPosition(of userId: Int) -> Int?    // >0 одолжил, <0 должен, 0 расчёт, nil не участвует
```

Позиция донора: «вы одолжили» = `sum − своя хранимая доля` (если донор среди
получателей), иначе весь `sum`. Использовать в строке операции группы, ленте
активности и карточке операции (там — `recipients[].sum` напрямую).

Канонический хелпер `Core/Money.swift` остаётся ТОЛЬКО для подсказки
предпросмотра в форме добавления (`splitHint`, операция ещё не создана):

```swift
func shares(sum: Int, count: Int) -> [Int]                 // предпросмотр равных долей
func rublesRange(_ minSum: Int, _ maxSum: Int) -> String   // «333–334 ₽»
```

`splitHint` при неровном делении честно показывает диапазон:
«100 ₽ / 3 = 33–34 ₽ с человека». Вся арифметика целочисленная (`Int`),
float в денежных расчётах запрещён.

Аватары: круг с мягким пастельным градиентом и белыми инициалами rounded
(`UserAvatarView(user:size:)`), пара цветов градиента детерминирована от `user.id`
(палитра из 10 пастельных пар), инициалы — первые буквы слов `displayName`.

## Офлайн-режим (зафиксированный дизайн v1)

Слой: `NetworkMonitor` (сеть) + `OfflineStore` (read-кеш) + `DataRepo`
(«кеш сразу → сеть») + `OutboxStore` (запись/синк локальных расходов).
Все четыре — в `Core/`, экраны меняются минимально (VM берут `session.repo`
вместо `session.api` для кешируемых GET).

### Read-кеш (OfflineStore + DataRepo)

- Последний успешный ответ ключевых GET хранится JSON-файлами в
  `Application Support/SplittyCache` (файл на ключ, ключ = эндпоинт+параметры,
  атомарная запись): список комнат (`rooms-archived-*`), деталь комнаты
  (`room-{id}`), друзья, ПЕРВАЯ страница активности, статистика
  (`statistics-{id}`), справочник валют, профиль `me`.
- `DataRepo` (обёртка над `APIClient`): кеш, если есть, отдаётся МГНОВЕННО
  (колбэк `onCached` — экран рисуется без спиннера) → параллельно сеть →
  успех перезаписывает кеш; **сетевая ошибка (transport/5xx) при наличии
  кеша → показывается кеш БЕЗ алерта**, данные помечаются `isFromCache`
  (флаг в VM). Ошибки 4xx и отмена `.task` пробрасываются как раньше.
- VM применяют `onCached` только пока в памяти пусто (кеш не затирает более
  свежие данные при тихом обновлении). Из кеша активности пагинация
  отключена (следующие страницы не кешируются).
- Кеш чистится при `logout()` (вместе с outbox).

### NetworkMonitor и глобальный баннер

- `NetworkMonitor` (NWPathMonitor, живёт в `SessionStore.network`):
  `@Observable isOnline`, доступ — `session.isOnline`.
- Глобальный тонкий баннер в `MainTabView` (`safeAreaInset(edge: .top)` —
  полоса под статус-баром над навбарами всех вкладок, фон `surface`,
  текст/иконка `inkSecondary`, hairline снизу):
  - нет сети → `wifi.slash` + «Офлайн — изменения сохраняются локально»;
  - идёт синк outbox → `icloud.and.arrow.up` + «Отправка…» (кратко);
  - онлайн без синка — баннер скрыт (онлайн-путь не меняется).

### Outbox расходов (OutboxStore, файл outbox.json)

- Запись: `{localId UUID, roomId, kind: create|update|delete, payload
  (описание/сумма/донор/recipientIds|recipientSums), targetOperationId,
  createdAt, status: pending|failed(message)}` — в
  `Application Support/outbox.json`, атомарная перезапись при каждой мутации.
  В v1 приложение создаёт только `kind=create` (см. правила ниже);
  `update/delete` с `targetOperationId` — зафиксированная схема на будущее.
- Правила (жёсткие):
  - офлайн-СОЗДАНИЕ расхода → запись в outbox (форма закрывается как обычно);
  - правка/удаление операции, которая ЕЩЁ в outbox → правится/удаляется сама
    запись outbox (правка сбрасывает `failed` → `pending`); удаление — кнопка
    «Удалить» в форме правки локальной записи (с подтверждением);
  - правка СИНХРОНИЗИРОВАННОЙ операции офлайн НЕДОСТУПНА: в форме — плашка
    «Нет соединения. Можно редактировать только неотправленные операции»
    (`wifi.slash`, inkSecondary) и заблокированный «Сохранить»;
  - погашения офлайн не работают: чип «Погасить долг» показывает алерт
    «Нет соединения. Погашение долга доступно только онлайн».

### Идемпотентность и синк

- При POST из outbox передаётся `clientOpId = localId` — бэкенд на повтор
  отвечает `200` существующей операцией (см. docs/API.md «Идемпотентность
  создания»), поэтому повторная отправка после потерянного ответа безопасна.
  Прямые онлайн-создания clientOpId не передают (поведение прежнее).
- Триггеры синка (`session.syncOutbox()`): `isOnline` стал true, приложение
  стало активным (scenePhase), pull-to-refresh на «Группах»/экране группы,
  сохранение формы расхода. Синк сериализован (`OutboxStore.sync`, guard
  `isSyncing`) — параллельных отправок нет.
- FIFO по одной: успех → запись удаляется + `noteDataChanged()`; ответ
  4xx → `status = failed(текст ошибки)`, запись остаётся (открыть →
  исправить → снова pending, или удалить); сеть/5xx → остаётся `pending`,
  синк прерывается до следующего триггера.

### Отображение локальных операций

- Экран группы: записи outbox этой комнаты рендерятся СВЕРХУ списка операций
  (карточка над секциями месяцев, новые первыми); строка = колонка даты +
  иконка `doc.plaintext` + описание + бейдж `icloud.slash` «не отправлено»
  (inkSecondary; для failed — negative и «не отправлено · <текст ошибки>»),
  справа сумма (neutral). Тап → форма правки локальной записи
  (`AddExpenseView(roomId:editEntry:)`).
- Hero-карточка группы при непустом outbox — подпись
  «без учёта N неотправленных» (inkSecondary, 13pt): серверные балансы
  локальных операций не учитывают.
- Список групп: маленький бейдж `icloud.slash` (inkSecondary) у названия
  группы, по которой есть записи outbox.
- «Профиль» → «Выйти» при непустом outbox: подтверждение
  «Есть N неотправленных операций — выйти и удалить их?» (logout чистит
  кеш и outbox).

## Структура экранов (полное соответствие Splitwise)

### Таб-бар (5 вкладок)
1. **Друзья** (`person.2.fill`)
2. **Группы** (`person.3.fill`)
3. **Добавить** — центральная кнопка добавления расхода: большой зелёный круг с `plus`, поднят над таб-баром
4. **Активность** (`chart.bar.fill` / `clock.fill`)
5. **Профиль** (`person.crop.circle`)

Реализация: `MainTabView` — свой оверлей для центральной кнопки поверх `TabView` (кнопка открывает `AddExpenseView` как sheet с выбором группы).
При видимой клавиатуре кнопка скрывается (отслеживание `keyboardWillShow/Hide`),
чтобы не висеть поверх контента над клавиатурой.

### Вкладка «Группы» (GroupsListView)
- Заголовок-шапка: суммарный баланс по всем группам: «Вам должны 1 500 ₽» / «Вы должны …» / «Все долги погашены» (цветовое правило).
- Список групп: слева квадратная иконка-плейсхолдер со скруглением (случайный из набора SF Symbols по id: `airplane`, `house.fill`, `fork.knife`, `car.fill`, `gift.fill`…, фон — пастельный от id), название, под ним «N участников».
- Справа две строки: «вам должны»/«вы должны» + сумма цветом; если 0 — «расчёт» серым.
- Pull-to-refresh. Кнопка `+` в navbar → `CreateGroupView` (поле «Название», кнопка «Создать»).
- Кнопка «Присоединиться по коду» (toolbar/меню) → `JoinGroupView`: поле для кода (roomId) → join.
- Секция «Архив» — отдельный пункт внизу списка, ведёт на список архивных групп
  (те же строки-NavigationLink в экран группы + кнопка «Разархивировать»).

### Экран группы (GroupDetailView)
- Крупный заголовок: имя группы; под ним статус: «Вам должны 500 ₽» / «Вы должны 400 ₽ Загиру» (если один кредитор) / «Нет долгов».
- Ряд кнопок-чипов: **«Погасить долг»** (зелёная, primary), **«Балансы»**, **«Итоги»**.
- Список операций, сгруппированных по месяцам («Июль 2026»), новые сверху. Строка операции:
  - слева колонка даты: «5 июл» (день + короткий месяц, серым);
  - иконка-квадратик: `doc.plaintext` для расхода, `arrow.left.arrow.right`/`banknote` зелёным для погашения; бейдж-скрепка если есть файл;
  - заголовок: описание; подзаголовок: «Загир заплатил(а) 1 200 ₽»;
  - справа: для расхода — «вы одолжили 800 ₽» зелёным / «вы должны 400 ₽» оранжевым / «не участвует» серым; для погашения — текст «Загир заплатил(а) вам» и сумма.
- Тап по операции → карточка операции (детали: кто платил, доля каждого, дата, файл-чек если есть; кнопки «Изменить»/«Удалить» — только если текущий пользователь — donor).
  Вложения тапабельны: sheet качает `GET /files/{fileId}` (фото — картинкой с ProgressView,
  видео/прочее — ShareLink по временному файлу; ошибка загрузки — алерт).
- Кнопка «＋ Расход» — плавающая зелёная внизу справа (в контексте группы открывает AddExpense сразу для этой группы).
- Toolbar: настройки группы (`gearshape`) → GroupSettingsView: список участников, секция «Валюта» (пикер из `GET /currencies`: флаг + код + символ, чекмарк у текущей; тап → `PUT /rooms/{id}/currency`, `session.noteDataChanged()`), «Пригласить» (ShareLink с кодом группы и ссылкой-приглашением: `inviteUrl` из `GET /rooms/{id}` — `https://<домен>/join/<roomId>`, а пока публичный домен на сервере не настроен — легаси-ссылка `https://t.me/split_money_bot?start=room<id>`; оба формата понимает `RoomCodeParser`), «Архивировать»/«Разархивировать».
- «Балансы» (GroupBalancesView, sheet): список вычисленных долгов: «Алмаз → Загир 500 ₽» (аватары, стрелка), у каждого долга с участием текущего пользователя — кнопка «Погасить».
- «Итоги» (GroupTotalsView, sheet, detent .large) — **дашборд группы** (см. раздел «Дашборд «Итоги»» ниже). Лейбл чипа «Итоги» зафиксирован UI-тестом.

### Дашборд «Итоги» v2 (GroupTotalsView, Swift Charts)

Открывается чипом «Итоги» (лейбл не менять — зафиксирован UI-тестом).
Данные — `GET /rooms/{id}/statistics`:

```json
{"currency": "USD", "totalSpent": 4200, "monthSpent": 1300,
 "byDay": [{"date": "2026-07-05", "sum": 800}, …],
 "byMonth": [{"month": "2026-02", "sum": 0}, …],
 "operationCount": 12,
 "paidByMember": [{"user": {…}, "sum": 3000}, …],
 "shareByMember": [{"user": {…}, "sum": 2100}, …],
 "topOperations": [{"id","description","sum","donor":{…},"createdAt"}, …]}
```

`byMonth` — ровно 6 календарных месяцев включая текущий, ascending, месяцы без
трат — нули (клиент рисует как есть); `operationCount` — количество расходов
за всё время (active, без погашений). Оба поля декодируются лениво
(`decodeIfPresent`, дефолты `[]`/`0`) — старый офлайн-кеш и ещё не обновлённый
бэкенд не роняют дашборд, соответствующие секции просто скрываются.

Все суммы дашборда — в валюте комнаты (`statistics.currency`). Чистая логика
(цвета участников, нетто-балансы, дни недели, сегменты доната, подписи
месяцев) — `Features/Groups/GroupTotalsLogic.swift` (`MemberPalette`,
`DashboardMath`), покрыта юнит-тестами. Состав (сверху вниз):

1. **Стат-плитки 2×2**: «Всего потрачено», «За <текущий месяц по-русски>»,
   «Операций» (`operationCount`, обычный `Text` + `monospacedDigit`),
   «Средний чек» (`totalSpent / operationCount` целочисленно, 0 при отсутствии
   операций). Суммы — `MoneyText` 22pt `.neutral`. У каждой плитки маленькая
   иконка 16–18pt слева от заголовка, тонированная своим цветом
   `chartCategorical` (декоративно: `banknote`, `calendar`, `list.bullet`,
   `chart.bar` — identity не несёт).
2. **«Динамика по месяцам»** — `BarMark` по 6 месяцам `byMonth`, подписи
   русскими короткими месяцами («фев»…«июл», `DashboardMath.monthLabel`).
   ЕДИНЫЙ оттенок `chartAccent` (magnitude одной меры — один цвет), скругление
   4pt, hairline-сетка, выбор столбца (`chartXSelection`) → аннотация
   «фев — 1 200 ₽». Пустой `byMonth` — секция скрыта.
3. **«Траты по дням»** — `BarMark` за последние 30 дней (ось X — даты; дни без
   трат дополняются нулями на клиенте, `DailySum.day` парсит «yyyy-MM-dd»).
   Тонкие бары (`width: .ratio(0.55)`) с зазорами, вершины скруглены 4pt,
   y-сетка `hairline`, подписи осей 11pt `inkSecondary` (X — каждые 7 дней,
   «5 июл»). Выбор бара — `chartXSelection`: RuleMark + аннотация-карточка
   «5 июл — 1 200 ₽».
4. **«Кто платил»** — ДОНАТ `SectorMark(angle:, innerRadius: .ratio(0.62),
   angularInset: 1.5)` цветами участников (`chartCategorical` по правилу id ASC).
   Больше 6 плательщиков — топ-5 сегментов + серый «Прочие»
   (`DashboardMath.donutSlices`). В центре доната — `totalSpent` мелко
   (caption «всего» + `MoneyText` 16pt `.neutral`). Под графиком легенда-столбик
   (identity дублируется легендой, не только цветом): цветная точка 10pt,
   имя (`ink`), сумма + процент (`inkSecondary`), сортировка по убыванию.
5. **«Чья доля»** — горизонтальные `BarMark` по участникам, каждый бар цветом
   СВОЕГО участника (та же карта цветов, что в донате; 7-й и дальше —
   `inkSecondary`), имя слева (`ink`, 13pt), сумма справа direct-label
   (12pt rounded monospacedDigit `inkSecondary`), сортировка по убыванию,
   высота строки ~30pt (бар 16pt), ось X скрыта (значения уже подписаны),
   запас шкалы +⅓ под подписи; нулевые участники скрыты, пустая секция
   не показывается. Тёзки различаются суффиксом « (2)».
6. **«Баланс участников»** — diverging горизонтальные бары от ОБЩЕЙ нулевой
   оси (hairline через все строки): для каждого участника, который есть хоть
   в одном из списков, net = paid − share (`DashboardMath.netBalances`).
   Положительные — вправо цветом `accent`, отрицательные — влево цветом
   `negative` (СЕМАНТИЧЕСКИЕ цвета денег, не категориальные — консистентно
   с остальным приложением), имя слева, сумма справа (`MoneyText` role `.auto`).
   Сортировка по net убыванию.
7. **«По дням недели»** — агрегация `byDay` по дню недели
   (`DashboardMath.weekdayTotals`, пн…вс): 7 колонок `chartAccent`,
   столбец-максимум выделен полной непрозрачностью (остальные — opacity 0.7),
   значения не подписываются (сетка + оси, подписи «пн»…«вс» 11pt
   `inkSecondary`). Все нули — секция скрыта.
8. **«Топ расходов»** — карточный список топ-5: описание (`ink`),
   «донор · дата» вторичной строкой, `MoneyText .neutral` справа.

Правила визуализации: категориальные цвета — только участники и только из
`chartCategorical` (по `user.id` ASC, без циклирования, 7-й+ — `inkSecondary`);
magnitude-графики (месяцы, дни, дни недели) — единый `chartAccent`; деньги
в «Балансе участников» — семантические `accent`/`negative`; текст — только
`ink`/`inkSecondary` (не цветом серии); сетка/оси тише данных; никаких подписей
на каждом баре столбиковых графиков (только аннотация выбора).
Пустая комната (`totalSpent == 0` и нет операций) — дружелюбный empty state
(«Пока нет расходов»). Загрузка — ProgressView, ошибка — «Не удалось
загрузить» + «Повторить», отмена задачи (`isTaskCancellation`) игнорируется.

### Добавление расхода (AddExpenseView)
Максимально близко к Splitwise:
- Заголовок «Добавить расход», кнопки «Отмена»/«Сохранить» (navbar).
- Если группа не выбрана — сверху блок «С кем делите расход?» — горизонтальный выбор группы (чипы).
- Поле описания с иконкой `doc.plaintext` в квадратике слева.
- Поле суммы: большая, `₽` слева серым, numeric keyboard, только целые.
- Карточка деления, сверху — переключатель режима: чипы **«Поровну»** | **«По суммам»**
  (`.softChip(isSelected:)`, дефолт «Поровну» — UI-тест демо-флоу работает в дефолтном режиме).
- Ключевая строка-«предложение», по центру: «Заплатили **вы** и разделено **поровну/по суммам**» — тапабельные сегменты:
  - тап по плательщику → sheet «Кто заплатил?» со списком участников (radio);
  - тап по «поровну»/«по суммам» → sheet «Разделить между» — участники с чекбоксами (все включены по умолчанию); внизу подпись по режиму (см. ниже).
- Режим «Поровну»: под строкой подпись «1 200 ₽ / 3 = 400 ₽ с человека»
  (пересчитывается); при неровном делении — диапазон «100 ₽ / 3 = 33–34 ₽ с человека»
  (остаток первым получателям, предпросмотр `shares()`); отправка — `recipientIds`.
- Режим «По суммам»: у каждого ВЫБРАННОГО участника строка с полем точной суммы
  (rounded + monospacedDigit, numeric keyboard, только целые); под списком живой
  остаток «Осталось распределить: X ₽» (перерасход — `negative`-цветом, при Σ == sum —
  «Сумма распределена полностью» акцентом); «Сохранить» активна ТОЛЬКО при Σ == sum;
  отправка — `recipientSums: [{userId, sum}]`; участники с долей 0 (пустое поле)
  в `recipientSums` не отправляются — получатель с нулевой долей не участвует
  в делении, сервер отклоняет суммы < 1.
- Валидация: сумма ≥ 1, описание непустое, ≥ 1 участник; в «По суммам» — Σ долей == сумме. Ошибки — alert.
- Режим редактирования: тот же экран, prefilled (режим — из `operation.splitType`,
  точные доли — из хранимых `recipients[].sum`), заголовок «Изменить расход».

### AI-добавление расхода (голос + фото чека)

Расход можно продиктовать голосом или сфотографировать чек — сервер распознаёт ввод
(Gemini) и заполняет **ту же** форму `AddExpenseView`. AI **не создаёт операцию**:
пользователь всегда видит черновик перед «Сохранить» и правит его руками, голосом или
тапом. Дальше — обычный путь `save() → clientOpId → outbox → noteDataChanged()`.
Контракт данных — `docs/API.md` (`POST /operations/parse`, поле `items`); транспортные
модели `Draft`/`OperationItem`/`ItemShare` в `Core/Models.swift`, вызов
`APIClient.parseOperation(roomId:audio?:image?:text?:draft:)` (multipart к нашему серверу).

- **AI-композер** — крупный блок только на **пустой** форме (`model.isEmptyForm`):
  большая кнопка микрофона (**hold-to-talk**, `DragGesture`) и «Сфотографировать чек»
  (`confirmationDialog` Камера/Галерея). Как только черновик заполнен, композер уступает
  место обычной форме, а компактный микрофон переезжает в **нижнюю панель** рядом с
  «Сохранить» (голосовая правка поверх готового черновика). AI-кнопки `.disabled` в офлайне
  (`!session.isOnline`) и во время распознавания (`model.isParsing` — спиннер `parsingOverlay`).
  Аудио пишется в `audio/aac`, фото — `image/jpeg` даунскейл до ~1024px (см. `AudioRecorder`,
  `ReceiptCapture`).
- **Карточка-чек** (`ReceiptCardView`) — визуальная метафора бумажного чека: перфорация по
  краю (`PerforationStrip` — полукруги цвета фона), пунктирные разделители (`DashedDivider`),
  моноширинные цифры (`MoneyText`/`.monospacedDigit()`), подвал **Подытог → Сборы → Итого**.
  У позиции с неравными весами — бейдж **`×N`**, у позиции с фикс-суммой — замочек
  (`lock.fill`). Только семантические токены `Theme.swift`, безупречная тёмная тема.
- **Шит позиции** (`ItemSheetView`, тап по строке чека) — segmented `Picker`
  **«Долями / Суммами»**: на строке участника — **ровно один** контрол (степпер веса ИЛИ
  поле точной суммы, не оба). Участие переключается тапом по имени; **пустое поле суммы =
  «авто»** (доля считается по весу). Надбавка (`surcharge`) — только название и цена.
- **Нераспознанные имена** (`unknown`) — **красные чипы** в карточке-чеке; тап → пикер
  участника (`UnknownPickerView`), выбор применяет доли локально и **best-effort** дозаписывает
  прозвище (`POST /users/{id}/aliases`) — в следующий раз AI сматчит имя сам.
- **`canSave = false` пока есть нераспознанные имена** (`hasUnknownItems`) — сохранить
  itemized-операцию с `unknown` нельзя (сервер вернёт `400`), под чеком подсказка
  «Выберите, кто такой „…"». Также блок при невыводимых долях (перебор фиксов над ценой).
- **Правка сохраняет позиции.** При открытии itemized-операции из `OperationDetailView`
  → «Изменить» позиции грузятся в `model.draftItems` и рисуются карточкой-чеком; `save()`
  шлёт `updateOperation(…, items:)` (PUT с `items`). Плоская же правка (ручное изменение
  суммы, чип «Поровну на всех» → `model.resetItems()`) **сбрасывает** позиции — тогда
  сервер затирает `items` и операция становится обычной (паритет с ботом/Android).
  `OperationDetailView` показывает позиции itemized-операции карточкой-чеком.

### Погашение долга / Settle up (SettleUpView)
- Sheet в фирменном зелёном стиле: заголовок «Записать платёж».
- Крупно по центру: аватар должника → стрелка → аватар кредитора, под ними «Загир платит Алмазу».
- Поле суммы (prefilled текущим долгом), редактируемое, но ≤ долга (иначе inline-ошибка «Не больше долга: 500 ₽»).
- Если долгов с участием пользователя несколько — сначала список «Ваши долги» для выбора.
- Кнопка «Записать платёж» — зелёная, во всю ширину.

### Вкладка «Друзья» (FriendsListView)
- Шапка: «Общий баланс: вам должны 1 500 ₽» (или «вы должны», по нетто всех друзей).
- Строки друзей: аватар, имя; справа «должен(на) вам 500 ₽» зелёным / «вы должны 300 ₽» оранжевым / «расчёт» серым.
- Тап → FriendDetailView: шапка с аватаром и нетто-балансом; секция «По группам»: строки «<группа>: вам должны 500 ₽»; тап по строке ведёт в группу.
  Переданный из списка `FriendBalance` — только начальное состояние: экран сам
  подтягивает актуальный баланс (`.task` + `.refreshable` + `dataVersion`, GET /friends и поиск по id).

### Вкладка «Активность» (ActivityView)
- Лента: аватар автора (donor), текст «**Загир** добавил(а) «Ужин» в группе «Стамбул»» /
  для погашений «**Загир** заплатил(а) **Алмазу** 500 ₽ в группе «Стамбул»»;
  вторая строка: ваша позиция: «Вы получили 800 ₽» зелёным / «Вы должны 400 ₽» оранжевым / серым «Вы не участвуете»;
  третья: относительное время («2 ч назад», RelativeDateTimeFormatter, ru).
- Пагинация: подгрузка при прокрутке (limit/offset). Pull-to-refresh.
- Тап по элементу → экран группы.

### Вкладка «Профиль» (AccountView)
- Шапка: большой аватар, имя, @username, id.
- Секции-настройки: «Имя» (редактирование displayName), «Язык» (ru/en), «Уведомления» (toggle → PATCH /me).
- «Сервер»: текущий base URL (read-only текст, мелко).
- Кнопка «Выйти» красным.

### Логин (LoginView)
- Нейтральный премиум-экран: словомарка «Splitty», подзаголовок «Делите расходы с друзьями».
- Основная секция «Вход через Telegram» (surface-карточка): инструкция
  «Откройте @split_money_bot и отправьте команду /login — бот пришлёт код»;
  поле «Код из Telegram» (капс `.characters`, без автокоррекции, monospaced);
  кнопка «Войти по коду» (primary pill), активна при ≥ 6 значимых символов
  (`LoginCode.isValid`; пробелы игнорируются, регистр приводится к верхнему) →
  `POST /auth/code` c телом `{"code":"ABCD2345"}`. На 401 `invalid_code`
  (неверный/просроченный/использованный код) — алерт «Неверный или просроченный код».
- Блок «Вход для разработки» (dev): поля «Telegram ID» (число) и «Имя», username опционально;
  кнопка «Войти» → `POST /auth/dev`. На симуляторе всегда раскрыт — UI-тест DemoFlowUITests
  зависит от лейблов «Telegram ID»/«Имя»/«Войти»; на устройстве свёрнут в DisclosureGroup
  (`#if targetEnvironment(simulator)` задаёт начальное состояние).
- Поле «Сервер» (advanced, свёрнуто DisclosureGroup): base URL, по умолчанию `http://127.0.0.1:7171`.

## Архитектурный контракт (обязателен для всех агентов)

MVVM на `@Observable` (Observation), Swift Concurrency (`async/await`, `@MainActor` для VM), `NavigationStack`.
Никаких сторонних зависимостей — только стандартный SDK. Все строки UI — по-русски, без Localizable (MVP).

### Расположение файлов (создаются каркасом; фичи меняют только свои файлы)

```
ios/
  project.yml
  Splitty/
    App/SplittyApp.swift        // @main, DI: SessionStore в environment
    App/RootView.swift          // session.isAuthenticated ? MainTabView : LoginView
    App/MainTabView.swift       // 5 вкладок + центральная зелёная кнопка
    Core/Theme.swift            // цвета-токены, модификаторы
    Core/Money.swift            // money(_:currency:), rubles(), currencySymbol(), aggregateByCurrency()
    Core/Models.swift           // Codable DTO из docs/API.md
    Core/APIClient.swift        // все эндпоинты; APIError
    Core/SessionStore.swift     // @Observable: token/me/baseURL, login/logout, Keychain,
                                // network/cache/outbox/repo, syncOutbox() (офлайн-режим)
    Core/NetworkMonitor.swift   // NWPathMonitor → @Observable isOnline
    Core/OfflineStore.swift     // файловый read-кеш GET (Application Support/SplittyCache)
    Core/DataRepo.swift         // «кеш сразу → сеть → перезапись» поверх APIClient
    Core/OutboxStore.swift      // outbox локальных расходов (outbox.json) + FIFO-синк
    Core/KeychainStore.swift
    Core/UserAvatarView.swift
    Core/DateFmt.swift          // форматтеры дат (ru locale)
    Features/Auth/LoginView.swift
    Features/Groups/{GroupsListView,GroupsListViewModel,GroupDetailView,GroupDetailViewModel,
                     CreateGroupView,JoinGroupView,GroupSettingsView,GroupBalancesView,
                     GroupTotalsView,OperationDetailView}.swift
    Features/Expense/{AddExpenseView,AddExpenseViewModel}.swift
    Features/SettleUp/SettleUpView.swift
    Features/Friends/{FriendsListView,FriendsViewModel,FriendDetailView}.swift
    Features/Activity/{ActivityView,ActivityViewModel}.swift
    Features/Account/AccountView.swift
  SplittyTests/MoneyTests.swift  // + OfflineStoreTests, OutboxStoreTests, OfflineEditPolicyTests…
```

### Ключевые типы (сигнатуры фиксированы, реализует каркас)

```swift
// Models.swift — имена типов: User, Me, Debt, Operation, OperationRecipient, SplitType,
// RoomSummary, RoomDetail, FriendBalance, FriendRoomBalance, ActivityItem, OperationFile,
// CurrencySum, CurrencyInfo, Statistics (+ DailySum, MonthlySum, MemberSum, TopOperation — дашборд)
// суммы: Int; id комнат/операций: String; id пользователей: Int
// APIClient.swift дополнительно: ExpenseSplit (.equally/.byExactAmount), RecipientSum, OperationBody

// APIClient.swift
final class APIClient {
    init(baseURL: URL?, token: String?)   // nil — невалидный адрес: каждый запрос бросит APIError.invalidURL
    var onUnauthorized: (() -> Void)?     // вызывается при любом 401 (SessionStore делает logout)
    func devLogin(userId: Int, displayName: String, username: String?) async throws -> AuthResponse
    func loginWithCode(_ code: String) async throws -> AuthResponse   // POST /auth/code; 401 invalid_code
    func me() async throws -> Me
    func updateMe(displayName: String?, lang: String?, notificationOn: Bool?) async throws -> Me
    func rooms(archived: Bool) async throws -> [RoomSummary]
    func createRoom(name: String) async throws -> RoomDetail
    func room(id: String) async throws -> RoomDetail
    func joinRoom(id: String) async throws -> RoomDetail
    func archiveRoom(id: String) async throws
    func unarchiveRoom(id: String) async throws
    func operations(roomId: String, type: String) async throws -> [Operation]
    // split: .equally(recipientIds:) → тело {recipientIds}; .byExactAmount(recipientSums:) → {recipientSums}
    // clientOpId — идемпотентный ключ создания (uuid записи outbox): повтор → 200 + существующая операция
    func addOperation(roomId: String, description: String, sum: Int, donorId: Int, split: ExpenseSplit, clientOpId: String? = nil) async throws -> Operation
    func updateOperation(roomId: String, operationId: String, description: String, sum: Int, donorId: Int, split: ExpenseSplit) async throws -> Operation
    func deleteOperation(roomId: String, operationId: String) async throws
    func debts(roomId: String, involving: String) async throws -> [Debt]
    func repay(roomId: String, debtorId: Int, lenderId: Int, sum: Int) async throws -> Operation
    func friends() async throws -> [FriendBalance]
    func activity(limit: Int, offset: Int) async throws -> [ActivityItem]
    func statistics(roomId: String) async throws -> Statistics   // новый контракт дашборда (currency/byDay/…)
    func currencies() async throws -> [CurrencyInfo]              // GET /currencies — справочник для пикера
    func setRoomCurrency(roomId: String, currency: String) async throws   // PUT /rooms/{id}/currency → 204
    func fileData(id: String) async throws -> Data   // GET /files/{fileId} — вложение операции
}

// SessionStore.swift
@Observable final class SessionStore {
    var me: Me?
    var isAuthenticated: Bool { get }
    var api: APIClient { get }        // всегда актуальный (token/baseURL); 401 из любого запроса → logout()
    var repo: DataRepo { get }        // чтение с офлайн-кешем поверх api (@MainActor)
    let network: NetworkMonitor       // NWPathMonitor
    var isOnline: Bool { get }        // == network.isOnline
    let cache: OfflineStore           // read-кеш (чистится в logout)
    let outbox: OutboxStore           // outbox локальных расходов (чистится в logout)
    var baseURLString: String         // персистится в UserDefaults
    var serverURL: URL? { get }       // nil — строка невалидна (НЕ подменяется дефолтом)
    var dataVersion: Int { get }      // версия данных, растёт после каждой мутации
    func noteDataChanged()            // bump dataVersion (звать после успешной мутации)
    func syncOutbox() async           // FIFO-синк outbox; успех хотя бы одной → noteDataChanged()
    func loginDev(userId: Int, displayName: String, username: String?) async throws
    func loginWithCode(_ code: String) async throws   // одноразовый код из Telegram-бота
    func logout()                     // @MainActor; чистит токен, кеш и outbox
    func refreshMe() async            // через repo.me: офлайн-старт берёт профиль из кеша
}

// DataRepo.swift — кешируемые GET (возвращают CachedResult<T> {value, isFromCache}):
// me, rooms(archived:), room(id:), friends(), activityFirstPage(limit:),
// statistics(roomId:), currencies() — все с onCached-колбэком «кеш мгновенно»;
// мутации и прочие запросы — через repo.api.

// OutboxStore.swift
struct OutboxPayload: Codable { description, sum, donorId, recipientIds|recipientSums }
struct OutboxEntry: Codable, Identifiable { localId UUID, roomId, kind create|update|delete,
                                            payload?, targetOperationId?, createdAt, status pending|failed(message) }
@Observable final class OutboxStore {
    private(set) var entries: [OutboxEntry]     // FIFO
    private(set) var isSyncing: Bool            // баннер «Отправка…»
    func entries(roomId: String) -> [OutboxEntry]
    func add(roomId: String, payload: OutboxPayload) -> OutboxEntry   // @MainActor, как и мутации ниже
    func update(localId: UUID, payload: OutboxPayload)  // failed → pending
    func remove(localId: UUID)
    func markFailed(localId: UUID, message: String)
    func clear()
    func sync(api: APIClient) async -> Bool     // FIFO, сериализовано; true — что-то отправлено
}
```

Доступ из view: `@Environment(SessionStore.self) private var session`, вызовы `session.api.…`.

### Конвенции

- VM: `@MainActor @Observable final class XxxViewModel`, состояние `enum LoadState { idle, loading, loaded, failed(String) }` где уместно; методы `load() async`, `refresh() async`.
- Все списки: `.refreshable`, состояния загрузки (`ProgressView`) и пустые состояния (дружелюбный текст + SF Symbol).
- **Единая инвалидация данных**: после каждой успешной мутации (создание/правка/удаление
  расхода, платёж, создание/join/архив комнаты) мутирующее view зовёт
  `session.noteDataChanged()`. Экраны-списки (Группы, Друзья, Активность, Детали группы,
  Друг) перезагружаются по `.onChange(of: session.dataVersion)` и по возврату на экран.
  `.task`/`.onAppear` вешать на ВНУТРЕННИЙ контент (List) внутри `NavigationStack`,
  а не на сам стек — иначе при pop он не срабатывает.
- Отмена запроса (`CancellationError`/`URLError.cancelled`, уход с экрана/вкладки) —
  НЕ ошибка: проверять `error.isTaskCancellation` (extension в APIClient.swift)
  и молча выходить, не трогая state/alert.
- Кнопки-мутации защищать от двойного тапа: `guard !isSaving` в начале async-метода
  плюс `.disabled(isSaving)`.
- Ошибки сети → alert с `error.localizedDescription` (в `APIError` человекочитаемые русские сообщения из тела ошибки бэкенда).
- Невалидный адрес сервера НЕ подменяется дефолтным: запросы бросают
  `APIError.invalidURL` («Некорректный адрес сервера»), пользователь видит алерт.
- 401 из любого запроса централизованно сбрасывает сессию (`APIClient.onUnauthorized`
  → `SessionStore.logout()`, токен чистится из Keychain, показывается экран логина).
- `Info.plist`: `NSAppTransportSecurity → NSAllowsLocalNetworking=true` + exception для `localhost`/`127.0.0.1` (HTTP dev).
- Сборка: `cd ios && xcodegen generate && xcodebuild -project Splitty.xcodeproj -scheme Splitty -destination 'platform=iOS Simulator,name=iPhone 16 Pro,OS=18.5' -quiet build` (строго `OS=18.5` — `OS=latest` на этой машине падает).
