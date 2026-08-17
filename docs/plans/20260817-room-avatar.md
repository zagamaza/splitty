# Ава группы

## Overview

У группы должна быть своя фотография. Сейчас ава генерируется из хэша id комнаты (`GroupsListView.swift:380` — берётся `UserAvatarView` с первой буквой названия и градиентом по хэшу), своего фото нет нигде, и загрузить файл из приложения нельзя в принципе.

Чек, привязанный к операции, из этого плана намеренно убран — отложено.

## Context (from discovery)

- бэкенд Go, одна монга без транзакций; комната — **один документ** со всеми операциями внутри, есть страж размера `$bsonSize` (`repository.go:693`)
- своего файлового хранилища нет: ни S3, ни диска
- отдача файлов уже написана: `GET /api/v1/files/{fileId}` (`server.go:383`) проверяет доступ через `userHasFile` (`handlers.go:1830`) и качает байты из телеграма по `file_id`. Эти id кладёт только бот, когда фото шлют в чат
- загрузки файлов из приложения нет
- у операций есть `Files []{type, fileId}` (`service_models.go:103`) — в этом плане не трогаем
- iOS SwiftUI + Observation, Android Compose + Hilt; клиенты — порты друг друга, правки парные

## Решения, принятые до плана

- **Хранилище — монга, не телеграм.** Байты в отдельной коллекции `files`, в комнате только ссылка. Внутрь документа комнаты класть нельзя: там уже все операции, потолок BSON 16 МБ, и ава вычитывалась бы при каждом открытии списка групп.
- **Старые ботовые файлы продолжают работать.** `GET /api/v1/files/{id}` сначала ищет id в коллекции, не нашёл — идёт в телеграм, как раньше. Клиентам менять эндпоинт не нужно.
- **Коллекция сразу с полем `kind`** — чтобы чек, когда до него дойдут руки, лёг рядом без миграции.

## Development Approach

- **testing approach**: Regular — сначала код, следом тест в той же задаче
- каждая поведенческая правка обязана иметь тест, который падает без неё — проверять фактическим откатом строки
- правки — напрямую в основной сессии; субагенты только с явного разрешения
- новые строки заводятся сразу во всех пяти локалях Android и в каталоге iOS
- комментарии в коде на русском, минимальные, объясняют «почему»
- коммиты: автор `AlmazNurmukhametov <zagirnur@gmail.com>`, по-русски, без упоминаний Claude/Anthropic/AI

## Testing Strategy

```
go test ./... -count=1
cd android && ./gradlew :app:testDebugUnitTest
cd ios && xcodebuild test -project Splitty.xcodeproj -scheme Splitty \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -only-testing:SplittyTests
```

Тесты репозитория требуют живой монги (`MONGO_TEST_URI`) и **не должны скипаться**.

Известные флаки, ослаблять нельзя: `AppRootJoinTest` (Android), `OfflineSmokeUITests` (iOS, нужен локальный бэкенд).

Проверка глазами обязательна: поставить на устройство, загрузить аву, посмотреть список групп и экран группы.

## Technical Details

### Коллекция `files`

```
_id        ObjectId
room_id    ObjectId   — чья картинка, по ней же проверяется доступ
owner_id   int        — кто загрузил
kind       string     — пока только "room_avatar"
mime       string     — image/jpeg | image/png | image/webp
size       int
data       Binary
created_at time.Time
```

Индекс по `room_id`. Потолок 5 МБ; сервер режет тело `http.MaxBytesReader` и проверяет тип **по сигнатуре файла**, а не по заголовку.

### Доступ

`userHasFile` сейчас перебирает все комнаты пользователя и ищет id среди файлов операций. Новый порядок:

1. id парсится как ObjectId и ищется в `files` → доступ = членство в `room_id` (одна выборка вместо перебора);
2. не ObjectId или не найден → прежний телеграмный путь без изменений.

### Ссылка на аву

`Room.AvatarFileId *string`, в DTO комнаты — `avatarFileId`. Есть — клиент грузит `/api/v1/files/{id}`, нет — рисует нынешний градиент. Клиент сжимает картинку до ~1024 px перед отправкой, чтобы в базу не летели десятки мегабайт с камеры.

Ава неизменяема: замена создаёт новый документ и удаляет старый, поэтому её можно кешировать надолго (`Cache-Control: private, max-age=...`) и не перекачивать при каждом скролле списка.

## What Goes Where

- **Implementation Steps** — хранилище, эндпоинты, клиенты, тесты
- **Post-Completion** — наблюдение за размером базы, выкатка

## Implementation Steps

### Task 1: Коллекция `files` и репозиторий

**Files:**
- Create: `internal/repository/files.go`, `internal/repository/files_test.go`
- Modify: `internal/api/service_models.go` (модель `StoredFile`)

- [x] модель `api.StoredFile` и репозиторий: `Save`, `Get`, `Delete`, `DeleteByRoom`
- [x] индекс по `room_id` (`EnsureIndexes`) — вызов из `main.go` в задаче 3, вместе с остальной проводкой
- [x] тесты на живой монге: байты возвращаются один в один, размер считает репозиторий, ненайденный файл — не ошибка, удаление комнаты не трогает чужие файлы

### Task 2: Отдача файла из монги, телеграм — запасной путь

**Files:**
- Modify: `internal/rest/handlers.go` (`handleGetFile`, `userHasFile`)
- Modify: `internal/rest/handlers_test.go`

- [x] `serveStoredFile` ищет id в `files`; доступ = членство в комнате файла (одна выборка вместо перебора всех комнат)
- [x] не найден — прежний телеграмный путь без изменений; хранилище не подключено (`SetFiles` не звали) — тоже
- [x] тот же allowlist инлайновых типов и `X-Content-Type-Options: nosniff`
- [x] `Cache-Control: private, max-age=30d, immutable` — ава неизменяема, id меняется вместе с картинкой
- [x] тесты (`files_test.go`): свой файл отдаётся байт в байт, чужой — 403, неизвестный id уходит в телеграмный путь

### Task 3: Загрузка и снятие авы

**Files:**
- Modify: `internal/rest/server.go` (маршруты), `internal/rest/handlers.go`
- Modify: `internal/api/service_models.go` (`Room.AvatarFileId`), `internal/rest/dto.go`
- Create: `internal/rest/room_avatar_test.go`

- [x] `PUT /api/v1/rooms/{roomId}/avatar` — multipart, право как у смены валюты (любой участник)
- [x] `DELETE /api/v1/rooms/{roomId}/avatar` — снять аву, байты удаляются; повтор безопасен
- [x] лимит 5 МБ, тип по сигнатуре (`http.DetectContentType`), замена удаляет прежний файл
- [x] не удалось поставить ссылку — только что загруженные байты убираются, сирот не остаётся
- [x] `avatarFileId` в DTO комнаты и в списке групп
- [x] ⚠️ «удаление комнаты удаляет её файлы» не подключено: комнаты в проекте не удаляются вообще (при удалении аккаунта участник анонимизируется). `DeleteByRoom` в репозитории есть и покрыт тестом — подключать некуда
- [x] тесты (`room_avatar_test.go`): загрузка, ссылка в списке групп, замена (старый файл исчез), снятие, чужак — 403, не картинка — 415, без хранилища — 503
- [x] проверено откатом: убрал проверку сигнатуры и удаление прежней авы — оба теста упали

### Task 4: iOS

**Files:**
- Modify: `ios/Splitty/Core/Models.swift`, `ios/Splitty/Core/APIClient.swift`
- Modify: `ios/Splitty/Features/Groups/GroupsListView.swift` (`GroupAvatarView`), `GroupSettingsView.swift`
- Create: тест в `ios/SplittyTests`

- [x] `GroupAvatarView` грузит фото по `avatarFileId`, иначе прежний градиент
- [x] в настройках группы — `PhotosPicker` и «Убрать»; сжатие переиспользует `ReceiptCapture` (1024 px / JPEG 0.7), второе такое же не заводил
- [x] кеш картинок по id файла в том же `AvatarStore` (отдельный словарь: id файла и id пользователя — разные пространства); `forgetFile` после замены, чтобы не остался старый кадр
- [x] сборка multipart вынесена из `parseOperation` в общий помощник и переиспользована — тест проверяет, что после этого части распознавания не потерялись
- [x] тесты (`RoomAvatarTests`): разбор с полем и без, PUT с частью `image`, DELETE, целостность multipart распознавания

### Task 5: Android

**Files:**
- Modify: `android/.../ui/groups/GroupsListScreen.kt`, `GroupSettingsScreen.kt`
- Modify: `android/.../data/**` (модели, api)
- Create: тесты

- [x] полный порт задачи 4: `GroupAvatar` с фото, секция в настройках группы, `PickVisualMedia` + `decodeDownscaledReceipt` (то же сжатие, что у чека)
- [x] кеш файлов в `AvatarStore` (`requestFile`/`forgetFile`), методы `setAvatar`/`removeAvatar` во вью-модели
- [x] строки в пяти локалях
- [x] ➕ починен найденный по пути баг: `GroupAvatar` отдавал хэш id комнаты как id пользователя в `GradientAvatar`, и тот грузил по нему фото из телеграма — при совпадении с реальным id группа показывала аватар постороннего. Добавлен `loadsPhoto`, iOS от этого был защищён, Android нет
- [x] тесты: разбор с полем и без, страж на `loadsPhoto = false` (проверен откатом — падает)

## Post-Completion

- [ ] следить за размером базы: фото живут в монге, бэкапов у неё сейчас нет
- [ ] выкатить бэкенд и собрать клиентов
