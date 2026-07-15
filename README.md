# Telegram бот [Split it](https://t.me/split_money_bot)

## Инструкции по локальной разработке

Приложение ожидает следующие переменные окружения:

* `TG_TOKEN` – токен полученный от BotFather
* `DB_HOST` – хост от mongodb
* `DB_NAME` – название db

Дополнительные переменные окружения со значениями по-умолчанию:

* `TG_DEBUG` (false) – включает режим отладки (логируется больше событий)
* `DEFAULT_LANGUAGE` (en) – язык в боте 

Запустить бота можно через Docker Compose:

```bash
docker-compose up splitty
```

## REST API

Помимо бота приложение поднимает REST API для мобильных клиентов (контракт: [docs/API.md](docs/API.md)).
Сервер запускается всегда; бот — опционален (если `TG_TOKEN` пуст или невалиден, приложение
пишет warning и продолжает работать только с REST API).

Переменные окружения REST API:

* `LISTEN` (`localhost:7171`) – адрес HTTP-сервера
* `API_JWT_SECRET` – секрет подписи JWT (HS256), **обязательна**: без неё сервер не стартует
  (сгенерировать: `openssl rand -hex 32`). Исключение — режим разработки `API_DEV_AUTH=true`:
  пустой секрет заменяется случайным эфемерным, все выданные токены протухают при рестарте
* `API_DEV_AUTH` (`false`) – включает `POST /api/v1/auth/dev` для разработки
  (вход под любым userId без проверки — никогда не включайте в проде)

Быстрая проверка:

```bash
curl http://localhost:7171/health
```

## AI-распознавание расхода (голос + фото чека)

REST-эндпоинт `POST /api/v1/rooms/{roomId}/operations/parse` распознаёт расход из голоса,
фото чека или текста в структурированный черновик (контракт — [docs/API.md](docs/API.md)).
Эндпоинт stateless: ничего не создаёт, только «черновик + ввод → черновик»; сохранение —
обычным `POST/PUT /operations` с полем `items`.

Переменные окружения:

* `GEMINI_API_KEY` (`""`) – ключ Google Gemini; пустой → `/parse` отдаёт `503`, остальной сервер работает как раньше
* `GEMINI_MODEL` (`gemini-2.0-flash`) – модель Gemini
* `AI_PARSE_RATE_PER_MIN` (`5`) – лимит запросов `/parse` на пользователя в минуту
* `AI_PARSE_DAILY_QUOTA` (`50`) – суточная квота запросов `/parse` на пользователя
* `AI_MAX_BODY_BYTES` (`15728640`) – общий лимит тела `/parse` (отдельно от глобального 1 МБ)

Архитектурные паттерны:

* **`internal/ai`** — распознаватель за интерфейсом `Parser` (`Parse(ctx, ParseInput) (ParseResult, error)`),
  провайдер скрыт; текущая реализация — Gemini-клиент (`gemini.go`, `generateContent` REST,
  медиа как `inline_data` base64). Транспортный черновик `ai.Draft`/`ai.DraftItem`/`ai.ItemShare`
  переиспользуется REST-слоем (не троим структуру в api/ai/rest).
* **Rate-limit** — `service.RateLimiter` (минутное окно + суточная квота на пользователя) поверх
  коллекции Mongo **`ai_usage`** с TTL-индексом (`expires_at`, `expireAfterSeconds=0` —
  окна сами протухают). Проверяется до чтения тела и вызова модели.
* **`Operation.Items` — источник правды.** Когда операция создаётся/правится с `items`, сервер
  сам выводит плоские `RecipientsWithSum`/`Sum` из позиций (`api.DeriveShares`) и игнорирует
  клиентские плоские суммы; `SplitType = by_exact_amount`. Долги, уведомления, бот и Android
  работают на плоских суммах и про `Items` не знают. Плоская правка без `items` затирает `Items`.
* **DI (не wire):** `ai.Parser` + `*service.RateLimiter` прокидываются в REST через
  `Server.SetAI(...)` в `cmd/splitty/main.go:initRestServer` (под `GEMINI_API_KEY != ""`),
  а не через wire (wire строит только бота).

> Примечание: в корне репозитория нет `CLAUDE.md`/`AGENTS.md` — заметки о паттернах AI-парсинга
> добавлены сюда, в README (ближайший подходящий существующий документ).