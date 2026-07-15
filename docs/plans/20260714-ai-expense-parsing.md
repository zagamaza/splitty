# AI-добавление расхода (голос + фото чека)

_Автор: AlmazNurmukhametov_

## Overview

Добавление расхода в splitty-app через AI: пользователь надиктовывает голосом («мы с Саней и Лёхой взяли пиццу за 1200, баурсаков 10 — Лёха 5, я 3, Маша 2, вино 3000 — с Маши 500, остальное поровну, плюс сервисный сбор 10%») или фотографирует чек. Сервер распознаёт ввод через Gemini Flash и возвращает заполненный черновик расхода. Целевой клиент v1 — **iOS**.

**Ключевой принцип:** AI **не создаёт операцию**, а заполняет существующую форму `AddExpenseView`. Пользователь всегда видит результат перед «Сохранить» и может поправить руками или голосом. Дальше идёт обычный путь `save() → clientOpId → outbox → noteDataChanged()`.

**Что решает:** убирает ручной ввод позиций для сложных чеков; распознаёт кто что ел и по каким долям; запоминает прозвища участников, чтобы не переспрашивать.

**Интеграция:** новый серверный эндпоинт `POST /api/v1/rooms/{roomId}/operations/parse` (stateless), новое аддитивное поле `Operation.Items`, расширение write-path (outbox/REST) для сохранения позиций, новый UI в iOS.

## Context (from discovery)

**Проект:** `/Users/almaznurmuhametov/projects/Архив/go/splitty-app`, ветка `feature/ios-app`. Telegram-бот «Split it» + REST API (stdlib `net/http`, Go 1.22) + нативные iOS (SwiftUI) и Android (Compose). MongoDB, операции — embedded-массив внутри документа комнаты. Деньги строго `int` (float в деньгах запрещён по `ios/docs/UX_SPEC.md`). DI через google/wire.

**Файлы/области:**
- Модель: `internal/api/service_models.go` (Operation, RecipientWithSum, SplitType), `internal/api/tg.go` (User), `internal/api/money.go` (`ShareOf` — каноничное деление).
- REST: `internal/rest/server.go` (роутер + `Server` struct с deps-полями:48; `maxBodyMiddleware` — лимит 1 МБ:174-182, оборачивает весь mux), `internal/rest/handlers.go` (`handleCreateOperation`:691, `validateOperationRequest`:515, `operationRequest`:481, `handleUpdateOperation`:778, `handleGetFile`:1479), `internal/rest/dto.go` (DTO/константы, `splitByExactAmount`:19), `internal/rest/auth.go` (JWT, `userIdFromCtx`), `internal/rest/fakes_test.go` (образец фейков).
- **DI REST — важно:** REST-сервер **не строится через wire**. Он собирается вручную в `cmd/splitty/main.go:initRestServer` (:91) вызовом `rest.NewServer(...)` (:119); зависимости — плоские поля `Server` struct. wire (`wire.go`/`wire_gen.go`) строит **только бота** (`initApp` → `*events.TelegramListener`). Новые зависимости (`ai.Parser`, rate-limit сервис) прокидываются через `Server` struct + сигнатуру `NewServer` + `initRestServer`, а НЕ через wire.
- Сервисы/репо: `internal/service/service.go` (`GetRoomDebts`, `settleBalances`), `internal/repository/repository.go` (коллекции room/user/chat_state/button/login_code/bug_report; `CreateOperationIfAbsent`).
- Бот: `internal/bot/operation_screen.go`, `internal/bot/bot.go` (Action-константы), `internal/bot/tablebuilder.go`, `internal/sdk/tablebox.go` (ASCII-таблицы).
- Конфиг: `cmd/splitty/config.go` (caarlos0/env), `cmd/splitty/wire.go` + `wire_gen.go`.
- iOS: `ios/Splitty/Features/Expense/AddExpenseView.swift` + `AddExpenseViewModel.swift`, `ios/Splitty/Core/APIClient.swift`, `ios/Splitty/Core/OutboxStore.swift`, `ios/Splitty/Core/Models.swift`, `ios/Splitty/Core/Theme.swift` + `DesignSystem.swift`, `ios/Splitty/Features/Groups/OperationDetailView.swift`, `ios/project.yml` (Info.plist генерится XcodeGen отсюда, iOS target 17.0).

**Паттерны:** handlers-экраны бота (`HasReact`/`OnMessage`); REST — `http.ServeMux` с Go 1.22 method patterns; idempotency по `clientOpId`; iOS — MVVM на `@Observable`+`@MainActor`, офлайн-outbox с `localId: UUID` как `clientOpId`. AI/LLM в проекте нет — зелёное поле.

**Зависимости:** MongoDB, Telegram Bot API v4, google/wire, golang-jwt/v5. Внешний: Google Gemini API (новый).

## Development Approach

- **testing approach:** TDD для расчётного ядра (деление позиций, округление, вывод плоских сумм из Items) — тесты пишутся вперёд, там легко разъехаться на рубль. Для остального (хендлеры, клиент Gemini, iOS) — regular (код, затем тесты).
- каждую задачу доводить до конца перед следующей; маленькие фокусные изменения
- **CRITICAL: каждая задача с изменением кода ОБЯЗАНА включать новые/обновлённые тесты** (success + error/edge), тесты — отдельные пункты чеклиста, не в связке с реализацией
- **CRITICAL: все тесты зелёные перед началом следующей задачи**
- **CRITICAL: обновлять этот план при изменении скоупа**
- обратная совместимость: `Items` аддитивно, старые клиенты (Android, бот, старые версии iOS) не шлют `items` и продолжают работать на плоских суммах
- проверка компиляции Go: `make build`; тесты: `make test`

## Testing Strategy

- **unit tests:** обязательны в каждой задаче. Расчётное ядро в `internal/api` покрывается table-driven тестами по образцу `internal/api/money_test.go` и `internal/service/*_test.go`. REST-хендлеры — по образцу `internal/rest/handlers_test.go` + `fakes_test.go`.
- **обязательные кейсы расчёта:** поровну; неравные веса (баурсаки 5/3/2); микс фикс+веса (вино: Маша 500, остальное поровну); полностью ручные суммы; неровное деление с остатком (остаток тому, у кого доля больше); сборы proportional и equally; перебор фиксов над ценой позиции; сумма долей == Sum до рубля.
- **e2e:** проект не имеет UI-e2e (Playwright/Cypress). iOS-логика ViewModel покрывается юнит-тестами по образцу `ios/SplittyTests/AddExpenseDistributionTests.swift`.
- **проверка сборки iOS:** на реальном устройстве (микрофон/камера в симуляторе не проверить полностью).

## Progress Tracking

- `[x]` сразу по завершении пункта
- ➕ — вновь обнаруженные задачи
- ⚠️ — блокеры
- держать план в синхроне с фактической работой

## Solution Overview

**Архитектура потока:**
1. iOS пишет аудио (`AVAudioRecorder`, m4a) или фото (`PhotosPicker`/камера, JPEG ≤1024px) + текущий черновик.
2. `POST /parse` (multipart): auth → членство в комнате → rate limit/квота (Mongo) → лимиты тела → загрузка каноничных участников (с алиасами) из коллекции `user` → вызов Gemini Flash с `responseSchema` → санитайз ответа → возврат `{draft, questions[]}`. **Stateless**: тот же вызов для распознавания с нуля и для голосовой правки (draft не пустой).
3. iOS показывает черновик в `AddExpenseView` (карточка-чек с позициями, сборами, долями). Правки: голосом (повторный `/parse`), тапом (шит позиции), или руками (сброс Items).
4. «Сохранить» → обычный `save()` с новым полем `items` в payload → outbox/REST → сервер **сам выводит** `RecipientsWithSum` из `Items` и не доверяет клиентским суммам.

**Ключевые решения:**
- `Operation.Items` — источник правды, `RecipientsWithSum` — всегда производная. Долги, уведомления, бот, Android работают на плоских суммах и про Items не знают.
- **Единое правило деления позиции:** снять фиксированные `Amount`, остаток разделить по `Weight`. Покрывает всё: поровну (Weight=1 у всех), неравные доли, микс, ручные суммы. Существующий `by_exact_amount` — частный случай.
- **Сборы (surcharge)** — не позиция: делятся пропорционально съеденному (default) или поровну (доставка).
- **Алиасы** глобальные в `User.Aliases`; нераспознанное имя → `DraftItem.Unknown` → чип в UI → выбор человека → дозапись алиаса.
- **Сервер — единственный источник доверия:** выводит суммы из Items, санитайзит ответ модели, затирает Items при плоском обновлении.

## Technical Details

**Модель (`internal/api/service_models.go`):**
```go
type ItemKind string   // "item" | "surcharge"
type SplitRule string  // "proportional" | "equally"

type ItemShare struct {
    UserId int  `bson:"user_id" json:"userId"`
    Weight int  `bson:"weight" json:"weight"`        // относительная доля; 1 = поровну
    Amount *int `bson:"amount,omitempty" json:"amount,omitempty"` // фикс-сумма; задана → Weight игнор
}
type OperationItem struct {
    Name    string      `bson:"name" json:"name"`
    Price   int         `bson:"price" json:"price"`   // ВСЕГДА total строки (не цена единицы)
    Qty     int         `bson:"qty" json:"qty"`       // только для отображения («×2»); в математике НЕ участвует
    Shares  []ItemShare `bson:"shares" json:"shares"`
    Kind    ItemKind    `bson:"kind" json:"kind"`
    Split   SplitRule   `bson:"split,omitempty" json:"split,omitempty"` // только surcharge
    Percent *int        `bson:"percent,omitempty" json:"percent,omitempty"` // только display («Сбор 10%»); НЕ пересчитывается
}
// Operation получает: Items []OperationItem `bson:"items,omitempty" json:"items,omitempty"`
```

**Семантика полей (зафиксировано до Task 2 — иначе формула неоднозначна):**
- `Price` — **всегда суммарная стоимость строки**, уже с учётом количества. `Qty` — чисто display («Баурсаки ×10»), в делёж НЕ входит. Никакого `UnitPrice*Qty` на сервере.
- `Percent` у surcharge — **только для показа** («Сервисный сбор 10%»). Сервер НЕ пересчитывает сбор из процента: сумма сбора всегда берётся из `Price`. Если AI дал только процент — он сам обязан посчитать `Price` (валидатор требует `Price>0` у surcharge). Так исключается расхождение «процент × база ≠ Price».
- **`Shares` у surcharge:** сбор делится не по своим shares, а по **base** (доли людей от обычных позиций). Поэтому `Kind==surcharge` → поле `Shares` **не используется и должно быть пустым/nil**; валидатор его игнорирует. Единственное, что у сбора значимо: `Price` и `Split` (proportional|equally).
- **Копейки/дробные суммы:** все деньги — целые единицы валюты (`int`). При распознавании **фото чека** цены с копейками округляются до целого **на этапе `sanitizeDraft`** (Task 5): каждая позиция округляется, затем копеечный остаток между `Σ позиций` и распознанным итогом добивается в крупнейшую позицию (детерминированно), чтобы `DeriveShares` сошёлся. Для голоса неактуально (называют круглыми). Задать явными кейсами в Task 5.

**Формула деления позиции (`internal/api/itemsplit.go` — чистые функции, TDD):**
1. Сумма фиксов `F = Σ Amount`. Если `F > Price` → перебор (ошибка валидации).
2. Остаток `R = Price - F` делится по весам между участниками без `Amount`.
3. Целочисленно: `base = R * weight / totalWeight`, остаток от деления раздаётся по одному тем, у кого доля больше (детерминированный tie-break по UserId).
4. Сборы: после расчёта обычных позиций считаем базовые доли людей; `proportional` → веса = базовые доли, `equally` → веса равные; тот же целочисленный сплит.
5. Итог сворачивается в `map[int]int` (userId→сумма) + total.

**Сигнатуры ядра (важно — ядро НЕ знает про `User`):**
```go
func SplitItem(price int, shares []ItemShare) (map[int]int, error)
func SplitSurcharge(price int, rule SplitRule, base map[int]int) map[int]int
func DeriveShares(items []OperationItem) (shares map[int]int, total int, err error) // userId→сумма
```
Ядро работает только с `userId` (int). Инвариант `Σ shares == total` проверяется на **int** внутри ядра.

**Маппинг в модель (в REST write-path, НЕ в ядре):** `RecipientWithSum` хранит embedded `User` целиком (`service_models.go:50`), а долги читают `recipient.User.ID`. Поэтому хендлер, получив `map[int]int` от `DeriveShares`, маппит каждый `userId` в `User` из `room.Members` и собирает `[]RecipientWithSum` (`Sum` — `float64`, конверсия из int только здесь). Если `userId` из ядра не найден в `room.Members` → 400 (защита от рассинхрона).

**Черновик (DTO parse-эндпоинта, `internal/rest/dto.go`):**
```go
type parseDraft struct {
    Description string          `json:"description"`
    Sum        int             `json:"sum"`
    DonorId    *int            `json:"donorId"`
    Items      []draftItem     `json:"items"`
}
type draftItem struct { /* Name, Price, Qty, Shares, Kind, Split, Percent, Unknown []string */ }
type parseResponse struct {
    Draft     parseDraft `json:"draft"`
    Questions []string   `json:"questions"` // «не расслышал сумму», «кто платил?»
}
```

**Лимиты `/parse`:** общий предел тела ~15 МБ (отдельно от глобального 1 МБ), покусочно: audio ≤3 МБ, image ≤8 МБ, draft ≤64 КБ. Content-Type allowlist. Отсечка rate-limit/квоты **до** чтения больших частей и до вызова Gemini.

**Rate limit (Mongo):** новая коллекция `ai_usage` (или счётчик в `user`): запросов/мин и суточная квота на пользователя. Инкремент атомарным `$inc` с TTL/сбросом по дню.

**Config (`cmd/splitty/config.go`):** `GEMINI_API_KEY`, `GEMINI_MODEL` (default flash), `AI_PARSE_RATE_PER_MIN`, `AI_PARSE_DAILY_QUOTA`, `AI_MAX_BODY_BYTES`.

## What Goes Where

- **Implementation Steps** (`[ ]`): модель, расчётное ядро, Gemini-клиент, parse-эндпоинт с защитами, write-path (сервер+outbox+REST), затирание Items на старых путях, бот-guard, миграция алиасов, iOS UI и запись медиа, permissions.
- **Post-Completion** (без чекбоксов): ручное тестирование на устройстве, релиз в App Store с новыми permissions, обновление Android (downgrade itemized), проверка биллинга Gemini, обновление `docs/API.md`.

## Implementation Steps

### Task 1: Доменная модель Items/ItemShare

**Files:**
- Modify: `internal/api/service_models.go`

- [x] добавить типы `ItemKind`, `SplitRule`, `ItemShare`, `OperationItem` с bson/json-тегами
- [x] добавить поле `Items []OperationItem` в `Operation` (omitempty, nil для обычных операций)
- [x] добавить константы `ItemKindItem/ItemKindSurcharge`, `SplitProportional/SplitEqually`
- [x] убедиться, что существующая (де)сериализация Operation не ломается (Items опускается) — прогнать существующие тесты `internal/service`, `internal/rest`
- [x] run `make build` + `make test` — должно пройти (go1.23.5 локально; go1.22 toolchain недоступен без сети)

### Task 2: Расчётное ядро деления позиции (TDD)

**Files:**
- Create: `internal/api/itemsplit.go`
- Create: `internal/api/itemsplit_test.go`

- [x] **тесты вперёд:** table-driven кейсы — поровну; веса 5/3/2; микс (Маша 500 + остальное поровну); все ручные; неровный остаток (тому, у кого доля больше); перебор фиксов; одиночный участник; нулевые/пустые shares; **`Qty>1` (Price=total, деление НЕ зависит от Qty)**; **surcharge с Percent (сумма берётся из Price, процент игнорируется в расчёте)**
- [x] реализовать `SplitItem(price int, shares []ItemShare) (map[int]int, error)`: снять фиксы → остаток по весам → целочисленно с детерминированным tie-break по UserId
- [x] реализовать `SplitSurcharge(price int, rule SplitRule, base map[int]int) map[int]int`
- [x] реализовать `DeriveShares(items []OperationItem) (map[int]int, int, error)` — свернуть позиции+сборы в `userId→сумма` + total; **ядро возвращает int-карту, про `User` НЕ знает** (маппинг в `RecipientWithSum` — в Task 7, в REST); инвариант `Σ == total` на int
- [x] тесты на `DeriveShares`: полный чек из Overview (пицца 1200 + баурсаки 500 веса 5/3/2 + вино 3000 микс + сбор 10% proportional), проверка каждой доли и суммы до рубля
- [x] run tests — должны пройти перед задачей 3

### Task 3: Клиент Gemini за интерфейсом

**Files:**
- Create: `internal/ai/ai.go` (интерфейс `Parser`)
- Create: `internal/ai/gemini.go`
- Create: `internal/ai/gemini_test.go`
- Modify: `cmd/splitty/config.go`

- [x] определить интерфейс `Parser.Parse(ctx, input ParseInput) (ParseResult, error)` — input: audio/image/text bytes + mime + участники (id, displayName, username, aliases) + валюта + текущий draft. **Единый транспортный черновик `ai.Draft`/`ai.DraftItem`/`ai.ItemShare` (с json+bson тегами) — переиспользуется REST-слоем вместо отдельного `parseDraft`, чтобы не троить структуру api/ai/rest**
- [x] реализовать Gemini-клиент: **`generateContent` REST, `Content-Type: application/json`**; медиа передаётся как `inline_data{ mime_type, data(base64) }` внутри JSON-body (НЕ HTTP-multipart); `responseMimeType=application/json` + `responseSchema`; stdlib `net/http`, `temperature=0`, таймаут по контексту; `httpDoer`-seam для тестов
- [x] один ретрай при невалидном/непарсящемся JSON ответа (HTTP-ошибки НЕ ретраятся)
- [x] промпт: правила матчинга имён (displayName/username/aliases; неоднозначность → в Unknown), правила surcharge (процент→proportional, фикс→equally; сумму сбора модель обязана дать в `Price`), правило `Price`=total строки, формат долей
- [x] **контракт голосовой правки:** при непустом входном `draft` в промпте явно: «черновик — ИСТИНА, применяй только дельту, не пересобирай, не трогай уже проставленные доли и разрешённые имена». (Юнит на «доли не меняются» не пишу — это поведение модели, не кода; проверяется в acceptance Task 14)
- [x] добавить ENV в config: `GEMINI_API_KEY`, `GEMINI_MODEL`, `AI_PARSE_RATE_PER_MIN`, `AI_PARSE_DAILY_QUOTA`, `AI_MAX_BODY_BYTES`
- [x] тесты с фейковым HTTP-транспортом: успех, невалидный JSON+ретрай, две невалидных→ошибка, HTTP 500 без ретрая, пустой ключ, audio inline_data base64
- [x] run tests — должны пройти

### Task 4: Rate limit и суточная квота (Mongo)

**Files:**
- Create: `internal/repository/ai_usage.go` (или расширить `repository.go`)
- Create: `internal/repository/ai_usage_test.go`
- Modify: `internal/service/service.go`
- Modify: `cmd/splitty/main.go` (вызов создания TTL-индекса при старте)

- [x] коллекция/счётчик `ai_usage`: атомарный `$inc` окна (userId+минута / userId+сутки) через `Incr(key, ttl)`, `expires_at` через `$setOnInsert`
- [x] метод `AllowParse(ctx, userId) (bool, reason, err)` в `service.RateLimiter` (минутное окно проверяется первым); окна сбрасываются сменой ключа по времени (`now` подменяется в тестах)
- [x] **`EnsureIndexes(ctx)` — TTL-индекс `expireAfterSeconds=0` на `expires_at`** (создан метод репо; вызов из старта подключается в Task 6 при правке `initRestServer`)
- [x] сервис-обёртка `RateLimiter` над узким интерфейсом `UsageCounter` (тестируется фейком, без реального Mongo)
- [x] тесты: в пределах лимита, превышение per-min, превышение daily, сброс минутного окна, ошибка счётчика
- [x] run tests — должны пройти

> Примечание: конструирование rate-limit сервиса и его прокидывание в REST делается в Task 6 через `initRestServer`/`NewServer` (не wire) — здесь только репозиторий+сервис+индекс.

### Task 5: Parse-DTO и санитайз ответа модели (TDD)

**Files:**
- Create: `internal/rest/parse_sanitize.go`
- Create: `internal/rest/parse_sanitize_test.go`

> Транспортный черновик — переиспользованный `ai.Draft`/`ai.DraftItem`/`ai.ItemShare` (у `DraftItem` уже есть `Unknown []string`); отдельный `parseDraft` в `dto.go` НЕ заводился, чтобы не троить структуру. `sanitizeDraft` работает над `ai.Draft`.

- [x] транспортный черновик — `ai.Draft` (создан в Task 3); отдельный `parseDraft` не нужен
- [x] **тесты вперёд:** userId не из комнаты → доля убрана; отрицательная/нулевая цена → дроп; >N позиций (лимит); Sum≠Σ → пересчёт из позиций; сборы без Split → default; surcharge с нулевой Price → дроп; чужой donorId → nil; позиция с Unknown сохраняется
- [x] реализовать `sanitizeDraft(d ai.Draft, members []api.User) ai.Draft` + хелпер `hasUnknown` (для блокировки сохранения в Task 7/13)
- [x] run tests — должны пройти

### Task 6: Parse-эндпоинт с защитами

**Files:**
- Modify: `internal/rest/server.go` (`Server` struct + `maxBodyMiddleware` + маршрут)
- Modify: `internal/rest/handlers.go` (`handleParseOperation`)
- Modify: `cmd/splitty/main.go` (`initRestServer` — конструирование `ai.Parser` + rate-limit сервиса)
- Modify: `internal/repository/repository.go` (`FindByIds` — батч, его нет)
- Create: `internal/rest/parse_handler_test.go`

> parse-DTO (`parseDraft`/`draftItem`/`parseResponse`) уже созданы в Task 5.

- [x] **DI (не wire!):** `ai.Parser` + `*service.RateLimiter` + `aiMaxBody` полями в `Server`; setter `SetAI(...)` (как `SetNotifier` — не ломает существующие вызовы `NewServer` в тестах); конструирование в `cmd/splitty/main.go:initRestServer` под условием `GEMINI_API_KEY != ""` + вызов `EnsureIndexes`
- [x] **снятие лимита 1 МБ в middleware:** `maxBodyMiddleware` пропускает `MaxBytesReader` для путей на `/operations/parse`; больший лимит ставится в хендлере
- [x] маршрут `POST /api/v1/rooms/{roomId}/operations/parse` под `s.auth` (зарегистрирован ДО `/operations`, чтобы не перехватывался)
- [x] порядок в хендлере: 503 если AI выключен → членство → rate-limit → `MaxBytesReader(aiMaxBody)` → multipart с покусочными лимитами (audio 3МБ/image 8МБ/draft 64КБ) + Content-Type allowlist
- [x] `FindByIds(ctx, ids []int)` в `UserRepository` — каноничные участники из коллекции `user` (с алиасами); `buildParticipants` с откатом к members при сбое
- [x] вызов `ai.Parser.Parse` → `sanitizeDraft` → `ai.ParseResult` (хендлер в отдельном `parse_handler.go`, не в handlers.go)
- [x] при ошибке AI: 502 + эхо входного draft (не теряется) + логирование
- [x] тесты: 401; 403 не участник; 503 AI выключен; 429 при лимите (parser не вызван); 415 неподдерживаемый mime; happy path (текст+аудио); ошибка AI сохраняет draft; 400 без ввода
- [x] run tests — должны пройти

### Task 7: Write-path — сохранение Items (сервер выводит суммы) + read-path

**Files:**
- Modify: `internal/rest/handlers.go` (`operationRequest`:481, `handleCreateOperation`:691, `validateOperationRequest`:515 — всё в handlers.go, НЕ в dto.go)
- Modify: `internal/rest/dto.go` (`operationDto`:56 + `toOperationDto`:218 — **read-path**)
- Modify: `internal/rest/handlers_test.go`

- [x] добавить `items []ai.DraftItem` в `operationRequest` (опционально, аддитивно)
- [x] **конвертация `ai.DraftItem` → `api.OperationItem`** (`toApiItems` в `parse_convert.go`); обратный `api.OperationItem → operationItemDto` для read-path
- [x] при наличии `items`: `validateItemizedRequest` вызывает `api.DeriveShares` → маппит `userId→User` через `findMember` → сервер формирует `RecipientsWithSum`/`Sum`, игнорируя клиентские плоские поля; `SplitType = by_exact_amount`; `Items` сохранены
- [x] валидация items: непусто, все userId из комнаты, `DeriveShares` без ошибки, **`Unknown` пуст** (иначе 400 «сначала выберите, кто такие …»)
- [x] при отсутствии `items`: прежнее поведение (`validateOperationRequest`), `Items = nil`
- [x] **read-path:** `items` в `operationDto` + заполнение в `toOperationDto` → `GET /rooms`, `/operations`, activity отдают позиции
- [x] тесты: сервер выводит суммы (клиентский sum=999 проигнорён → 300); полный чек с инвариантом; обычная операция без items; непустой Unknown → 400; чужой userId → 400; read-path возвращает items
- [x] run tests — должны пройти

### Task 8: Затирание Items на плоских путях правки (сервер)

**Files:**
- Modify: `internal/rest/handlers.go` (`handleUpdateOperation`:778)
- Modify: `internal/rest/handlers_test.go`

- [x] `handleUpdateOperation`: ветвление как в create — запрос **без** `items` принудительно ставит `Items = nil`; удалён мёртвый `parseOperationRequest`
- [x] запрос **с** `items` — `validateItemizedRequest` пересчитывает `RecipientsWithSum`/`Sum` из Items
- [x] тест: itemized-операция → PUT без items → Items очищены (в ответе и хранилище), плоские суммы сохранены; PUT с items (веса 2:1) → Items обновлены, суммы 200/100
- [x] run tests — должны пройти

### Task 9: Бот — показ позиций и guard на правку

**Files:**
- Modify: `internal/bot/operation_screen.go`
- Modify: `internal/bot/bot.go` (при необходимости — Action/строки)
- Modify: `conf/lang/ru.ini`, `conf/lang/en.ini`
- Create: `internal/bot/operation_items_test.go`

- [ ] при показе операции с `Items != nil` — рендер позиций текстом через `tablebuilder`/`tablebox` (позиция, цена, кто, сбор, итого)
- [ ] на «изменить» itemized-операцию — вместо редактора показать сообщение «эта операция с позициями, правьте в приложении», правку не запускать
- [ ] строки i18n (ru/en) для guard-сообщения
- [ ] тест: рендер itemized-операции в текст; guard блокирует вход в редактирование (юнит на функцию-детектор `Items != nil`)
- [ ] run tests — должны пройти

### Task 10: User.Aliases — модель и дозапись

**Files:**
- Modify: `internal/api/tg.go` (User.Aliases)
- Modify: `internal/repository/repository.go` (метод `AddUserAlias`)
- Modify: `internal/rest/handlers.go` (эндпоинт дозаписи алиаса)
- Modify: `internal/rest/server.go` (**регистрация маршрута** — routes прописаны вручную в `Handler`, :125; без этого хендлер недостижим)
- Modify: `internal/rest/dto.go`
- Modify: `internal/repository/repository_test.go` или `internal/rest/handlers_test.go`

- [x] добавить `Aliases []string` в `User` (сделано в Task 6)
- [x] `AddAlias(userId, alias)` — `$addToSet`, дедуп; нормализация (trim/lower) в хендлере
- [x] `POST /api/v1/users/{userId}/aliases` под auth; **маршрут зарегистрирован в `server.go`**; **область записи:** только при общей комнате (`shareRoom`), в свой профиль всегда; 204 No Content
- [x] parse-хендлер (Task 6) читает aliases из каноничных user (`buildParticipants` через `FindByIds`) — связка есть
- [x] тесты: 401 без токена, 403 без общей комнаты, 204 happy path + нормализация («  Саня  » → «саня»), идемпотентность, пустой → 400
- [x] run tests — должны пройти

### Task 11: iOS — модель черновика и API-клиент

**Files:**
- Modify: `ios/Splitty/Core/Models.swift` (`OperationItem`, `ItemShare`, `Draft` + **`items` в read-модели `Operation`** — иначе detail не покажет позиции)
- Modify: `ios/Splitty/Core/APIClient.swift` (parse-запрос; `items` в `OperationBody`:96 + сигнатуры `createOperation`:290 / `updateOperation`:310; **seam для тестируемости**)
- Modify: `ios/Splitty/Core/OutboxStore.swift` (`items` в `OutboxPayload`:9 + маппинг в `send`:213)
- Create: `ios/SplittyTests/ItemDraftTests.swift`

- [ ] модели `OperationItem`, `ItemShare`, `ParseDraft`, `ParseResponse` (Codable); **добавить `items` в read-модель `Operation`** (для OperationDetailView в Task 13)
- [ ] `APIClient.parseOperation(roomId, audio?/image?/text?, draft)` — первый upload в проекте: **JSON-body или multipart к НАШЕМУ серверу** (не к Gemini напрямую); сервер уже принимает multipart (Task 6)
- [ ] **вся цепочка write-path (иначе Items молча потеряются при офлайн-сохранении — критично по Codex):** добавить `items` в `OperationBody` struct (:96); протащить `items` параметром в `APIClient.createOperation` (:290) и `updateOperation` (:310) и вложить в конструктор `OperationBody`; добавить `items` в `OutboxPayload` (:9); замапить `payload.items` в `OutboxStore.send` (:213) при вызове `api.createOperation`/`updateOperation`
- [ ] **seam для теста (находка Codex — сейчас `APIClient` final с private `URLSession.shared`, fake не подставить):** ввести узкий протокол create/update/delete (или инъекцию `URLSession`/transport в `APIClient`), чтобы `OutboxStore.sync` можно было прогнать с фейком и проверить отправленный payload
- [ ] **тест офлайн-раундтрипа outbox:** `items` переживают enqueue → flush → вызов API (через seam из предыдущего пункта)
- [ ] **загрузка `items` при правке (критично — иначе правка itemized-операции стирает позиции):** в `AddExpenseViewModel.load(editOperation:)` (`AddExpenseViewModel.swift:179`) сейчас читаются только `recipients`/`splitType`/`amountTexts`. Добавить чтение `editOperation.items` в форму, чтобы при редактировании чек не превращался в плоскую операцию (иначе PUT уйдёт без items → сервер затрёт Items в Task 8)
- [ ] тесты ViewModel-логики черновика: сериализация items, вывод per-person сумм (клиентское превью), сброс items при ручной правке, **загрузка editOperation.items в форму (round-trip: load → save → items не потеряны)**
- [ ] сборка iOS-таргета проходит

### Task 12: iOS — запись аудио, фото, permissions

**Files:**
- Modify: `ios/project.yml` (NSMicrophoneUsageDescription, NSCameraUsageDescription, NSPhotoLibraryUsageDescription)
- Create: `ios/Splitty/Features/Expense/AudioRecorder.swift`
- Create: `ios/Splitty/Features/Expense/ReceiptCapture.swift`

- [ ] usage-strings в `project.yml` (по-русски), регенерация проекта XcodeGen
- [ ] `AudioRecorder` на `AVAudioRecorder` (AAC ~16kbps), hold-to-talk. **MIME: слать `audio/aac` (Gemini поддерживает aac; `.m4a`/`audio/mp4` НЕ в списке).** Зафиксировать формат контейнера и явно проставлять поддерживаемый mime при upload; server allowlist принимает только `audio/aac`, `audio/mp3`, `audio/wav`, `audio/ogg`, `audio/flac`
- [ ] `ReceiptCapture`: `PhotosPicker`/камера → JPEG (`image/jpeg`) сжатие до ~1024px
- [ ] `.disabled` состояние при `!session.isOnline` (прецедент — погашение долга)
- [ ] проверка на устройстве, что permission-запрос не крашит (ручная — см. Post-Completion)

### Task 13: iOS — интеграция в AddExpenseView (композер, чек, шит позиции)

**Files:**
- Modify: `ios/Splitty/Features/Expense/AddExpenseView.swift`
- Modify: `ios/Splitty/Features/Expense/AddExpenseViewModel.swift`
- Modify: `ios/Splitty/Features/Groups/OperationDetailView.swift` (показ позиций в детали)
- Create: `ios/SplittyTests/AddExpenseAIFlowTests.swift`

- [ ] композер (крупный микрофон + «Сфотографировать чек») — только на пустой форме; после заполнения микрофон переезжает в нижнюю панель рядом с «Сохранить»
- [ ] **правка существующей itemized-операции:** `OperationDetailView` → «Изменить» открывает `AddExpenseView(editOperation:)` с уже загруженными `items` (Task 11); чек показывается в редакторе, сохранение уходит PUT **с** items (не плоским путём). Проверить, что редактирование чека не сбрасывает позиции
- [ ] карточка-чек: перфорация, пунктирные разделители, моноширинные цифры, подвал Подытог→Сборы→Итого; бейдж ×N (неравные доли), замочек (фикс-сумма)
- [ ] шит позиции: переключатель «Долями / Суммами» (один контрол на строку), галочка участия по тапу на имя, пустое поле суммы = «авто»
- [ ] чипы «Поровну на всех / По позициям» под позициями (переопределение; сбрасывает Items); ручная правка суммы сбрасывает Items
- [ ] голосовая правка: повторный `/parse` с текущим draft; вызов parse из ViewModel, спиннер, ошибка не теряет draft
- [ ] нераспознанное имя (Unknown) — красный чип, тап → выбор участника → `POST aliases` + локальное применение
- [ ] **`canSave` = false, пока в любой позиции есть непустой `Unknown`** (нельзя сохранить черновик с нераспознанными именами; сервер тоже вернёт 400 — Task 7); подсказка «выберите, кто такой …»
- [ ] тесты ViewModel: заполнение из parseResponse, сброс Items при ручной правке, применение выбора для Unknown, `canSave=false` при непустом Unknown, `canSave` при несходящихся суммах
- [ ] сборка iOS проходит; только семантические токены Theme.swift, тёмная тема, только стандартный SDK

### Task 14: Verify acceptance criteria

- [ ] полный сценарий из Overview проходит: голос → черновик с пиццей/баурсаками/вином/сбором → суммы сходятся
- [ ] голосовая правка («пиво Лёха не пил») пересчитывает и сбор
- [ ] нераспознанный «Саня» → чип → выбор → алиас сохранён → повторный parse матчит сам
- [ ] сохранение itemized-операции: сервер вывел суммы, долги считаются корректно (`GetRoomDebts`)
- [ ] правка itemized-операции старым плоским путём (бот/Android) затирает Items (нет разъезда)
- [ ] правка itemized-операции **в iOS** сохраняет позиции (items загружены в редактор, PUT с items) — не превращает чек в плоскую операцию
- [ ] голосовая правка поверх ручных изменений не сбрасывает уже проставленные доли и разрешённые имена
- [ ] бот показывает позиции и блокирует правку
- [ ] защиты `/parse`: 401/403/429/413 срабатывают; фото >1 МБ проходит, а глобальный JSON-лимит цел
- [ ] run `make build` && `make test` — всё зелёное
- [ ] прогнать iOS-тесты в Xcode

### Task 15: [Final] Update documentation

- [ ] обновить `docs/API.md`: эндпоинт `/parse`, поле `items` в create/update, `/users/{id}/aliases`
- [ ] обновить `ios/docs/UX_SPEC.md`: AI-композер, чек-карточка, шит позиции
- [ ] обновить `CLAUDE.md`/`AGENTS.md` если появились новые паттерны (пакет `internal/ai`, rate-limit)
- [ ] переместить план в `docs/plans/completed/`

## Post-Completion

_Требуют ручного вмешательства или внешних систем — без чекбоксов._

**Ручная проверка:**
- запись голоса и съёмка чека на реальном iPhone (микрофон/камера в симуляторе не проверить); permission-запросы не крашат
- качество распознавания Gemini на реальных чеках (русский, разные форматы), калибровка промпта
- поведение при плохой сети во время parse (таймаут, ретрай, сохранность черновика)

**Внешние системы:**
- Android: добавить downgrade-логику (показ позиций / блок itemized-правки), чтобы не разъезжать с сервером — отдельная задача в Android-репо
- App Store: релиз с новыми usage-permissions пройдёт review (описать назначение микрофона/камеры)
- Gemini: проверить лимиты бесплатного тира под реальную нагрузку компании; настроить биллинг-алерт на случай выхода за free-tier

**Получение и настройка `GEMINI_API_KEY`:**
1. [aistudio.google.com](https://aistudio.google.com) → «Get API key» → «Create API key». Правильный ключ Gemini начинается с **`AIza`** (~39 символов). Строки вида `AQ.…` — это НЕ ключ Gemini (другой сервис/OAuth), `generateContent` их отвергнет.
2. Free-tier не требует карты. Лимиты (~10–15 rpm, сотни–тысячи в день) — **на весь ключ, не на пользователя**; на бесплатном тире запросы идут в обучение Google. Переход на платный тир отключает data-sharing без изменения кода.
3. Класть **только в ENV** окружения бота (`GEMINI_API_KEY`), либо в `.env` (убедиться, что он в `.gitignore`). **Никогда не коммитить ключ, не хранить в коде/плане, не пересылать в переписке.** Если ключ где-то засветился — отозвать в AI Studio и выпустить новый.
4. Наш rate-limit (`AI_PARSE_RATE_PER_MIN`, `AI_PARSE_DAILY_QUOTA`, Task 4) настроить **консервативнее** гугловского общего лимита, чтобы один пользователь не исчерпал квоту для всех. Ориентир для компании друзей: ~3–5 rpm и ~30–50/сутки на человека.
5. Удобно завести **два ключа** — dev и prod, чтобы тесты не съедали дневной лимит продакшена.
