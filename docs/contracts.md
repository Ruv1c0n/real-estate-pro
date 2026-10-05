# Контракты между сервисами

Статус: **черновик на согласование** (Илья, Дима). После подтверждения менять только по договорённости всех троих.

## Общие соглашения

| Тема | Решение |
|---|---|
| Формат | JSON, UTF-8, имена полей в `snake_case` |
| Базовая валюта | **USD**. Поле `price_usd` заполняет Collector всегда |
| Страна | ISO 3166-1 alpha-2, верхний регистр: `BY`, `RU`, `PL` |
| Город | Каноническое название **латиницей, на английском**: `Minsk`, `Moscow`. Приведение к канону делает Collector |
| Тип сделки | `rent` или `sale` |
| Комнаты | целое число; `0` = студия; `null` = неизвестно |
| Время | ISO 8601 в UTC, например `2026-10-05T12:30:00Z` |

## Поток данных

```
Collector --POST /api/v1/listings--> Core API --PUBLISH listings.new--> Redis --> Notifier (Go) --> Telegram
```

Событие публикует **Core API после успешного INSERT**. Дубли (по `source` + `external_id`) в канал не попадают.

## 1. POST /api/v1/listings (Collector → Core API)

Запрос:

```json
{
  "source": "avito",
  "external_id": "2845513901",
  "deal_type": "rent",
  "country": "BY",
  "city": "Minsk",
  "rooms": 1,
  "area_m2": 38.5,
  "price": 1100,
  "currency": "BYN",
  "price_usd": 350,
  "title": "1-к квартира, 38 м²",
  "description": "Очищенный от HTML текст",
  "url": "https://example.com/listing/2845513901"
}
```

| Поле | Тип | Обязательное | Примечание |
|---|---|---|---|
| source | string | да | `avito`, `cian`, `yandex`, `mock`, ... |
| external_id | string | да | ID на стороне источника. Вместе с `source` уникален |
| deal_type | string | да | `rent` / `sale` |
| country | string | да | ISO alpha-2 |
| city | string | да | см. соглашения |
| rooms | int \| null | нет | `0` = студия |
| area_m2 | number \| null | нет | |
| price | number | да | цена в исходной валюте |
| currency | string | да | ISO 4217: `USD`, `BYN`, `RUB`, ... |
| price_usd | number | да | пересчёт в USD, `> 0` |
| title | string | да | |
| description | string | нет | без HTML |
| url | string | да | ссылка на первоисточник |

Ответы:

- `201 Created` — объявление сохранено, событие опубликовано:
  ```json
  { "id": 123, "created": true }
  ```
- `200 OK` — дубль, ничего не записано, события нет:
  ```json
  { "id": 98, "created": false }
  ```
- `422 Unprocessable Entity` — ошибка валидации (стандартный формат FastAPI).

## 2. Событие `listings.new` (Core API → Redis Pub/Sub → Notifier)

Канал Redis: **`listings.new`**. Сообщение — JSON-строка:

```json
{
  "id": 123,
  "source": "avito",
  "deal_type": "rent",
  "country": "BY",
  "city": "Minsk",
  "rooms": 1,
  "price": 1100,
  "currency": "BYN",
  "price_usd": 350,
  "title": "1-к квартира, 38 м²",
  "url": "https://example.com/listing/2845513901",
  "created_at": "2026-10-05T12:30:00Z"
}
```

| Поле | Тип | Обязательное | Примечание |
|---|---|---|---|
| id | int | да | id объявления в Postgres Core API |
| source | string | да | |
| deal_type | string | да | `rent` / `sale` |
| country | string | да | ISO alpha-2 |
| city | string | да | каноническое название |
| rooms | int \| null | да | ключ есть всегда, значение может быть `null` |
| price | number | да | исходная валюта |
| currency | string | да | ISO 4217 |
| price_usd | number | да | `> 0` |
| title | string | да | |
| url | string | да | |
| created_at | string | да | ISO 8601 UTC |

В событии нет `description`: Notifier он не нужен, а сообщения остаются лёгкими.

## 3. Открытые вопросы (решить до 10.10)

1. Подходит ли `Minsk` латиницей как канон города, или нужен отдельный справочник `cities`?
2. Нужна ли в событии площадь `area_m2` (для фильтров подписок)?
3. Pub/Sub теряет события, если Notifier в этот момент перезапускается. Для зачёта принимаем это или переходим на Redis Streams?