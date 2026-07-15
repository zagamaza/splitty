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

- [ ] добавить типы `ItemKind`, `SplitRule`, `ItemShare`, `OperationItem` с bson/json-тегами
- [ ] добавить поле `Items []OperationItem` в `Operation` (omitempty, nil для обычных операций)
- [ ] добавить константы `ItemKindItem/ItemKindSurcharge`, `SplitProportional/SplitEqually`
- [ ] убедиться, что существующая (де)сериализация Operation не ломается (Items опускается) — прогнать существующие тесты `internal/service`, `internal/rest`
- [ ] run `make build` + `make test` — должно пройти

### Task 2: Расчётное ядро деления позиции (TDD)

**Files:**
- Create: `internal/api/itemsplit.go`
- Create: `internal/api/itemsplit_test.go`

- [ ] **тесты вперёд:** table-driven кейсы — поровну; веса 5/3/2; микс (Маша 500 + остальное поровну); все ручные; неровный остаток (тому, у кого доля больше); перебор фиксов; одиночный участник; нулевые/пустые shares; **`Qty>1` (Price=total, деление НЕ зависит от Qty)**; **surcharge с Percent (сумма берётся из Price, процент игнорируется в расчёте)**
- [ ] реализовать `SplitItem(price int, shares []ItemShare) (map[int]int, error)`: снять фиксы → остаток по весам → целочисленно с детерминированным tie-break по UserId
- [ ] реализовать `SplitSurcharge(price int, rule SplitRule, base map[int]int) map[int]int`
- [ ] реализовать `DeriveShares(items []OperationItem) (map[int]int, int, error)` — свернуть позиции+сборы в `userId→сумма` + total; **ядро возвращает int-карту, про `User` НЕ знает** (маппинг в `RecipientWithSum` — в Task 7, в REST); инвариант `Σ == total` на int
- [ ] тесты на `DeriveShares`: полный чек из Overview (пицца 1200 + баурсаки 500 веса 5/3/2 + вино 3000 микс + сбор 10% proportional), проверка каждой доли и суммы до рубля
- [ ] run tests — должны пройти перед задачей 3

### Task 3: Клиент Gemini за интерфейсом

**Files:**
- Create: `internal/ai/ai.go` (интерфейс `Parser`)
- Create: `internal/ai/gemini.go`
- Create: `internal/ai/gemini_test.go`
- Modify: `cmd/splitty/config.go`

- [ ] определить интерфейс `Parser.Parse(ctx, input ParseInput) (ParseResult, error)` — input: audio/image/text bytes + mime + участники (id, displayName, username, aliases) + валюта + текущий draft
- [ ] реализовать Gemini-клиент: **`generateContent` REST, `Content-Type: application/json`**; медиа передаётся как `inline_data{ mime_type, data(base64) }` внутри JSON-body (это НЕ HTTP-multipart!); `generationConfig.responseMimeType=application/json` + `responseSchema` (JSON Schema черновика); stdlib `net/http`, таймаут по контексту. Учесть, что base64 раздувает тело ~на 33% — держаться под лимитом запроса Gemini
- [ ] один ретрай при невалидном/непарсящемся JSON ответа
- [ ] промпт: правила матчинга имён (displayName/username/aliases; неоднозначность → в Unknown), правила surcharge (процент→proportional, фикс→equally; сумму сбора модель обязана дать в `Price`), правило `Price`=total строки, формат долей
- [ ] добавить ENV в config: `GEMINI_API_KEY`, `GEMINI_MODEL`, `AI_PARSE_RATE_PER_MIN`, `AI_PARSE_DAILY_QUOTA`, `AI_MAX_BODY_BYTES`
- [ ] тесты с фейковым HTTP-транспортом: успех, невалидный JSON+ретрай, таймаут, пустой ответ
- [ ] run tests — должны пройти

### Task 4: Rate limit и суточная квота (Mongo)

**Files:**
- Create: `internal/repository/ai_usage.go` (или расширить `repository.go`)
- Create: `internal/repository/ai_usage_test.go`
- Modify: `internal/service/service.go`
- Modify: `cmd/splitty/main.go` (вызов создания TTL-индекса при старте)

- [ ] коллекция/счётчик `ai_usage`: атомарный `$inc` окна «запросов/мин» и «в сутки» на userId, поле `expires_at`
- [ ] метод `AllowParse(userId) (bool, reason)` + сброс окна по времени
- [ ] **`EnsureAIUsageIndexes(ctx)` — TTL-индекс `expireAfterSeconds` на `expires_at`** (в репозитории сейчас нет bootstrap-инфраструктуры индексов; без явного создания коллекция растёт вечно); вызвать из `initRestServer`/старта
- [ ] сервис-обёртка над репозиторием
- [ ] тесты: в пределах лимита, превышение per-min, превышение daily, сброс окна
- [ ] run tests — должны пройти

> Примечание: конструирование rate-limit сервиса и его прокидывание в REST делается в Task 6 через `initRestServer`/`NewServer` (не wire) — здесь только репозиторий+сервис+индекс.

### Task 5: Parse-DTO и санитайз ответа модели (TDD)

**Files:**
- Modify: `internal/rest/dto.go` (`parseDraft`, `draftItem` c полем `Unknown []string`, `parseResponse`)
- Create: `internal/rest/parse_sanitize.go`
- Create: `internal/rest/parse_sanitize_test.go`

> DTO черновика создаётся ЗДЕСЬ (не в Task 6), иначе `sanitizeDraft` не скомпилируется — он использует эти типы.

- [ ] завести `parseDraft`, `draftItem` (с `Unknown []string`), `parseResponse` в `dto.go`
- [ ] **тесты вперёд:** userId не из комнаты → выкинуть/в Unknown; отрицательная/нулевая цена; >N позиций (лимит); >M shares; Sum≠Σ позиций → пересчитать Sum из позиций; сборы без Split → default; surcharge с Percent но без Price → отклонить
- [ ] реализовать `sanitizeDraft(draft, members) parseDraft`: userId только из участников, цены ≥0, лимиты числа позиций/долей, нормализация Kind/Split
- [ ] run tests — должны пройти

### Task 6: Parse-эндпоинт с защитами

**Files:**
- Modify: `internal/rest/server.go` (`Server` struct + `maxBodyMiddleware` + маршрут)
- Modify: `internal/rest/handlers.go` (`handleParseOperation`)
- Modify: `cmd/splitty/main.go` (`initRestServer` — конструирование `ai.Parser` + rate-limit сервиса)
- Modify: `internal/repository/repository.go` (`FindByIds` — батч, его нет)
- Create: `internal/rest/parse_handler_test.go`

> parse-DTO (`parseDraft`/`draftItem`/`parseResponse`) уже созданы в Task 5.

- [ ] **DI (не wire!):** добавить `ai.Parser` и rate-limit сервис полями в `Server` struct (`server.go:48`), расширить сигнатуру `rest.NewServer(...)`, сконструировать обе зависимости в `cmd/splitty/main.go:initRestServer` (:91) и передать в `NewServer` (:119)
- [ ] **снятие лимита 1 МБ в middleware, не в хендлере:** в `maxBodyMiddleware` (`server.go:174`) пропускать `MaxBytesReader`, если `r.URL.Path` оканчивается на `/operations/parse` (re-wrap внутри хендлера НЕ снимет внешний 1 МБ-ридер)
- [ ] маршрут `POST /api/v1/rooms/{roomId}/operations/parse` под `s.auth`
- [ ] порядок в хендлере: auth → членство в комнате (roomForMember) → rate limit/квота → `MaxBytesReader` (~15 МБ) → streaming multipart с покусочными лимитами (audio/image/draft) + Content-Type allowlist
- [ ] `FindByIds(ctx, ids []int)` в `UserRepository` (сейчас есть только `FindById`:451) — загрузка **каноничных** участников из коллекции `user` по member ids (не embedded-снимки) для алиасов; либо явный цикл `FindById`
- [ ] вызов `ai.Parser.Parse` → `sanitizeDraft` → `parseResponse`
- [ ] при ошибке AI: понятная ошибка, входной draft не теряется (эхо обратно), логирование
- [ ] тесты: 401 без токена; 403 не участник; 429 при превышении квоты; 413 при превышении размера; happy path с фейковым Parser; ошибка AI сохраняет draft
- [ ] run tests — должны пройти

### Task 7: Write-path — сохранение Items (сервер выводит суммы) + read-path

**Files:**
- Modify: `internal/rest/handlers.go` (`operationRequest`:481, `handleCreateOperation`:691, `validateOperationRequest`:515 — всё в handlers.go, НЕ в dto.go)
- Modify: `internal/rest/dto.go` (`operationDto`:56 + `toOperationDto`:218 — **read-path**)
- Modify: `internal/rest/handlers_test.go`

- [ ] добавить `items []draftItem` в `operationRequest` (структура в `handlers.go:481`, опционально, аддитивно)
- [ ] при наличии `items`: сервер вызывает `api.DeriveShares` → маппит `userId→User` через `room.Members` → **сам** формирует `RecipientsWithSum` и `Sum`, игнорируя клиентские плоские суммы; `SplitType = by_exact_amount`; сохраняет `Items` в операцию. userId из ядра не найден в `room.Members` → 400
- [ ] валидация items: непусто, все userId из комнаты, `DeriveShares` без ошибки, **`Unknown` пуст** (нельзя сохранить нераспознанные имена — иначе 400)
- [ ] при отсутствии `items`: поведение как раньше (плоские суммы), `Items = nil`
- [ ] **read-path (находка Codex — иначе iOS detail не увидит позиций):** добавить `items` в `operationDto` (`dto.go:56`) и заполнять в `toOperationDto` (`dto.go:218`), чтобы `GET /rooms`, `/operations`, activity отдавали позиции
- [ ] тесты: создание itemized-операции (суммы выведены на сервере, клиентские проигнорированы); создание обычной операции без items; невалидные items → 400; **непустой Unknown → 400**; **GET room/operations itemized-операции возвращает items**
- [ ] run tests — должны пройти

### Task 8: Затирание Items на плоских путях правки (сервер)

**Files:**
- Modify: `internal/rest/handlers.go` (`handleUpdateOperation`:778)
- Modify: `internal/rest/handlers_test.go`

- [ ] `handleUpdateOperation` (:778): если запрос **без** `items` — принудительно `Items = nil` (плоское обновление затирает позиции)
- [ ] если запрос **с** `items` — пересчитать `RecipientsWithSum`/`Sum` из Items (как в Task 7)
- [ ] тест: itemized-операция → PUT без items → Items очищены, плоские суммы сохранены; PUT с items → Items обновлены и суммы выведены
- [ ] run tests — должны пройти

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

- [ ] добавить `Aliases []string` в `User` (bson/json, omitempty)
- [ ] `AddUserAlias(userId, alias)` — `$addToSet`, нормализация (trim/lower), дедуп
- [ ] `POST /api/v1/users/{id}/aliases` под auth; **зарегистрировать маршрут в `server.go` Handler** (`mux.Handle("POST /api/v1/users/{id}/aliases", s.auth(...))`); **область записи:** разрешить дозапись только если целевой user состоит с вызывающим в общей комнате (алиас пишется в чужой документ участника)
- [ ] parse-хендлер (Task 6) уже читает aliases из коллекции user — проверить связку
- [ ] тесты: добавление алиаса, идемпотентность ($addToSet), нормализация; **401 без токена, 403 если нет общей комнаты, 200 happy path**
- [ ] run tests — должны пройти

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
- [ ] тесты ViewModel-логики черновика: сериализация items, вывод per-person сумм (клиентское превью), сброс items при ручной правке
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
- [ ] правка itemized-операции старым плоским путём затирает Items (нет разъезда)
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
- Настроить `GEMINI_API_KEY` и лимиты в окружении деплоя (docker-compose/ENV)
