# Редизайн «Общий чек»: всё приложение, iOS и Android

## Overview

Пользователю категорически не нравится текущий вид («финтех-минимализм»: белые
скруглённые карточки с тенью, изумрудный акцент, rounded-шрифт). Из четырёх
концепций он выбрал «Общий чек»: туса — общий открытый счёт, расходы — его
позиции, расчёт — закрытие остатка. Задача — перевести на этот язык ВСЁ
приложение на обеих платформах, сохранив поведение и денежную логику.

- Направление и правила от Codex: `docs/design/receipt/direction.md`
- Одобренные макеты: `docs/design/receipt/receipt/{welcome,groups,group,add,settle}.html|png`
  (открываются локально, CSS/JS лежат рядом)
- Цель пользователя дословно: «сделать как в референсах Codex». Вопросы по ходу
  задаются Codex, а не пользователю. Codex — ревьюер, писатель — Claude.

## Context (from discovery)

- Рабочий checkout: `splitty-app`, ветка `feature/ios-app` (выбор пользователя).
- Цвета уже семантические и централизованы: iOS `ios/Splitty/Core/Theme.swift`
  (`bg`, `surface`, `ink`, `inkSecondary`, `accent`, `negative`, `hairline`,
  `receiptPaper`, `accentText`, `negativeText`, `chartCategorical`…), Android
  `ui/theme/Theme.kt` (`SplittyColors`, `Splitty.colors`). Прямых hex в экранах:
  iOS 5, Android 15.
- Общие элементы: iOS `Core/DesignSystem.swift` (`scaledFont` с `.rounded` по
  умолчанию, `surfaceCard` — 70 мест, `PrimaryPillButtonStyle`, `SoftChip`,
  `MoneyText`, `MoneyTotalsText`, `sectionHeaderStyle`), `Core/Components.swift`
  (`FailedStateView`, `SuccessToast`); Android `ui/components/DesignSystem.kt`
  (`SurfaceCard` — 80 мест, `PrimaryPillButton`, `SoftChip`, `MoneyText`,
  `AnimatedMoneyText`, `MoneyTotalsText`, `SectionHeader`), `FailedState.kt`,
  `AppToast.kt`, `CacheNote.kt`, `GradientAvatar.kt`.
- Разбросанные скругления: iOS 41 `cornerRadius`, Android 73 `RoundedCornerShape`.
- Данные для «чека» есть: `RoomDetail.totalSpent(Minor)`, `mySpent(Minor)` —
  настоящая доля пользователя (поэтому «на каждого» не нужен и не показывается),
  `myBalance(Minor)`, `debtsUnavailable`, `createdAt`, `members`.
- Проверки: iOS `ContrastTests`, `SnapshotRenderTests`, `HardcodedTextTests`,
  `LocalizationCatalogTests`, UI-тесты (`StoreShotsUITests`, `DashboardShotsUITests`,
  `ManualSharesAfterParseUITests`…); Android `ContrastTest`, Roborazzi
  `ComponentSnapshotTest`, compose-тесты экранов, `LocalizationCatalogTest`.

### Инвентаризация экранов

| # | Экран | iOS | Android |
|---|---|---|---|
| 1 | Приветствие | `Onboarding/WelcomeView.swift` | `onboarding/WelcomeScreen.kt` |
| 2 | Вход | `Auth/LoginView.swift` | `auth/LoginScreen.kt` |
| 3 | Каркас, табы | `App/MainTabView.swift`, `RootView.swift` | `main/MainScaffold.kt`, `AppRoot.kt` |
| 4 | Список тус, архив | `Groups/GroupsListView.swift` | `groups/GroupsListScreen.kt` (+ `ArchivedGroupsScreen`) |
| 5 | Туса: лента | `Groups/GroupDetailView.swift` | `groups/GroupDetailScreen.kt` (`GroupDetailContent`, `DebtHeroCard`, `OperationRow`) |
| 6 | Балансы | `Groups/GroupBalancesView.swift` | `GroupDetailScreen.kt` (`GroupBalancesTab`, `DebtRow`) |
| 7 | Итоги | `Groups/GroupTotalsView.swift` | `groups/GroupDashboardScreen.kt`, `GroupDashboardCharts.kt` |
| 8 | Операция | `Groups/OperationDetailView.swift` | `groups/OperationDetailScreen.kt` |
| 9 | Добавление расхода | `Expense/AddExpenseView.swift`, `ReceiptView.swift`, `MicTouchSurface.swift` | `expense/AddExpenseScreen.kt`, `ReceiptCard.kt`, `ItemSheet.kt`, `RecordingOverlay.kt`, `PersonBreakdownCard.kt` |
| 10 | Рассчитаться | `SettleUp/SettleUpView.swift` | `settleup/SettleUpScreen.kt` |
| 11 | Создать / войти / пригласить | `Groups/CreateGroupView.swift`, `JoinGroupView.swift`, `InviteGroupView.swift`, `InviteFriendView.swift` | `GroupsListScreen.kt` (`CreateGroupSheet`, `JoinGroupSheet`), `GroupDetailScreen.kt` (`InviteBottomSheet`, `InviteFriendsSheet`) |
| 12 | Настройки тусы | `Groups/GroupSettingsView.swift` | `GroupDetailScreen.kt` (`GroupSettingsTab`) |
| 13 | Друзья, друг | `Friends/FriendsListView.swift`, `FriendDetailView.swift` | `friends/FriendsListScreen.kt`, `FriendDetailScreen.kt` |
| 14 | Уведомления (лента) | `Activity/ActivityView.swift` | `activity/ActivityScreen.kt` |
| 15 | Профиль, пароль, настройки пушей | `Account/AccountView.swift`, `ChangePasswordSheet.swift`, `NotificationSettingsView.swift` | `profile/ProfileScreen.kt`, `NotificationSettingsScreen.kt` |
| 16 | Splitor Plus | `Paywall/PaywallView.swift` | `paywall/PaywallSheet.kt` |
| 17 | Системные состояния | `Core/Components.swift`, `CacheNote.swift`, `UserAvatarView.swift` | `components/FailedState.kt`, `AppToast.kt`, `CacheNote.kt`, `GradientAvatar.kt`, `NudgeHighlight.kt` |

## Development Approach

- **testing approach**: Regular. Существующие тесты обязаны остаться зелёными.
  Новые тесты — только на поведение: нет «на каждого» при неравных долях,
  «Балансы недоступны» вместо ложного нуля, декор края/штампа скрыт от
  VoiceOver/TalkBack, правило регистра для ja/zh/ko. Тестов «цвет равен hex» не писать.
- Вид сверяется скриншотами: iOS — симулятор iPhone 17 Pro (+ узкий и крупный
  текст), Android — Roborazzi JVM-снимки и эмулятор, где он поднимется.
- Сначала основа (токены, шрифты, общие элементы) — приложение сразу целиком
  меняет облик; затем экраны по одному перестраиваются под иерархию чека.
- Новые строки — только через каталоги (`Localizable.xcstrings`, `values*/strings.xml`)
  во всех 10 языках; русский прямо в исходниках не писать.
- Денежная логика, запросы, модели — не трогать. Только представление.
- Аналитика: редизайн новых шагов не вводит. Если появится новый шаг — событие по
  правилу `CLAUDE.md` (документ → белый список → обе платформы).
- Коммиты — по задаче, явными путями. В develop не пушить без отдельного решения.

## Testing Strategy

- iOS unit: `cd ios && xcodegen generate && xcodebuild test -project Splitty.xcodeproj -scheme Splitty -destination 'platform=iOS Simulator,id=A9000922-4775-45C1-852F-F5676A0D1C55' -only-testing:SplittyTests`
- iOS UI (бэкенд локально, `make seed`): `-only-testing:SplittyUITests`
- Android: `cd android && ./gradlew testDebugUnitTest` (Roborazzi пишет PNG), `./gradlew assembleDebug`
- UI-тесты, завязанные на старые подписи/идентификаторы, обновляются в той же задаче.

## Progress Tracking

- `[x]` сразу по готовности; ➕ — новая задача; ⚠️ — блокер.

## Solution Overview

### Визуальный язык (из макетов)

| Токен | Светлая | Тёмная (семантически) |
|---|---|---|
| `bg` — стол под чеком | `#DEDBD2` | `#16170F`-ish тёмный тёплый |
| `paper` (бывш. `surface`/`receiptPaper`) | `#FFFDF4` | `#24251E` — поверхность различима, не белый лист |
| `paperClosed` (закрытый/архив) | `#E9E8DD` | `#1D1E18` |
| `ink` | `#262722` | `#ECEADF` |
| `inkSecondary` | `#6B6B60` (≥4.5:1 на paper — проверить) | `#A7A595` |
| `rule` (hairline) | `#CBC9BC`; пунктир `#AAA999`; итоговая черта `#2A2D24` | соответствующие светлые |
| `action` (CTA) | `#D65C37`, текст `#FFFDF4` — проверить контраст, при необходимости затемнить | тёплый оранжевый для тёмной |
| `stamp` (штамп состояния) | `#36579C` | осветлённый синий |
| «вам должны / вы должны» | словами, цвет вторичен: `accentText`/`negativeText` пересчитываются под новую палитру | |

- Углы: 2–4 pt у бумаги и кнопок; аватары остаются круглыми.
- Тени: почти нет (лёгкая подложка бумаги в светлой, нет в тёмной).
- Шрифт: системный sans (не rounded) для текста и заголовков; крупные заголовки
  жирные с плотной разрядкой; суммы — tabular figures; моноширинный — только
  короткие реквизиты («ПОЗИЦИЯ / КТО ПЛАТИЛ», даты, «ВАШ ОСТАТОК»).
- Реквизиты капсом с разрядкой — только для языков с регистром; ja/zh/ko —
  без uppercase и tracking.
- Бумага: `ReceiptPaper` — фон paper, рваный (перфорированный) нижний край
  формой, внутренние поля 22–24. Край — декор, скрыт от доступности.
- Пунктир `ReceiptDivider(dashed)`, итоговая черта `ReceiptDivider(total)`.
- Строка `ReceiptLine`: слева название + мелкая подпись (дата · кто платил),
  справа сумма моно/табличными цифрами.
- `ReceiptTotal`: «ВСЕГО» + крупная сумма.
- `StatusStamp`: синяя рамка, поворот −6°, моно — только для реального состояния
  («ВЫ В РАСЧЁТЕ», «ЧЕРНОВИК», «АРХИВ»). От доступности скрыты только рамка и
  поворот; текст состояния читается ровно один раз. Декоративный штрихкод и выдуманные
  номера счетов («№ 004») НЕ переносим: таких сущностей в продукте нет.
- `PrimaryAction`: прямоугольная оранжевая кнопка во всю ширину, 53–54 pt;
  вторичные действия — подчёркнутые текстовые ссылки («Все балансы», «Рассчитаться ↗»).
- Корешок (stub) для списка тус: бумага с пунктиром снизу и полукруглыми
  вырезами по углам; закрытые — `paperClosed`.
- Навигация: системные TabView / NavigationBar остаются (доступность, UI-тесты,
  liquid glass iOS 26), но перекрашиваются: фон `bg`, моно-заголовок по центру
  («ТУСА», «РАСЧЁТ»), текстовая кнопка назад. Четыре раздела + центральное действие
  «+»: для доступности это действие, а не пустая вкладка. Старый приподнятый FAB
  поверх нового прямоугольного CTA не оставлять. Рамку чека вокруг каждого
  элемента не рисовать: туса — один документ, а не прежний дашборд в новой палитре.
- Мок-данных в рабочих экранах нет; пример-чек допустим только на приветствии.

### Экраны (ключевые решения)

- **Список тус**: заголовок «Чеки тус» + «+ Новый»; сегмент «Открытые · N / Закрытые»
  (закрытые = архив); корешки: реквизит (дата создания · N чел.), название,
  остаток словами + сумма. Нулевой баланс ≠ закрыт.
- **Туса**: единый документ — шапка (название, дата создания · N человек) →
  позиции (лента расходов по месяцам, погашения отдельной пометкой) → «ВСЕГО» →
  «Ваша доля» (`mySpent`) → «ВАШ ОСТАТОК» словами → ссылки «Все балансы» /
  «Рассчитаться». `debtsUnavailable` → «Балансы недоступны». Кнопка «+ Дописать
  расход» внизу. Длинная история — ленивая, документ не рендерится одной картинкой.
  Итог, личный остаток и «Рассчитаться» не прячутся за сотнями строк: компактная
  закреплённая сводка над лентой (или под шапкой), всегда достижимая.
- **Добавление**: заголовок «Дописать в чек.», голос/фото/ручной ввод как сейчас;
  результат ИИ — бумажная квитанция «ПРОВЕРКА ПЕРЕД ПЕЧАТЬЮ» с позициями, долями,
  «Изменить доли» / «Снять чек»; пометка «ЧЕРНОВИК / В ОБЩИЙ СЧЁТ ЕЩЁ НЕ ДОБАВЛЕН»;
  CTA «Подтвердить и дописать». Вся логика формы — без изменений.
- **Рассчитаться**: бумага «Без остатка.» / сумма крупно / «Кирилл → вам» / «После
  перевода ваш остаток» / штамп. Остаток «после перевода» вычисляется из реального
  участника и суммы; при частичном платеже или отметке чужого расчёта обещание
  «0» не показывается; ссылки «План всей тусы · N переводов», «Почему?»;
  подпись, что перевод — в банке; CTA «Деньги получены» / «Записать перевод».

## What Goes Where

- Implementation Steps — код, тесты, строки, скриншоты в репозитории.
- Post-Completion — заливка новых скриншотов в сторы, релизы, ручная проверка на телефоне.

## Implementation Steps

### Task 1: Палитра «Общий чек» на обеих платформах

**Files:**
- Modify: `ios/Splitty/Core/Theme.swift`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/theme/Theme.kt`
- Modify: `ios/SplittyTests/ContrastTests.swift`, `android/app/src/test/java/com/zagir/splitty/ui/theme/ContrastTest.kt`

- [x] заменить значения семантических токенов на палитру чека (светлая + тёмная), добавить `paperClosed`, `rule`, `ruleStrong`, `stamp`, `action`, `onAction`
- [x] `surface`/`receiptPaper` указывают на бумагу; легаси-алиасы остаются компилируемыми
- [x] пересчитать `accentText`/`negativeText`/`chartCategorical` под новые фоны
- [x] Android `toColorScheme()` — материал-схема из новых токенов (диалоги, поля, свитчи)
- [x] обновить тесты контраста: пары текст/фон ≥ 4.5:1, CTA-текст на action ≥ 4.5:1
- [x] прогнать тесты обеих платформ

### Task 2: Типографика и реквизиты

**Files:**
- Modify: `ios/Splitty/Core/DesignSystem.swift`
- Modify: `android/app/src/main/java/com/zagir/splitty/ui/theme/Theme.kt` (Typography)
- Create: `ios/SplittyTests/ReceiptTypographyTests.swift`, `android/.../ui/components/ReceiptTypographyTest.kt`

- [x] `scaledFont` по умолчанию `.default` вместо `.rounded`; явные `.rounded` в экранах убрать
- [x] стиль реквизита `receiptLabel` (моно, мелкий, капс с разрядкой) с правилом: ja/zh/ko — без капса и разрядки
- [x] `MoneyText` — tabular figures, крупные суммы — жирные с плотной разрядкой
- [x] Android: Typography без rounded, tabular-nums через `fontFeatureSettings = "tnum"`
- [x] тест правила регистра по локалям (обе платформы)
- [x] прогнать тесты

### Task 3: Компоненты чека

**Files:**
- Create: `ios/Splitty/Core/ReceiptComponents.swift`
- Create: `android/app/src/main/java/com/zagir/splitty/ui/components/ReceiptComponents.kt`
- Modify: `ios/Splitty/Core/DesignSystem.swift`, `android/.../ui/components/DesignSystem.kt`
- Modify: `android/.../ui/components/ComponentSnapshotTest.kt`, `ios/SplittyTests/SnapshotRenderTests.swift`

- [x] `ReceiptPaper` (форма с перфорированным краем, край скрыт от доступности), `ReceiptStub`
- [x] `ReceiptDivider` (dashed / total), `ReceiptLine`, `ReceiptTotal`, `StatusStamp`, `TextLinkButton`
- [x] `surfaceCard`/`SurfaceCard` → бумага с углами 3pt без тени; `PrimaryPill` → прямоугольный `action`; `SoftChip` → рамка 1pt, углы 3pt; `SectionHeader` → `receiptLabel`
- [x] снапшоты компонентов в обеих темах (Roborazzi + iOS render)
- [x] тесты: край и штамп не в дереве доступности
- [x] прогнать тесты

### Task 4: Каркас — навигация, табы, системные состояния

**Files:**
- Modify: `ios/Splitty/App/MainTabView.swift`, `ios/Splitty/App/SplittyApp.swift`, `ios/Splitty/Core/Components.swift`, `ios/Splitty/Core/CacheNote.swift`, `ios/Splitty/Core/UserAvatarView.swift`
- Modify: `android/.../ui/main/MainScaffold.kt`, `components/FailedState.kt`, `AppToast.kt`, `CacheNote.kt`, `GradientAvatar.kt`, `NudgeHighlight.kt`

- [x] внешний вид TabBar/NavigationBar: фон `bg`, hairline, активный таб чернилами; моно-заголовки по центру
- [x] центральная «+» — прямоугольная `action`
- [x] FailedState, тост, CacheNote, аватары — в новой палитре, без пилюль
- [x] убрать оставшиеся хардкод-цвета экранов (iOS 5, Android 15) в токены
- [x] обновить затронутые тесты; прогнать

### Task 5: Приветствие и вход

**Files:**
- Modify: `ios/Splitty/Features/Onboarding/WelcomeView.swift`, `ios/Splitty/Features/Auth/LoginView.swift`
- Modify: `android/.../ui/onboarding/WelcomeScreen.kt`, `android/.../ui/auth/LoginScreen.kt`
- Modify: каталоги строк обеих платформ

- [x] приветствие по `welcome.html`: реквизит, «Всё общее — в одном чеке.», наклонённый пример-чек (данные-пример), штамп, CTA; существующие страницы/пропуск/аналитика сохраняются
- [x] вход: та же бумага и шрифты, три пункта обещаний как строки чека; кнопки Apple/Google/Telegram — по гайдлайнам брендов
- [x] строки во всех 10 языках
- [x] обновить `WelcomeRenderTests`, `LoginValuePropsTests`, Android-тесты приветствия/входа; прогнать

### Task 6: Список тус и архив

**Files:**
- Modify: `ios/Splitty/Features/Groups/GroupsListView.swift`
- Modify: `android/.../ui/groups/GroupsListScreen.kt`, `GroupsCommon.kt`

- [x] «Чеки тус» + «+ Новый» (меню создать/войти по коду сохраняется)
- [x] сегмент «Открытые · N / Закрытые»: закрытые = архив (вместо строки «Архив»)
- [x] корешок тусы: реквизит (дата создания · N чел.), название, остаток словами и сумма; непрочитанное — штамп-метка; `debtsUnavailable` — «Балансы недоступны»
- [x] сводка «вам должны / вы должны» — строкой-реквизитом, не карточкой
- [x] тесты: нулевой баланс открытой тусы не попадает в «Закрытые»; прогнать

### Task 7: Туса — документ

**Files:**
- Modify: `ios/Splitty/Features/Groups/GroupDetailView.swift`
- Modify: `android/.../ui/groups/GroupDetailScreen.kt`

- [x] шапка-документ, позиции по месяцам (`ReceiptLine`), погашения — с пометкой, офлайн-операции — штамп «НЕ ОТПРАВЛЕНО»
- [x] итог: «ВСЕГО», «Ваша доля» (`mySpent`), «ВАШ ОСТАТОК» словами, «Все балансы» / «Рассчитаться»
- [x] вкладки (Лента/Балансы/Итоги/Настройки) — текстовые табы с подчёркиванием; фильтр «Только мои» сохраняется
- [x] «+ Дописать расход» — нижняя `action`; баннер приглашения — строкой-ссылкой
- [x] тесты: `debtsUnavailable` → «Балансы недоступны», а не «в расчёте»; прогнать

### Task 8: Добавление расхода

**Files:**
- Modify: `ios/Splitty/Features/Expense/AddExpenseView.swift`, `ReceiptView.swift`, `MicTouchSurface.swift`
- Modify: `android/.../ui/expense/AddExpenseScreen.kt`, `ReceiptCard.kt`, `ItemSheet.kt`, `RecordingOverlay.kt`, `PersonBreakdownCard.kt`

- [x] заголовок «Дописать в чек.», композер голоса — оранжевый круг с волной, фото и ручной ввод рядом
- [x] результат разбора — квитанция «ПРОВЕРКА ПЕРЕД ПЕЧАТЬЮ», «Изменить доли» / «Снять чек», пометка «ЧЕРНОВИК»
- [x] плоская форма (сумма, плательщик, участники, доли) — на бумаге, нативные поля
- [x] CTA «Подтвердить и дописать» / «Сохранить» — `action`
- [x] UI-тесты (`ManualSharesAfterParseUITests`, Android `ManualSharesButtonTest` и др.) — обновить подписи/идентификаторы; прогнать

### Task 9: Рассчитаться

**Files:**
- Modify: `ios/Splitty/Features/SettleUp/SettleUpView.swift`
- Modify: `android/.../ui/settleup/SettleUpScreen.kt`

- [x] бумага: состояние словами, сумма крупно, «Кто → кому», «После перевода ваш остаток», штамп при нуле
- [x] «План всей тусы · N переводов», «Почему?» — ссылки на существующие объяснения
- [x] подпись: перевод делается в банке; CTA «Деньги получены» / «Записать перевод»; защита от двойного нажатия сохраняется
- [x] тесты экрана обновить; прогнать

### Task 10: Контрольная точка — пять основных экранов

➕ Найдено по живым скриншотам и исправлено: нижний бар Android растягивался на весь экран; сумма в расчёте Android обрезалась; шапка приветствия Android залезала под статус-бар; вырезы корешка на iOS торчали белыми кругами.
⚠️ Вне редизайна: Android спрашивает уведомления в `MainActivity.onCreate` на первом кадре (на iOS — после первой тусы). Не трогал.

- [x] скриншоты пяти экранов: iOS (iPhone 18 Pro, светлая), Android (эмулятор Pixel 7, API 35) — `/tmp/receipt-shots/`; тёмная и узкий — в Task 15
- [x] сравнить с `docs/design/receipt/receipt/*.png`, поправить расхождения
- [ ] отправить Codex через peer-chat: пути, diff, скриншоты; исправить его замечания

### Task 11: Балансы, итоги, операция

**Files:**
- Modify: `ios/.../Groups/GroupBalancesView.swift`, `GroupTotalsView.swift`, `OperationDetailView.swift`
- Modify: `android/.../groups/GroupDetailScreen.kt` (`GroupBalancesTab`), `GroupDashboardScreen.kt`, `GroupDashboardCharts.kt`, `OperationDetailScreen.kt`

- [x] балансы — строки обязательств словами + план переводов на бумаге
- [x] итоги — итоговая часть документа, графики в новой палитре
- [x] операция — развёрнутый чек: позиции, доли, сбор, плательщик; правка/удаление как сейчас
- [x] тесты; прогнать

### Task 12: Создание, вход по коду, приглашения, настройки тусы

**Files:**
- Modify: `ios/.../Groups/CreateGroupView.swift`, `JoinGroupView.swift`, `InviteGroupView.swift`, `InviteFriendView.swift`, `GroupSettingsView.swift`
- Modify: `android/.../groups/GroupsListScreen.kt` (листы), `GroupDetailScreen.kt` (`InviteBottomSheet`, `InviteFriendsSheet`, `GroupSettingsTab`)

- [x] короткие формы на бумаге, нативные поля, `action` и текстовые ссылки
- [x] QR и ссылки приглашения — реальные, без декоративного штрихкода
- [x] тесты; прогнать

### Task 13: Друзья и лента уведомлений

**Files:**
- Modify: `ios/.../Friends/FriendsListView.swift`, `FriendDetailView.swift`, `ios/.../Activity/ActivityView.swift`
- Modify: `android/.../friends/FriendsListScreen.kt`, `FriendDetailScreen.kt`, `android/.../activity/ActivityScreen.kt`

- [x] друзья — люди и общие счета строками, долги по валютам без суммирования
- [x] лента — журнал записей по дням, непрочитанные — явной меткой
- [x] тесты; прогнать

### Task 14: Профиль, пароль, пуши, Splitor Plus

**Files:**
- Modify: `ios/.../Account/AccountView.swift`, `ChangePasswordSheet.swift`, `NotificationSettingsView.swift`, `ios/.../Paywall/PaywallView.swift`
- Modify: `android/.../profile/ProfileScreen.kt`, `NotificationSettingsScreen.kt`, `android/.../paywall/PaywallSheet.kt`

- [x] профиль и настройки — компактные нативные формы в палитре чека
- [x] Plus — бумага с реальными ценами/сроками/лимитами из StoreKit/Billing, ссылки Terms/Privacy (3.1.2(c)), восстановление покупки
- [x] тесты (`SubscriptionRestoreTests` и др.); прогнать

### Task 15: Сквозная проверка качества

➕ Крупный текст ломал суммы и подписи: сумма больше не переносится (`MoneyText` одной строкой с ужатием), строки «подпись — сумма» и позиции тусы на крупном шрифте складываются в столбик (iOS `isAccessibilitySize`, Android `isLargeFontScale`). Проверено: тёмная тема обеих платформ, iPhone 17e + AX L, Android fontScale 2.0, de и ja.

- [x] grep остатков старого стиля: `.rounded`, `cornerRadius: 1[0-9]|2[0-9]`, `RoundedCornerShape(1[0-9]|2[0-9])`, тени карточек
- [x] тёмная тема, iPhone SE-размер, крупный текст (AX3 / fontScale 2), ja/de/ru
- [x] VoiceOver/TalkBack: порядок фокуса, декор скрыт, суммы читаются словами
- [x] паритет: смысловые состояния экранов совпадают на iOS и Android

### Task 16: Скриншоты витрины

➕ Рамка витрины и баннер Play перекрашены из изумрудного градиента в стол и чернила. Съёмка витрины знает о приветствии перед входом (iOS и Android), Android запускается через `am start`. Учётки витрины для zh-Hans и pt-BR — `shots-zh`/`shots-pt`.
⚠️ Иконка приложения осталась зелёной — в план не входила, нужна новая иконка на обе платформы.

**Files:**
- Modify: `metadata/screenshots-framed/**` (перегенерация)

- [x] прогнать `StoreShotsUITests` / `DashboardShotsUITests` на новом дизайне, обрамить как раньше
- [x] Android-скриншоты витрины — тем же путём, что прошлые

### Task 17: Verify acceptance criteria

- [ ] все экраны инвентаризации в новом стиле на обеих платформах
- [ ] iOS unit + UI, Android unit + assembleDebug — зелёные
- [ ] финальное ревью Codex и исправления

### Task 18: [Final] Документация

- [ ] `CLAUDE.md`: короткий раздел о дизайн-системе чека (где токены/компоненты, правила)
- [ ] план → `docs/plans/completed/`

## Post-Completion

- Заливка новых скриншотов в App Store Connect и Play — с явного согласия пользователя.
- Релизные сборки iOS/Android — отдельным решением.
- Ручная проверка на телефоне «Maza» и Android-устройстве.
