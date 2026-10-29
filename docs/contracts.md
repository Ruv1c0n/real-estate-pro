## Общие соглашения

| Тема | Решение |
|---|---|
| Формат | JSON, UTF-8, поля в `snake_case` |
| Базовая валюта | USD; `price_usd` вычисляет Collector, Core API проверяет |
| Курс | `rate_to_usd` — стоимость единицы валюты в USD; округление `ROUND_HALF_UP` до целого USD, вычисления Decimal |
| Страна | Код ISO alpha-2: `BY`, `RU`, `PL` |
| Город | Каноническое латинское название из справочника: `Minsk`; определяется вместе со страной |
| Тип сделки | `rent` / `sale` |
| Комнаты | Целое ≥0; `0` = студия; `null` запрещён |
| Время | ISO 8601 в UTC: `2026-10-28T10:00:00Z` |

Collector приводит написания городов к справочнику; Notifier проверяет по нему подписки. Неизвестные и неоднозначные значения отклоняются.

## Поток данных

Collector отправляет объявление в Core API. Core API сохраняет объявление и outbox-задание одной транзакцией; фоновый обработчик публикует событие в Redis Streams. Notifier сохраняет задания доставки и отправляет уведомления в Telegram.

Дубль по `(source, external_id)` возвращает существующую запись без обновления и нового события.

## 1. POST /api/v1/listings (Collector → Core API)

Запрос:

```json
{
  "source": "mock",
  "external_id": "seed-000123",
  "deal_type": "rent",
  "country": "BY",
  "city": "Minsk",
  "rooms": 1,
  "area_m2": 38.5,
  "price": 380,
  "currency": "USD",
  "price_usd": 380,
  "title": "1-к квартира, 38.5 м²",
  "address": "ул. Примерная, 1",
  "url": "https://example.com/listing/123"
}
```

| Поле | Тип | Обязательное | Примечание |
|---|---|---|---|
| `source` | string | да | Код источника из справочника |
| `external_id` | string | да | Стабильный ID; вместе с `source` уникален |
| `deal_type` | string | да | `rent` / `sale` |
| `country` | string | да | Код страны из справочника |
| `city` | string | да | Каноническое название внутри страны |
| `rooms` | int | да | ≥0; `0` = студия |
| `price` | number | да | Исходная цена >0 |
| `currency` | string | да | Код ISO 4217 из справочника |
| `price_usd` | int | да | Округлённая цена USD >0 |
| `url` | string | да | Абсолютная HTTP(S)-ссылка; у mock — синтетическая |
| `area_m2` | number или null | нет | >0, если задана |
| `title`, `address`, `description` | string или null | нет | Описание очищено от HTML |
| `floor` | int или null | нет | Этаж |
| `photos` | array[string] или null | нет | HTTP(S)-ссылки |
| `published_at` | string или null | нет | ISO 8601 UTC |

Обязательные строки непустые. Необязательные поля можно опустить. `id` и `created_at` создаёт Core API.

Ответы:

- `201 Created` — объявление и outbox-задание сохранены; публикация выполняется асинхронно:

  ```json
  {"id": 123, "created": true}
  ```

- `200 OK` — дубль; новое событие не создаётся:

  ```json
  {"id": 123, "created": false}
  ```

- `422 Unprocessable Entity` — ошибка полей, справочников или пересчёта цены:

  ```json
  {"error": "validation_error", "message": "rooms должен быть целым числом >= 0"}
  ```

При сетевой ошибке, 429 или 5xx Collector повторяет запрос до пяти раз после первой попытки; задержки 1–30 с, с учётом `Retry-After`. Внешний ID при повторе не меняется. Redis отмечает объявление обработанным только после 200/201; окончательную уникальность обеспечивает PostgreSQL.

## 2. Событие listings.new (Core API → Redis Streams → Notifier)

Stream: **`listings.new`**. Поле Redis-записи **`data`** содержит JSON-строку:

```json
{
  "event_id": "550e8400-e29b-41d4-a716-446655440000",
  "id": 123,
  "source": "mock",
  "deal_type": "rent",
  "country": "BY",
  "city": "Minsk",
  "rooms": 1,
  "price": 380,
  "currency": "USD",
  "price_usd": 380,
  "address": "ул. Примерная, 1",
  "url": "https://example.com/listing/123",
  "created_at": "2026-10-28T10:00:00Z"
}
```

Все ключи обязательны. Типы полей объявления — как в разделе 1. Дополнительные поля события:

| Поле | Тип | Примечание |
|---|---|---|
| `event_id` | string (UUID) | Устойчивый ID outbox-задания; не меняется при повторной публикации |
| `id` | int | ID объявления >0 |
| `created_at` | string | Время создания объявления, ISO 8601 UTC |

`address` может быть `null`. `area_m2`, `title`, `description` и `photos` в событие не входят; подписки не фильтруются по площади.

Правила доставки:

- Публикация: `XADD listings.new * data <json>`. При ошибке Redis outbox-задание остаётся для повтора.
- Consumer group — `notifier`, начальная позиция `0`, создание с `MKSTREAM`; имя consumer уникально для экземпляра.
- Чтение — `XREADGROUP`; pending старше 30 с — `XAUTOCLAIM`. При долгой обработке владение продлевается.
- `XACK` — после сохранения заданий доставки. Повтор события не создаёт повторные задания; несколько подписок пользователя дают одно уведомление на объявление.
- Некорректный payload сначала сохраняется в `listings.dead` с исходным ID и причиной, затем подтверждается.
- Доставка событий — как минимум один раз. Журнал предотвращает обычные повторы Telegram; неопределённый результат отправки получает статус `unknown`, автоматический повтор не выполняется.
- Лимит — 30 уведомлений пользователю за скользящий час; лишние задания откладываются в PostgreSQL. Log-режим учитывается отдельно от Telegram.
- Redis использует volume, AOF `appendfsync everysec` и `noeviction`. Порог 10 000 событий — мягкий; необработанные записи не обрезаются. Перезапуск Notifier не теряет события; авария Redis может потерять последний интервал AOF.

## 3. Справочники, чтение и mock

| Метод | Ответ |
|---|---|
| `GET /api/v1/countries` | Массив `{code, name}` |
| `GET /api/v1/cities?country=BY` | Массив `{country, name, aliases}`; `name` — каноническое название |
| `GET /api/v1/sources` | Массив `{code, name}` |
| `GET /api/v1/currencies` | Массив `{code, rate_to_usd}` |
| `GET /api/v1/listings/{id}` | Карточка: поля объявления, `id`, `created_at`; неизвестный ID — 404 |
| `GET /api/v1/listings` | `{items, total, page, size}`; `items` — карточки |

Фильтры списка: `deal_type`, `country`, `city`, `price_min`, `price_max` (USD), `rooms`, `area_min`, `area_max`, `source`. Если задан город, нужна страна. Сортировка: `price_asc`, `price_desc`, `date_asc`, `date_desc` (по умолчанию), затем `id` в том же направлении. `page` ≥1, по умолчанию 1; `size` 1–100, по умолчанию 20. Пустая страница возвращается с 200. Ошибки JSON API имеют формат `{error, message}`.

`RawListing` — общий внутренний формат Collector. Адаптер преобразует в него ответ API, mock читает его из дампа; структура внешнего API и RawListing описываются отдельно при реализации адаптера. Синтетический дамп: ≥500 объектов, ≥5 стран, ≥10 городов.

`MOCK_MODE=true` по умолчанию. `MOCK_PUBLISH_MODE=batch` — загрузка дампа с постоянными ID; `stream` — новые объекты каждые 5–30 с с уникальными ID. В stream-режиме пакетный планировщик отключён; ручной импорт через `POST /collect/run` доступен.
