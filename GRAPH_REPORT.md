# Graph Report - real-estate-pro  (2026-11-18)

## Corpus Check
- 88 files · ~65,100 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 13 file(s) not represented in the graph (top: (none) 10, .example 1, .ini 1)

## Summary
- 1278 nodes · 3192 edges · 85 communities (60 shown, 25 thin omitted)
- Extraction: 84% EXTRACTED · 16% INFERRED · 0% AMBIGUOUS · INFERRED: 526 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `78fc518c`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- main.go
- Core API Config & Filters
- testing.T
- Core API Health
- Core API Bootstrap & Errors
- jobs
- Python CI Workflow
- Bot
- Stream Consumer & Claim
- Telegram API Client
- Dependabot Config
- Compose Environment Vars
- Collector Requirements (TZ)
- Event Delivery & Notification Rules
- Team Process & Migrations
- Compose Logging
- Core API Listings (TZ)
- Client
- Compose Infrastructure
- Notifier Requirements (TZ)
- Team Ownership
- Core API Dependencies
- Compose Service Ordering
- Reference Data (TZ)
- Core API Concepts
- Notifier Concepts
- Compose Healthchecks
- Collector Compose Service
- Deduplication Rules
- Notifier SQL Schema
- Python Dev Tooling
- Redis Compose Service
- Project Documentation
- fakeRedis
- Security Policy
- Russian Contributing Guide
- Contract Conventions
- Secrets via .env
- Compose Launch Requirement
- Git Process Requirement
- Browser Interface Requirement
- SQL Injection Protection
- Internal Network Isolation
- Healthcheck Orchestration
- External Call Timeouts
- Notifier Compose Service
- Russian README
- context.Context
- ListingCreate
- postgres_test.go
- with
- postgres
- test_migrations.py
- Dispatcher
- Subscription
- health.py
- DeferredJob
- steps
- Event
- asyncio
- main.py
- Таблицы
- sync.Mutex
- runConsumer
- steps
- Reaper
- test_listing_filters_http.py
- Резервное копирование и проверка восстановления
- Быстрый старт по схеме core — для разработчиков
- ci.yml
- postgres
- reaper_test.go
- test_listings_http.py
- 0001_core_schema.py
- .XRange
- Бенчмарк доставки notifier (НФТ-11)
- redis
- env
- syncBuf
- notifier-p95.sh
- check-md-links.sh

## God Nodes (most connected - your core abstractions)
1. `testEvent()` - 46 edges
2. `NewDispatcher()` - 37 edges
3. `NewMemory()` - 35 edges
4. `Event` - 29 edges
5. `mustAddUser()` - 25 edges
6. `ListingFilters` - 24 edges
7. `Subscription` - 24 edges
8. `Client` - 23 edges
9. `resetTestDB()` - 22 edges
10. `Dispatcher` - 22 edges

## Surprising Connections (you probably didn't know these)
- `Событие listings.new (Core API → Redis Streams → Notifier)` --shares_data_with--> `Redis (кэш, SET, Streams, порт 6379)`  [INFERRED]
  docs/contracts.md → docs/Real_Estate_Pro_TZ.pdf
- `ФТ-3.7: GET /health notifier и POST /debug/event` --references--> `Событие listings.new (Core API → Redis Streams → Notifier)`  [INFERRED]
  docs/Real_Estate_Pro_TZ.pdf → docs/contracts.md
- `ФТ-3.5: Журнал дедупликации уведомлений (user_id, listing_id)` --semantically_similar_to--> `Дедупликация по паре (source, external_id)`  [INFERRED] [semantically similar]
  docs/Real_Estate_Pro_TZ.pdf → docs/contracts.md
- `ФТ-1.6: Доставка в Core API и повторы (1–30 с, Retry-After)` --references--> `POST /api/v1/listings (Collector → Core API)`  [EXTRACTED]
  docs/Real_Estate_Pro_TZ.pdf → docs/contracts.md
- `ФТ-2.1: POST /api/v1/listings — валидация и сохранение` --references--> `POST /api/v1/listings (Collector → Core API)`  [EXTRACTED]
  docs/Real_Estate_Pro_TZ.pdf → docs/contracts.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Поток данных листинга: Collector → Core API → outbox → Redis Stream → Notifier → Telegram** — docs_real_estate_pro_tz_collector, docs_contracts_post_api_v1_listings, docs_real_estate_pro_tz_transactional_outbox, docs_contracts_listings_new, docs_real_estate_pro_tz_notifier, docs_real_estate_pro_tz_telegram [EXTRACTED 1.00]
- **Listing delivery pipeline (collector -> core-api -> listings.new -> notifier -> Telegram)** — readme_collector, readme_core_api, readme_listings_new, readme_notifier, readme_telegram_bot_api [EXTRACTED 1.00]
- **Mock data ecosystem (adapters, modes, RawListing dump)** — readme_collector, readme_avito_api_adapter, readme_mock_adapter, readme_mock_modes, readme_rawlisting [EXTRACTED 1.00]
- **Надёжная доставка событий: outbox, pending/XAUTOCLAIM, DLQ, AOF, лимит уведомлений** — docs_real_estate_pro_tz_transactional_outbox, docs_real_estate_pro_tz_pending_ownership, docs_contracts_listings_dead, docs_real_estate_pro_tz_aof_durability, docs_contracts_notification_limit [EXTRACTED 1.00]
- **Required CI checks for merge (require-ci ruleset)** — contributing_ci_checks, contributing_require_ci, contributing_gitleaks, contributing_python_ci [EXTRACTED 1.00]
- **Многоуровневая дедупликация: Redis-хэш Collector → уникальность PostgreSQL → журнал уведомлений notifier** — docs_contracts_dedup_source_external_id, docs_real_estate_pro_tz_url_hash, docs_real_estate_pro_tz_dedup_urls, docs_real_estate_pro_tz_фт_3_5, docs_real_estate_pro_tz_exactly_once [INFERRED 0.85]

## Communities (85 total, 25 thin omitted)

### Community 0 - "main.go"
Cohesion: 0.05
Nodes (65): checkResult, FormatEvent(), TestFormatEventGiantCurrencyUnconditionalTier(), TestFormatEventNewContract(), TestFormatEventNilAddressUnchanged(), TestFormatEventShortEventUnchanged(), TestFormatEventTruncatesGiantURL(), TestFormatEventTruncatesLongAddress() (+57 more)

### Community 1 - "Core API Config & Filters"
Cohesion: 0.11
Nodes (28): Settings, ListingFilters, clean_settings_env(), test_database_url_accepts_asyncpg(), test_database_url_built_from_parts_escapes_special_characters(), test_database_url_rejects_wrong_driver(), test_database_url_takes_priority_over_parts(), test_database_url_validation_error_hides_password() (+20 more)

### Community 2 - "testing.T"
Cohesion: 0.09
Nodes (99): New(), newTestBot(), TestStartEnsuresUser(), TestStartSubscribeStopStartScenario(), TestStopDeletesUserAndReportsCount(), TestStopDropsDeferredJobs(), TestStopRemovesAllSubscriptions(), TestStopWithoutCleanerStillWorks() (+91 more)

### Community 3 - "Core API Health"
Cohesion: 0.08
Nodes (12): check_postgres(), check_redis(), health_client(), test_dependency_check_timeout(), scenario(), hanging_operation(), test_health_summary(), test_probe_external_cancellation_propagates() (+4 more)

### Community 4 - "Core API Bootstrap & Errors"
Cohesion: 0.13
Nodes (5): http_error_handler(), internal_error_handler(), register_error_handlers(), validation_error_handler(), client()

### Community 5 - "jobs"
Cohesion: 0.18
Nodes (11): run, runs-on, runs-on, jobs, docker, gitleaks, notifier, defaults (+3 more)

### Community 6 - "Python CI Workflow"
Cohesion: 0.08
Nodes (28): concurrency, cancel-in-progress, group, jobs, pytest, ruff, service, name (+20 more)

### Community 7 - "Bot"
Cohesion: 0.13
Nodes (7): API, Bot, ChatCleaner, stubValidator, Validator, isAlphaUpper2(), ChatCleaner

### Community 8 - "Stream Consumer & Claim"
Cohesion: 0.22
Nodes (3): isBusyGroup(), Consumer, Redis

### Community 9 - "Telegram API Client"
Cohesion: 0.23
Nodes (5): fakeAPI, Update, NewClient(), Client, Message

### Community 10 - "Dependabot Config"
Cohesion: 0.12
Nodes (15): prefix, patterns, patterns, docker, github-actions, day, interval, updates (+7 more)

### Community 11 - "Compose Environment Vars"
Cohesion: 0.19
Nodes (16): environment, environment, ADMIN_KEY, CORE_API_LOG_LEVEL, CORE_API_URL, MOCK_MODE, POSTGRES_DB, POSTGRES_HOST (+8 more)

### Community 12 - "Collector Requirements (TZ)"
Cohesion: 0.15
Nodes (15): POST /collect/run (ручной импорт), MOCK_MODE и MOCK_PUBLISH_MODE (batch/stream), RawListing — общий внутренний формат Collector, 3.4 Mock и источники, 4.5 Устройство Collector (адаптеры, нормализатор, batch/stream), 4.6 Необязательный mock-сервер источника, collector (httpx, Jinja2, HTMX, порт 8001), Синтетический дамп RawListing (≥500 объектов, ≥5 стран, ≥10 городов) (+7 more)

### Community 13 - "Event Delivery & Notification Rules"
Cohesion: 0.18
Nodes (14): Consumer group notifier (XREADGROUP, XAUTOCLAIM, XACK), listings.dead — некорректный payload (DLQ), Событие listings.new (Core API → Redis Streams → Notifier), Лимит 30 уведомлений за скользящий час, POST /api/v1/listings (Collector → Core API), 3.6 Параметры (TTL, лимиты, интервалы, часовой пояс), 4.3 Redis Streams и outbox (публикация, чтение, подтверждение, Telegram, хранение), 4.4 Поток данных (Collector → core → XADD → notifier → Telegram) (+6 more)

### Community 14 - "Team Process & Migrations"
Cohesion: 0.20
Nodes (11): Alembic migrations (schema core), CI required checks (notifier lint/test, gitleaks, docker compose), Secrets via .env, gitleaks (secret leak detection), require-ci ruleset, SQL migrations (schema notifier), Pull request template, PR checklist (branch, contracts, migrations, schema isolation, secrets) (+3 more)

### Community 15 - "Compose Logging"
Cohesion: 0.20
Nodes (14): logging, build, logging, ports, restart, driver, options, logging (+6 more)

### Community 16 - "Core API Listings (TZ)"
Cohesion: 0.15
Nodes (14): GET /api/v1/listings (фильтры, сортировка, пагинация), GET /api/v1/listings/{id} (карточка), 3.5 Кэш (ключ с хэшем фильтров, TTL 600 с), 6.5 Границы ответственности команды, cache:version и инвалидация кэша, core-api (FastAPI, порт 8000), НФТ-10: p95 GET списка <300 мс с кэшем, <500 мс без, НФТ-12: Ошибки JSON API {error, message} (+6 more)

### Community 17 - "Client"
Cohesion: 0.07
Nodes (31): City, Country, eventJSON, healthProber, fresh(), Client, matchCity(), New() (+23 more)

### Community 18 - "Compose Infrastructure"
Cohesion: 0.17
Nodes (11): image, ports, restart, volumes, services, postgres, volumes, pgdata (+3 more)

### Community 19 - "Notifier Requirements (TZ)"
Cohesion: 0.17
Nodes (12): 3.1 Объявление и валидация, 4.2 Данные PostgreSQL (схемы core и notifier), notifier (Go, go-redis, Telegram Bot API, порт 8080), PostgreSQL (схемы core и notifier, порт 5432), Redis (кэш, SET, Streams, порт 6379), Telegram Bot API (канал доставки уведомлений), НФТ-5: Volumes для PostgreSQL и Redis, НФТ-6: Миграции (Alembic core, SQL notifier) до готовности сервисов (+4 more)

### Community 20 - "Team Ownership"
Cohesion: 0.22
Nodes (10): AvitoApiAdapter, POST /collect/run (background batch collection), Collector (Python, httpx, Jinja2, HTMX, port 8001), content_hash (cross-source duplicate linking), Daniil Matveev (Go notifier, Telegram, Compose), Dima Budaev (UI, mock data, manual testing), MockAdapter, Mock publish modes (batch / stream) (+2 more)

### Community 21 - "Core API Dependencies"
Cohesion: 0.18
Nodes (10): anyio 4.15.1, asyncpg 0.32.0, FastAPI 0.162.0, opentelemetry-api 1.45.1, pydantic 2.14.1, pydantic-settings 2.16.1, python-dotenv 1.2.4, SQLAlchemy[asyncio] 2.1.6 (+2 more)

### Community 23 - "Compose Service Ordering"
Cohesion: 0.22
Nodes (10): depends_on, postgres, redis, build, depends_on, ports, restart, condition (+2 more)

### Community 24 - "Reference Data (TZ)"
Cohesion: 0.24
Nodes (10): GET /api/v1/cities?country=BY, GET /api/v1/countries, GET /api/v1/currencies, GET /api/v1/sources, Курс rate_to_usd и округление ROUND_HALF_UP до целого USD, 3.2 Валюты и время (Decimal, NUMERIC, UTC/Europe/Minsk), 3.7 Справочники стран и городов, ФТ-1.3: Нормализация (USD, HTML, комнаты, справочники) (+2 more)

### Community 25 - "Core API Concepts"
Cohesion: 0.22
Nodes (8): Core API (Python, FastAPI, SQLAlchemy, port 8000), listings.dead (dead-letter stream), POST /debug/event (X-Admin-Key protected demo event), GET /api/v1/listings (search, sorting, pagination), Listing cache (TTL 600s, cache:version, stale-after-Redis-failure), GET /api/v1/listings/{id} (listing card), listings.new event (Redis Streams), Reference data endpoints (/countries, /cities, /sources, /currencies)

### Community 26 - "Notifier Concepts"
Cohesion: 0.25
Nodes (9): redis (redis-py) 8.1.0, Consumer group delivery (XREADGROUP, XACK, XAUTOCLAIM), Docker Compose orchestration, GET /health (notifier health check), Notifier (Go, go-redis, Telegram Bot API, port 8080), Rate limits and deferral (10 subscriptions, 30 notifications/hour, defer to PostgreSQL), Redis 7+ (cache, dedup, Streams, port 6379), Telegram Bot API (+1 more)

### Community 27 - "Compose Healthchecks"
Cohesion: 0.50
Nodes (9): healthcheck, interval, retries, start_period, test, timeout, healthcheck, healthcheck (+1 more)

### Community 28 - "Collector Compose Service"
Cohesion: 0.29
Nodes (7): build, depends_on, ports, restart, condition, core-api, collector

### Community 29 - "Deduplication Rules"
Cohesion: 0.38
Nodes (7): Дедупликация по паре (source, external_id), 3.3 Дедупликация (url_hash, content_hash, dedup:urls), content_hash — связь «возможный дубль» между источниками, Redis SET dedup:urls, url_hash — SHA-256 от сериализации (source, external_id), ФТ-1.5: Redis-дедупликация Collector, ФТ-3.5: Журнал дедупликации уведомлений (user_id, listing_id)

### Community 30 - "Notifier SQL Schema"
Cohesion: 0.36
Nodes (7): notifications_log_deferred_idx, notifications_log_rate_idx, notifier.notifications_log, notifier.subscriptions, notifier.users, subscriptions_chat_id_idx, notifications_log_available_idx

### Community 31 - "Python Dev Tooling"
Cohesion: 0.40
Nodes (4): python-ci workflow (ruff and tests), httpx2, pytest, ruff 0.17.3

### Community 32 - "Redis Compose Service"
Cohesion: 0.33
Nodes (6): command, image, ports, restart, volumes, redis

### Community 34 - "Project Documentation"
Cohesion: 0.50
Nodes (3): docs/contracts.md (inter-service contracts), MIT License, Real_Estate_Pro_TZ.pdf (requirements spec)

### Community 49 - "context.Context"
Cohesion: 0.12
Nodes (5): errStore, recordingStore, Memory, Store, userGateStore

### Community 50 - "ListingCreate"
Cohesion: 0.07
Nodes (26): build_listing_event(), InMemoryListingStore, OutboxEvent, ListingCreate, CreateListingResult, ListingStore, StoredListing, create_listing() (+18 more)

### Community 51 - "postgres_test.go"
Cohesion: 0.14
Nodes (31): baseEvent(), TestMatchAll(), TestRecipientsDedupsChats(), TestSubscriptionMatches(), Int64Ptr(), IntPtr(), applyMigration(), assertSameInt() (+23 more)

### Community 52 - "with"
Cohesion: 0.08
Nodes (24): runs-on, steps, concurrency, cancel-in-progress, group, jobs, build-and-deploy, name (+16 more)

### Community 53 - "postgres"
Cohesion: 0.08
Nodes (24): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, jobs, notifier-sql, name, runs-on, services (+16 more)

### Community 54 - "test_migrations.py"
Cohesion: 0.16
Nodes (9): _alembic(), _app_dsn(), _components(), _dsn(), _head_revision(), _lifecycle(), test_migrations_full_cycle(), _verify_schema() (+1 more)

### Community 55 - "Dispatcher"
Cohesion: 0.17
Nodes (7): Journal, Limiter, logRateLimited(), Dispatcher, journalKey, mockLimiter, Sender

### Community 56 - "Subscription"
Cohesion: 0.15
Nodes (8): Describe(), Subscription, MatchAll(), sameText(), collectSubscriptions(), derefStr(), scanSubscription(), failingStore

### Community 57 - "health.py"
Cohesion: 0.27
Nodes (11): check_dependencies(), health_summary(), live(), DependencyState, HealthChecks, HealthReport, LiveReport, ReadinessError (+3 more)

### Community 58 - "DeferredJob"
Cohesion: 0.24
Nodes (6): DeferredQueue, jobKey(), DeferredJob, MemoryQueue, mockQueue, PgQueue

### Community 59 - "steps"
Cohesion: 0.12
Nodes (15): runs-on, steps, jobs, check, name, on, pull_request, push (+7 more)

### Community 60 - "Event"
Cohesion: 0.23
Nodes (3): Event, Recipients(), MemoryJournal

### Community 61 - "asyncio"
Cohesion: 0.17
Nodes (4): do_run_migrations(), run_async_migrations(), run_migrations_offline(), run_migrations_online()

### Community 62 - "main.py"
Cohesion: 0.22
Nodes (5): create_db_engine(), setup_logging(), lifespan(), root(), create_redis_client()

### Community 63 - "Таблицы"
Cohesion: 0.13
Nodes (14): cities (ТЗ 3.7: уникальность города внутри страны), city_name_aliases (ТЗ 4.2: «таблица либо поле по решению Ильи» — выбрана таблица), countries (ТЗ 3.7), currencies (ТЗ 3.2: исходные курсы задаются миграциями), listing_duplicates (ТЗ 3.3: связь «возможный дубль», обе записи сохраняются), listing_photos (решение: отдельная таблица, не массив/JSONB), listings (ТЗ 3.1 + 3.3), outbox_events (ТЗ 4.3: transactional outbox, объявление + задание в одной транзакции) (+6 more)

### Community 64 - "sync.Mutex"
Cohesion: 0.13
Nodes (5): blockingSender, hangingSender, mockJournal, serialLimiter, slowSender

### Community 65 - "runConsumer"
Cohesion: 0.22
Nodes (12): NewConsumer(), runConsumer(), TestConsumerDeadLetterOnBadPayload(), TestConsumerDeadLetterOnMissingData(), TestConsumerDeliversAndAcks(), TestConsumerGroupCreateFailureIsFatal(), TestConsumerKeepsPendingWhenDeadLetterFails(), TestConsumerKeepsPendingWhenHandlerFails() (+4 more)

### Community 66 - "steps"
Cohesion: 0.26
Nodes (12): steps, steps, notifier-pg-tests, runs-on, steps, steps, name, run (+4 more)

### Community 68 - "test_listing_filters_http.py"
Cohesion: 0.29
Nodes (6): client(), test_http_filters_defaults(), test_http_filters_explain_invalid_combinations(), test_http_filters_openapi_query_parameters(), test_http_filters_parse_query_parameters(), test_http_filters_return_validation_error()

### Community 69 - "Резервное копирование и проверка восстановления"
Cohesion: 0.18
Nodes (7): Дамп, Проверка восстановления (обязательно для каждого дампа), Расписание и хранение, Резервное копирование и проверка восстановления, Что покрывается, backup.sh script, restore-verify.sh script

### Community 70 - "Быстрый старт по схеме core — для разработчиков"
Cohesion: 0.18
Nodes (10): Outbox (ТЗ 4.3), Быстрый старт по схеме core — для разработчиков, Дедупликация (ТЗ 3.3) — контракт для реализации, Деньги (ТЗ 3.2), Идентичности и массовая вставка (важно для дампа), Карта таблиц, Подъём базы, Роли (миграция 0002) (+2 more)

### Community 71 - "ci.yml"
Cohesion: 0.20
Nodes (9): concurrency, cancel-in-progress, group, name, on, pull_request, push, branches (+1 more)

### Community 72 - "postgres"
Cohesion: 0.25
Nodes (8): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, env, image, options, ports, postgres

### Community 73 - "reaper_test.go"
Cohesion: 0.50
Nodes (6): NewReaper(), TestReaperDeletesOnlyConfirmedBeforeMinPending(), TestReaperEmptyPendingUsesLastDelivered(), TestReaperNothingToCleanWhenGroupFresh(), TestReaperRaceNewPendingBetweenReads(), TestReaperSkipsAllWhenPendingAtHead()

### Community 77 - "Бенчмарк доставки notifier (НФТ-11)"
Cohesion: 0.33
Nodes (5): Бенчмарк доставки notifier (НФТ-11), Ограничения, Переменные окружения, Предусловия, Что делает скрипт

### Community 78 - "redis"
Cohesion: 0.40
Nodes (5): services, image, options, ports, redis

### Community 79 - "env"
Cohesion: 0.50
Nodes (4): GITHUB_TOKEN, NOTIFIER_TEST_DSN, NOTIFIER_TEST_REDIS_ADDR, env

## Knowledge Gaps
- **62 isolated node(s):** `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов`, `ФТ-1.7: Веб-интерфейс Jinja2 + HTMX`, `Consumer group notifier (XREADGROUP, XAUTOCLAIM, XACK)` (+57 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 337 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **25 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `fakeRedis` connect `fakeRedis` to `main.go`, `runConsumer`, `sync.Mutex`, `Reaper`, `Stream Read Calls`, `reaper_test.go`, `.XRange`, `.XAutoClaim`?**
  _High betweenness centrality (0.006) - this node is a cross-community bridge._
- **Are the 12 inferred relationships involving `testEvent()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`testEvent()` has 12 INFERRED edges - model-reasoned connections that need verification._
- **What connects `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов` to the rest of the system?**
  _62 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `main.go` be split into smaller, more focused modules?**
  _Cohesion score 0.05140131186642814 - nodes in this community are weakly interconnected._
- **Why does `Client` connect `Client` to `main.go`, `Telegram API Client`, `sync.Mutex`?**
  _High betweenness centrality (0.005) - this node is a cross-community bridge._
- **Are the 29 inferred relationships involving `NewDispatcher()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`NewDispatcher()` has 29 INFERRED edges - model-reasoned connections that need verification._
- **Should `Core API Config & Filters` be split into smaller, more focused modules?**
  _Cohesion score 0.10685249709639953 - nodes in this community are weakly interconnected._