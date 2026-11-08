# Real Estate Pro

[English](README.md) | [Русский](README.ru.md)

Educational team project (4 people): a microservices aggregator of rental and sale apartment listings with search and Telegram notifications.

My part of the project: the Go notifier (Telegram Bot API, Redis Streams), the Telegram bot, and shared service orchestration.

## Stack and services

| Component | Stack / port | Purpose |
|---|---|---|
| `core-api` | Python, FastAPI, SQLAlchemy, Alembic / 8000 | Listings, search, reference data, cache, outbox, event publishing |
| `collector` | Python, httpx, Jinja2, HTMX / 8001 | API adapter, mock data, normalization, delivery, web routes |
| `notifier` | Go, go-redis, Telegram Bot API / 8080 | Users, subscriptions, Streams, delivery jobs and notifications |
| `postgres` | PostgreSQL / 5432 | Schemas `core` and `notifier` |
| `redis` | A single Redis 7+ instance / 6379 | Cache, preliminary deduplication, Streams |

HTML scraping of listing sites is not used. An optional mock server may be added as a separate Compose profile; the three main services work without it.

## Data flow

1. The adapter converts a source response into the internal `RawListing` format. The mock adapter reads the same internal format from a local dump.
2. Collector normalizes the data and calls `POST /api/v1/listings`.
3. Core API saves the listing and an outbox job in a single transaction. A repeated submission returns the existing record.
4. A Core API background task invalidates the cache and adds an event to the `listings.new` Redis Stream.
5. Notifier reads the event via a consumer group, stores delivery jobs in PostgreSQL and acknowledges the event.
6. Jobs are sent to Telegram with rate limits respected. Without a token, matches are written to the log.

Collector and notifier never touch the `core` schema directly: only through the API and events.

## Sources and mock mode

Two adapters are required: `AvitoApiAdapter` and `MockAdapter`.

The real adapter is implemented against a specific documented Avito API method with the required permissions and authorization. Access to global search, a tariff, or a way to pass credentials is not assumed without checking the method documentation. Without credentials, the adapter is verified against fixtures; this does not replace verification against the real API. The demo does not require purchasing an account.

The dump contains at least 500 synthetic listings for 5 countries and 10 cities. It stores the internal `RawListing` format; samples of the external API response are used separately for adapter tests. Multi-country mock data is never presented as an Avito response.

Two mock modes:

| Mode | Behavior |
|---|---|
| `batch` | Processing the dump as a whole, manually or on a schedule; external IDs are stable |
| `stream` | New objects are created one at a time every 5–30 seconds; the batch scheduler is disabled, manual import remains available |

A newly generated object gets a unique `external_id`; a repeated delivery of that object keeps the same ID. Falling back from the real source to mock is explicitly reflected in logs and `source=mock`.

## Running the current skeleton

Docker with Compose is required, e.g. Docker Desktop.

```bash
git clone https://github.com/Ruv1c0n/real-estate-pro.git
cd real-estate-pro
docker compose up --build
```

To enable the existing Telegram bot, copy `.env.example` to `.env` next to `docker-compose.yml` and fill in:

```dotenv
TELEGRAM_BOT_TOKEN=your_token
ADMIN_KEY=key_for_debug_event
```

Without a token, notifier writes matches to stdout. In the current Compose, `MOCK_MODE` is set directly to `true`; the Python mock logic does not exist yet. Adding the variable to `.env` by itself does not change the value hardcoded in Compose.

Once the skeleton is up:

- `http://localhost:8000` and `http://localhost:8001` — temporary HTTP servers; the real applications are not implemented yet.
- `http://localhost:8080/health` — notifier health check: Redis and Core API reference data availability, delivery mode (telegram/log).
- `POST http://localhost:8080/debug/event` with the `X-Admin-Key` header — a demo event: the body is validated and published to the `listings.new` stream, going through the regular delivery path. If `ADMIN_KEY` is not set, the endpoint responds with 404.

Logs and shutdown:

```bash
docker compose logs -f
docker compose down
```

`docker compose down -v` removes volumes, including PostgreSQL data. For a regular shutdown, use the command without `-v`.

## Target configuration (per the spec)

`.env.example`, Redis volume/AOF/noeviction and notifier readiness checks have been added. What remains are migrations for the `notifier` schema (SQL migrations are prepared by Ilya), after which subscriptions, the journal and deferred jobs move from in-process memory to PostgreSQL. With `.env` filled in, startup is a single `docker compose up --build`.

| Setting | Purpose / value |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Without a token — log mode; implemented in notifier |
| `DATABASE_URL` | PostgreSQL address; different permissions per service schema |
| `REDIS_URL` | Redis address; currently set in Compose |
| `CORE_API_URL` | Core API address for collector and notifier reference data |
| `MOCK_MODE` | `true` by default; still needs to be wired into collector configuration |
| `MOCK_PUBLISH_MODE` | `batch` by default or `stream`; to be implemented |
| Admin key | Variable `ADMIN_KEY`, header `X-Admin-Key`; an empty value disables `/debug/event` |
| API credentials | The set of variables is determined by the chosen method and its authorization |

Default parameters: cache TTL 600 s, collection every 15 minutes, up to five delivery retries after the first attempt with 1–30 s delays, up to 10 subscriptions and 30 notifications per rolling hour per user, eight notifier workers. Data exchange uses UTC; display and scheduling use Europe/Minsk.

## Target API

| Method and path | Purpose |
|---|---|
| `POST /api/v1/listings` | New listing: 201 `{id, created:true}`; duplicate: 200 `{id, created:false}` |
| `GET /api/v1/listings` | Search, sorting and pagination; response `{items, total, page, size}` |
| `GET /api/v1/listings/{id}` | Listing card; 404 for an unknown ID |
| `GET /api/v1/countries` | Countries |
| `GET /api/v1/cities?country=BY` | Cities of the selected country |
| `GET /api/v1/sources` | Sources |
| `GET /api/v1/currencies` | Currencies and rates |
| `POST /collect/run` | Background batch collection: 202 with `run_id`; 409 if already running |
| `GET /collect/runs/{run_id}` | Batch collection status |
| `POST /debug/event` | Validation and adding a test event to Streams |
| `GET /health` | Service and dependency health |

Collector's utility routes and `/debug/*` are protected by `X-Admin-Key`. Core API serves OpenAPI at `/docs`.

Filters: `deal_type`, `country`, `city`, `price_min`, `price_max`, `rooms`, `area_min`, `area_max`, `source`. Sorting via `sort`: `price_asc`, `price_desc`, `date_asc`, `date_desc` (default); ties are broken by `id`. `page` starts at 1, `size` is 20 by default, 100 maximum. JSON API errors, including 422: `{error, message}`.

Required listing fields: `source`, `external_id`, `url`, `deal_type`, `country`, `city`, `price`, `currency`, `price_usd`, `rooms`. `rooms` is an integer ≥0, `0` means a studio; `null` is not allowed. `title`, address, area, floor, description, photos and the source date are optional.

## Reference data, prices and duplicates

- Country: ISO alpha-2 code, e.g. `BY`. City: canonical Latin name, e.g. `Minsk`, unique within a country. Alternatives like «Минск»/«Мінск» are resolved via the reference data; an unknown or ambiguous city yields 422.
- `rate_to_usd` — the price of one currency unit in USD. Prices are converted using Decimal and rounded `ROUND_HALF_UP` to whole USD; PostgreSQL uses NUMERIC. The rate source is the Core API reference data.
- Identity is `(source, external_id)`; PostgreSQL guarantees uniqueness even under parallel requests. The Redis hash flag is set after a successful API response.
- `content_hash` links possible duplicates across sources while keeping both records; incomplete data is not used for automatic linking.
- List TTL is 10 minutes; the key accounts for parameters, pagination and `cache:version`. After a Redis failure, a stale cache is not used until a successful invalidation.

## The `listings.new` event — Redis Streams

Target format: the `data` field contains JSON. `event_id` is the outbox job UUID, stable across retries; `id` is the listing ID in Core API.

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
  "created_at": "2026-10-27T10:00:00Z"
}
```

`event_id`, `source`, `country`, `city`, `currency`, `url` are strings; `id` is an integer >0; `rooms` is an integer ≥0; `deal_type` is `rent`/`sale`; prices are numbers >0; `address` is a string or `null`; `created_at` is ISO 8601 UTC. All keys are present. `area_m2`, `title` and `description` are not part of the base event; area-based subscription filtering is not supported.

Delivery:

- Core API: `XADD listings.new * data <json>` after the listing and outbox are committed to PostgreSQL. On a Redis failure, the job remains for a retry.
- Notifier: the `notifier` group is created at initial position `0` with `MKSTREAM`; reading via `XREADGROUP`. Pending entries older than 30 s are reclaimed via `XAUTOCLAIM`; long processing requires ownership extension.
- `XACK` happens after delivery jobs are stored. A malformed payload first goes to `listings.dead` with a reason, and is then acknowledged.
- The event delivery model is at-least-once. A unique user–listing pair, reservations and the journal prevent ordinary notification duplicates. An uncertain external delivery result gets the `unknown` status; absolute exactly-once Telegram delivery is not guaranteed.
- A user limit defers sending to PostgreSQL rather than dropping the notification. Log mode is tracked separately from Telegram.
- 10,000 events is a soft monitoring threshold. Unconditional `MAXLEN` trimming is not used: only safely processed entries may be removed, accounting for pending and not-yet-read events.
- Redis must use a volume, AOF `appendfsync everysec` and `noeviction`. A notifier restart does not lose events; a Redis failure may lose the last AOF interval. Volume deletion and disk loss are not covered by the guarantee.

## Telegram commands

| Command | Action per the spec |
|---|---|
| `/start` | User registration |
| `/subscribe` | Create a subscription with reference data validation |
| `/list` | Own subscriptions |
| `/unsubscribe <id>` | Remove one of your subscriptions |
| `/stop` | Delete the user, subscriptions, journal and deferred jobs |

## Team

| Member | Responsibility |
|---|---|
| Ilya Golubev | Both PostgreSQL schemas, migrations, reference data, relations, indexes, roles, backups |
| Dima Budaev | HTML/CSS and templates from examples, mock data, manual testing, documentation |
| Timur Gadzhigasanov | Core API and collector, normalization, cache, outbox, Streams publishing, web routes |
| Daniil Matveev | Go notifier, Telegram, Streams consumption and delivery, Docker Compose and integration |

## Docs

- [Real_Estate_Pro_TZ.pdf](docs/Real_Estate_Pro_TZ.pdf) — the requirements spec (numbered: ФТ-x.x functional, НФТ-x non-functional).
- [docs/contracts.md](docs/contracts.md) — the actual inter-service contracts (Redis Streams, reference data, outbox). In case of any conflict with this README, contracts.md wins.
- [Contributing](CONTRIBUTING.md) — branch/commit conventions, CI and the spec's change-control rules.

## License

MIT, see [LICENSE](LICENSE).
