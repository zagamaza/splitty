# Матрица экранов «Общий чек» — HEAD 688cc67 (feature/ios-app, не запушено)

Кадры в этой папке. iOS — iPhone 18 Pro (если без суффикса — тоже он), -xl-17e — iPhone 17e AccessibilityL.
Android — эмулятор 411dp; -xl-narrow — 329dp, fontScale 2.0. -dark — тёмная тема.

| # | Группа | iOS | Android |
|---|---|---|---|
| 1 | Приветствие | ios-t10-01-welcome-18pro | and-t10-01-welcome |
| 2 | Вход | ios-t10-01-login(-email)-18pro | and-t10-01-login(-email) |
| 3 | Каркас, табы | ios-02/10/12/13 (+dark), заголовки вкладок | and-m-03-tabs-groups |
| 4 | Список тус, архив | ios-02-groups(-dark), ios-02-groups-return(-dark), ios-m-04-archive-18pro | and-m-03-tabs-groups, and-m-04-archive |
| 5 | Туса: лента | ios-03-group(-scrolled)(-dark) | and-m-05-group |
| 6 | Балансы | ios-06-balances(-dark) | and-m-06-balances |
| 7 | Итоги | ios-07-totals(-dark) | and-m-07-totals |
| 8 | Операция | ios-09-operation(-dark) | and-m-08-operation |
| 9 | Добавление, ИИ | ios-04-add(-dark), ios-t10-04-ai-draft(-scrolled), ios-t10-04-ai-manual | and-t10-04-ai-draft(-scrolled) |
| 10 | Рассчитаться | ios-05-settle(-dark), ios-t10-05-settle-partial*, ios-t10-05-settle-large*, *-xl-17e | and-t10-05-settle-full/partial*/large*, *-xl-narrow* |
| 11 | Создать / вход по коду / пригласить | ios-14-create-group(-dark), ios-m-11-join, ios-m-11-invite-friend, ios-m-11-invite-link | and-m-11-add-menu, and-m-11-create, and-m-11-join, and-m-11-invite, and-m-11-invite-link |
| 12 | Настройки тусы | ios-08-settings(-dark) | and-m-12-settings(-2) |
| 13 | Друзья, друг | ios-10-friends(-dark), ios-11-friend(-dark) | and-m-13-friends, and-m-13-friend |
| 14 | Уведомления | ios-12-activity(-dark) | and-m-14-activity |
| 15 | Профиль, пароль, пуши | ios-13-profile(-scrolled)(-dark), ios-m-15-password, ios-m-15-notif-settings | and-m-15-profile(-2,-3), and-m-15-password, and-m-15-notif-settings |
| 16 | Splitor Plus | ios-m-16-plus-18pro (цены StoreKit) | and-m-16-plus (эмулятор без Play: цены нет) |
| 17 | Системные состояния | ios-m-17-offline-groups/activity-18pro | and-m-17-offline-groups/activity, and-m-17-network-error |

## Найдено и исправлено при обходе (после ccfff6b)
- 191b92c — Android Plus без цены печатал «Оформить за» и « за . Продлевается автоматически…». Тест PaywallWithoutPriceTest.
- 05aaa6f — Android: цена позиции чека отъезжала от правого края (два веса в ряду). Эталоны Roborazzi были записаны со сдвигом — перезаписаны.
- 2f4aedc — iOS: «Добавить в группу» обрезался в заголовке листа; теперь ужимается.
- c3b6330 — компонентный тест 1234567,89 на 260dp при fontScale 2 (NATIVE-графика; ловит и отсутствие подгонки, и кегль в sp).

## Замечено, не тронуто
- Без сети лента уведомлений: Android показывает кеш, iOS — «Не удалось загрузить» (кеша ленты на iOS нет). Поведение, не стиль.
- Android: переключатели в настройках уведомлений — Material-свитч в цвете действия; iOS — системный чёрный.
- Семейство AppRoot* на Android (AppRootJoinTest, AppRootIntroTest) флейкает в полном прогоне (гонка Dispatchers.Main), изолированно зелёное.

## Тесты на 688cc67
go test ./... — зелёный (до этих правок, Go не менялся); iOS unit 467/467; Android testDebugUnitTest 664, 2 падения AppRootJoinTest (флейк, изолированно зелёный).
