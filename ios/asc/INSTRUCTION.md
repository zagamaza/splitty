# Splitty (iOS) → App Store Connect / TestFlight

Полная инструкция по заливке. Всё headless, без GUI Xcode.

## Идентификаторы

| Что | Значение |
|---|---|
| Bundle ID | `com.zagir.splitty` (ASC id `WHJCASA6A5`) |
| Team ID | `K8922Y6R3M` |
| App в ASC | `Splitttty`, app id `6787746052` |
| Issuer ID | `a30d44ef-0dc4-4c01-bb7b-7235968f61f8` |
| ASC API Key | `T6PMYHX4T7` (рабочий для distribution), запасной `CZ62LFKG7N` |
| Internal-группа | `Internal`, id `30552ae6-0874-42da-a579-a1a80f8d3073` |
| Public-группа | `Public`, id `328fc762-514b-460c-ba01-5c4e9aed5faa`, ссылка `https://testflight.apple.com/join/pX8rSTns` |

Проект: XcodeGen (`ios/project.yml`), схема `Splitty`, конфиг `Release`.
Подпись — automatic + cloud signing через ASC API-ключ.

## Предустановка на новой машине

```bash
# 1. ASC API-ключи — этого достаточно для всего остального
mkdir -p ~/.appstoreconnect/private_keys
cp keys/AuthKey_*.p8 ~/.appstoreconnect/private_keys/
chmod 600 ~/.appstoreconnect/private_keys/*.p8

# 2. Provisioning-профили (опционально — cloud signing выпустит свои)
mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
cp profiles/*.mobileprovision ~/Library/MobileDevice/Provisioning\ Profiles/

# 3. Инструменты
brew install xcodegen        # + Xcode и Command Line Tools
```

### Про сертификаты подписи

**Отдельный .p12 не нужен.** Подпись идёт через *cloud signing*: `xcodebuild` с флагами
`-allowProvisioningUpdates -authenticationKeyPath/-KeyID/-KeyIssuerID` сам выпускает
distribution-сертификат и provisioning-профиль через ASC API. Именно так собраны все
билды 1.0(1) … 1.2(5). На чистой машине достаточно `.p8` из `keys/`.

Приватные ключи существующих сертификатов **невозможно выгрузить через CLI** — macOS
защищает их в keychain (`security export` отдаёт `The contents of this item cannot be
retrieved`). В `certs/certs-only.p12` лежат только *публичные* сертификаты — для подписи
они непригодны, оставлены для справки.

Если всё же нужен именно перенос существующего сертификата (а не выпуск нового):
Keychain Access → найти `Apple Distribution: Zagir Nurgaliev (K8922Y6R3M)` → правой кнопкой →
Export → `.p12` (потребует пароль от учётки Mac). Это единственный рабочий путь, и он ручной.

Проверка после сборки: `security find-identity -v -p codesigning` — должны появиться
`Apple Distribution: Zagir Nurgaliev (K8922Y6R3M)` и `Apple Development: …`.

## Заливка билда

Переменные (один раз в шелле):

```bash
export ASC_KEY=T6PMYHX4T7
export ASC_ISSUER=a30d44ef-0dc4-4c01-bb7b-7235968f61f8
export ASC_P8=~/.appstoreconnect/private_keys/AuthKey_$ASC_KEY.p8
cd /path/to/splitty/ios
```

### 1. Поднять номер сборки

Номер обязан строго расти и быть уникальным, иначе ASC отклонит («build already exists»).
Правится в `ios/project.yml` → `CFBundleVersion`. Последний залитый — **5** (версия 1.2).

```bash
grep -E "CFBundleShortVersionString|CFBundleVersion" project.yml
# поднять CFBundleVersion на следующий номер
```

### 2. Генерация проекта

```bash
xcodegen generate
```

### 3. Архив (Release)

```bash
rm -rf build/Splitty.xcarchive build/export
xcodebuild -project Splitty.xcodeproj -scheme Splitty -configuration Release \
  -archivePath build/Splitty.xcarchive -destination 'generic/platform=iOS' \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_P8" \
  -authenticationKeyID "$ASC_KEY" \
  -authenticationKeyIssuerID "$ASC_ISSUER" \
  archive
```

### 4. Экспорт IPA

`ExportOptions.plist` лежит в `ios/` (method: `app-store`, signingStyle: `automatic`).

```bash
xcodebuild -exportArchive -archivePath build/Splitty.xcarchive \
  -exportOptionsPlist ExportOptions.plist -exportPath build/export \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_P8" \
  -authenticationKeyID "$ASC_KEY" \
  -authenticationKeyIssuerID "$ASC_ISSUER"
# на выходе: build/export/Splitty.ipa
```

### 5. Загрузка

```bash
xcrun altool --upload-app -f build/export/Splitty.ipa -t ios \
  --apiKey "$ASC_KEY" --apiIssuer "$ASC_ISSUER"
```

Успех = `UPLOAD SUCCEEDED with no errors`. Обработка Apple ~5–15 мин.

### 6. Привязать билд к внутреннему тесту

Внутренний тест не требует Beta Review — тестеры получают билд сразу.
Скрипт `asc.py` (в архиве) уже настроен на нужный issuer/ключи.

```bash
python3 asc.py attach-build <BUILD_VERSION>   # напр. 6
```

Или вручную через API: `POST /v1/betaGroups/30552ae6-.../relationships/builds`
с телом `{"data":[{"type":"builds","id":"<build-uuid>"}]}`.

## Грабли (важно)

- **Только altool + API-ключ.** Через Xcode-аккаунт/Organizer в headless НЕ работает.
- **Ключ для distribution — `T6PMYHX4T7`.** `CZ62LFKG7N` даёт `Cloud signing permission error`
  на экспорте app-store (прав не хватает), но годится для dev-подписи и регистрации устройств.
- **Иконка обязательна.** Без `ASSETCATALOG_COMPILER_APPICON_NAME: AppIcon` и
  `CFBundleIconName: AppIcon` (оба уже в `project.yml`) ASC отклоняет билд с ошибками 90022/90713.
- **Export compliance.** `ITSAppUsesNonExemptEncryption: false` прописан в `project.yml` —
  без него ASC будет спрашивать про шифрование при каждом билде.
- **Внешний (публичный) тест требует Beta App Review.** Пока не одобрен, публичная ссылка
  отдаёт «Beta not found». Билд нельзя привязать к внешней группе до одобрения.
- **Внутренний тест — мгновенный.** Тестеры должны быть пользователями команды ASC.
- iOS-сборки на этой машине: destination `iPhone 16 Pro, OS=18.5` (`OS=latest` падает).

## Установка на физическое устройство (dev)

```bash
xcrun devicectl list devices                 # найти id устройства
# зарегистрировать UDID в портале (asc.py register-device <UDID> <Name>)
xcodebuild -project Splitty.xcodeproj -scheme Splitty -configuration Debug \
  -destination "id=<DEVICE_ID>" -derivedDataPath build/DerivedData \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_P8" -authenticationKeyID "$ASC_KEY" \
  -authenticationKeyIssuerID "$ASC_ISSUER" build
xcrun devicectl device install app --device <DEVICE_ID> \
  build/DerivedData/Build/Products/Debug-iphoneos/Splitty.app
```

Требования на телефоне: **Developer Mode включён** (Настройки → Конфиденциальность и
безопасность → Режим разработчика, затем перезагрузка) и **телефон разблокирован**
в момент установки — иначе `developer disk image could not be mounted`.

Зарегистрированные устройства: `Maza` (`00008130-00160D283CD2001C`),
`iPhone Aleksander` (`00008110-001451A21AD9801E`), `imbir1`.

## Тестеры

Внутренняя группа `Internal`: `zagirnur@gmail.com`, `almazic91@gmail.com`.

Добавить нового: `python3 asc.py add-tester <email> <FirstName> <LastName>`

## ⚠️ Безопасность

`keys/` и `certs/` — приватные ключи, дающие полный доступ к Apple Developer аккаунту
(выпуск сертификатов, заливка билдов от твоего имени). Не коммитить в git,
не выкладывать. При компрометации — отозвать ключи в ASC → Users and Access →
Integrations и перевыпустить сертификаты.
