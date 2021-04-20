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