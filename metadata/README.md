# Витрины App Store и Google Play

Здесь лежит всё, что уходит в магазины: тексты, кадры и графика. Источник
правды — эти файлы, а не веб-интерфейсы: правка в консоли переживёт ровно до
следующей заливки, правка здесь остаётся.

## Что где

```
app-info/<loc>.json        имя, подзаголовок, ссылка на политику (App Store)
version/<ver>/<loc>.json   описание, ключевые слова, промо-текст, «что нового»
version/next/<loc>.json    то же, но номер версии ещё не назначен
play/<loc>.json            заголовок, короткое и полное описание (Google Play)
screenshots/<lang>/        СЫРЫЕ кадры iOS, 1320×2868
screenshots-android/<lang>/ СЫРЫЕ кадры Android, 1280×2856
screenshots-framed/<lang>/ оформленные кадры App Store, 1320×2868
screenshots-framed-android/<lang>/ оформленные кадры Google Play, 1080×1920
feature-graphic/<loc>.png  обязательная графика Google Play, 1024×500
```

Каталог версии выбирает `ios/asc/push_metadata.py` по номеру редактируемой
версии в ASC. Пока такой версии нет, тексты лежат в `version/next`: заводить
версию в App Store Connect — ручной шаг, и придумывать номер за человека
скрипт не должен.

Локали именуются по-разному, потому что так их называют сами магазины:
App Store — `ru`, `en-US`, `es-ES`, `de-DE`, `fr-FR`, `ja`, `zh-Hans`, `ko`,
`pt-BR`, `it`; Google Play — `ru-RU` вместо `ru`, `zh-CN` вместо `zh-Hans`.
Каталоги кадров идут по языку приложения: `ru`, `en`.

## Как пересобрать

Нужен локальный бэкенд с демо-данными и поднятые симулятор/эмулятор.

```bash
# 1. Бэкенд на всех интерфейсах: эмулятор Android ходит на 10.0.2.2.
# Отдельная база: демо-данные витрины не должны мешаться с рабочими.
# Собирать go1.24.6 — сборка go1.22.12 (она для CI) не стартует на macOS,
# dyld ругается на missing LC_UUID.
GOTOOLCHAIN=go1.24.6 go build -o bin/splitty ./cmd/splitty
LISTEN=0.0.0.0:7171 DB_NAME=splitty_shots API_JWT_SECRET=<любой> \
  API_DEV_AUTH=true ./bin/splitty

# 2. Демо-данные: своя витринная учётка, группы и расходы на каждый язык
python3 scripts/seed-store-shots.py
docker cp scripts/backdate-shots.js splitty-test-mongo:/tmp/backdate.js
docker exec splitty-test-mongo mongosh splitty_shots --quiet --file /tmp/backdate.js

# 3. Сырые кадры
cd ios && TEST_RUNNER_SHOTS_LANG=ru TEST_RUNNER_SHOTS_EMAIL=shots-ru@splitty.test \
  xcodebuild test -project Splitty.xcodeproj -scheme Splitty \
  -destination 'platform=iOS Simulator,name=Splitty Shots 6.9' \
  -only-testing:SplittyUITests/StoreShotsUITests
python3 marketing/android-shots.py ru en

# 4. Оформление и графика
python3 marketing/frame-shots.py ios ru en
python3 marketing/frame-shots.py play ru en
python3 marketing/feature-graphic.py

# 5. Заливка
python3 ios/asc/push_metadata.py
python3 ios/asc/push_screenshots.py
python3 marketing/push-play-listing.py
```

## Грабли

- **Keychain переживает переустановку.** Между съёмками разных языков
  обязателен `xcrun simctl keychain <udid> reset`, иначе приложение остаётся
  залогиненным под прежней учёткой и кадры выходят не на том языке.
- **Валюта группы видна в кадре.** У англоязычных демо-групп она EUR: рубли
  в витрине App Store US выглядят ошибкой.
- **Google Play не берёт кадр, у которого длинная сторона больше удвоенной
  короткой.** Сырой снимок Pixel — 1280×2856 (1:2.23), поэтому профиль `play`
  оформляет в 1080×1920.
- **«Что нового» у ПЕРВОГО релиза не редактируется** — ASC отвечает 409 и
  режет весь запрос. `push_metadata.py` это учитывает.
- **Правам Play нужно время.** Свежевыданное «Manage store presence»
  доходит до API не мгновенно.
