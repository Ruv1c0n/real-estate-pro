# Контракты, которые нужно согласовать

## Collector → Core API (Илья)
POST /api/v1/listings, тело = ListingCreate (collector/app/schemas.py).
Ответы, которые ждёт collector: 201/200 = сохранено, 409 = дубль (url_hash или content_hash), остальное = ошибка.
Decimal уходит строкой ("1250.50"), даты в ISO 8601.

## UI → Core API (Илья), предположения
GET /api/v1/listings?country&city&deal_type&rooms&price_max&page&page_size
  -> {"items": [...], "total": N}  (в items есть id, title, city, address, rooms, area_m2,
     floor, floors_total, price, currency, price_usd, deal_type, url, source)
GET /api/v1/listings/{id} -> один объект с теми же полями + description, country.
price_max фильтрует по price_usd.

## Redis Pub/Sub (Даниил)
Публикацию события о новой квартире предполагается делать из Core API после успешного INSERT.
Collector в Redis сам не пишет.
