# Декларации магазинов после включения аналитики

С 4 сентября 2026 приложение собирает обезличенные продуктовые события
(`docs/analytics-events.md`). Расхождение между тем, что приложение делает, и
тем, что заявлено в магазине, — повод для отказа на ревью, а не формальность.

Что уже сделано в коде: `ios/Splitty/PrivacyInfo.xcprivacy` объявляет
`NSPrivacyCollectedDataTypeProductInteraction` с назначением `Analytics`,
`Linked = true`, `Tracking = false`; текст `/privacy` дополнен разделом о том,
что собирается и сколько хранится.

Осталось два шага **в консолях** — их API не отдаёт.

## App Store: App Privacy

Публичный API App Store Connect nutrition labels не публикует. У `asc` есть
экспериментальный путь через веб-сессию, и он требует пароль и 2FA — их вводит
человек:

```
asc web auth login --apple-id <твой apple id>
asc web privacy pull --app 6787746052 --out ./privacy.json
# правка файла: добавить Product Interaction → Analytics, Linked, без Tracking
asc web privacy plan  --app 6787746052 --file ./privacy.json
asc web privacy apply --app 6787746052 --file ./privacy.json
asc web privacy publish --app 6787746052 --confirm
```

Руками — https://appstoreconnect.apple.com/apps/6787746052/appPrivacy

Что отметить:

- **Data Type:** Product Interaction (раздел Usage Data).
- **Used for:** Analytics.
- **Linked to the user:** ДА. Событие пишется под номером аккаунта — без этого
  воронку не построить, и говорить обратное было бы неправдой.
- **Used for tracking:** НЕТ. Стороннего SDK и рекламных идентификаторов нет,
  данные не уходят третьим лицам, поэтому и ATT-запрос не нужен.

Ничего больше добавлять не надо: содержимого в событиях нет — ни сумм, ни
названий групп, ни описаний расходов.

## Google Play: Data safety

Формы Data safety нет ни в Play Developer API, ни в gradle-play-publisher —
только консоль (там есть импорт CSV, но это тот же ручной путь).

https://play.google.com/console → Splitor → Policy → App content → Data safety

- **Data collected:** App activity → App interactions.
- **Collected / Shared:** collected — да; shared — **нет** (третьим лицам не
  передаётся, своего сервера достаточно).
- **Processed ephemerally:** нет — события хранятся 90 дней.
- **Required or optional:** required (выключателя у человека нет; он константа
  сборки).
- **Purpose:** Analytics.
- **Data is encrypted in transit:** да (HTTPS).
- **Users can request data deletion:** да — удаление аккаунта в приложении
  вычищает события, см. `/account-deletion`.
