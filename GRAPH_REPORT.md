# Graph Report - real-estate-pro  (2026-11-26)

## Corpus Check
- 118 files · ~82,394 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 13 file(s) not represented in the graph (top: (none) 10, .example 1, .ini 1)

## Summary
- 1617 nodes · 3923 edges · 129 communities (98 shown, 31 thin omitted)
- Extraction: 85% EXTRACTED · 15% INFERRED · 0% AMBIGUOUS · INFERRED: 595 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `b496afb5`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- model_test.go
- ListingFilters
- testing.T
- test_health.py
- error_handlers.py
- Notifier CI Workflow
- steps
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
- notifier
- Redis
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
- fakeRedis
- postgres_test.go
- main.py
- context.Context
- in_memory_listing_store.py
- test_postgres_listing_store.py
- main.go
- test_listing_store.py
- with
- test_listings_http.py
- ListingStore
- Event
- health.py
- postgres
- sync.Mutex
- parse_alter_table
- conftest.py
- Settings
- test_reference_data.py
- reference_data.py
- DeferredJob
- 2. Сценарии
- bot.go
- dicts/client_test.go
- gen-analysis-data.py
- sql-to-er.py
- Dispatcher
- ListingCreate
- migrate.go
- consumer_test.go
- listing_body_limit.py
- env.py
- Reaper
- test_listing_filters_http.py
- Анализ запросов и индексов схемы core (НФТ-10, ТЗ 6.1)
- steps
- env
- pytest
- smoke-flow.sh
- Таблицы
- Быстрый старт по схеме core — для разработчиков
- ci.yml
- notifier-p95.sh — p95 доставки уведомлений (НФТ-11)
- db-backup-restore.md
- CICD.md
- docs-links.yml
- telegram/client_test.go
- FormatEvent
- reaper_test.go
- core-api
- core-migrations
- Отчёт приёмочного замера НФТ-11 (p95 задержки доставки)
- on
- on
- on
- asyncpg
- collector
- Проект схемы `core`
- core-api
- Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой)
- .XRange
- Feature branch workflow
- postgres
- Резервное копирование и проверка восстановления
- python-ci.yml
- jobs
- strategy
- docs/README.md
- ER-диаграмма схемы core
- syncBuf
- .Validate
- notifier-p95.sh
- ui-headless.sh
- scripts/tools — инструменты-заделы
- coreapi-p95.sh
- pgbench-run.sh

## God Nodes (most connected - your core abstractions)
1. `testEvent()` - 46 edges
2. `ListingFilters` - 42 edges
3. `NewDispatcher()` - 37 edges
4. `NewMemory()` - 35 edges
5. `ListingCreate` - 30 edges
6. `Event` - 29 edges
7. `InMemoryListingStore` - 26 edges
8. `mustAddUser()` - 25 edges
9. `Subscription` - 24 edges
10. `Client` - 23 edges

## Surprising Connections (you probably didn't know these)
- `Дедупликация (ТЗ 3.3) — контракт для реализации` --references--> `content_hash()`  [INFERRED]
  docs/db-quickstart-for-devs.md → scripts/db/gen-analysis-data.py
- `Запросы (по contracts.md) и результаты` --references--> `content_hash()`  [INFERRED]
  docs/query-analysis.md → scripts/db/gen-analysis-data.py
- `Методика` --references--> `content_hash()`  [INFERRED]
  docs/query-analysis.md → scripts/db/gen-analysis-data.py
- `Наблюдение: диспетчер читает все подписки на каждое событие` --references--> `MatchAll()`  [INFERRED]
  docs/query-analysis.md → notifier/internal/model/subscription.go
- `S3. Полный поток (ТЗ 4.4)` --references--> `content_hash()`  [INFERRED]
  docs/qa/e2e-protocol.md → scripts/db/gen-analysis-data.py

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Поток данных листинга: Collector → Core API → outbox → Redis Stream → Notifier → Telegram** — docs_real_estate_pro_tz_collector, docs_contracts_post_api_v1_listings, docs_real_estate_pro_tz_transactional_outbox, docs_contracts_listings_new, docs_real_estate_pro_tz_notifier, docs_real_estate_pro_tz_telegram [EXTRACTED 1.00]
- **Listing delivery pipeline (collector -> core-api -> listings.new -> notifier -> Telegram)** — readme_collector, readme_core_api, readme_listings_new, readme_notifier, readme_telegram_bot_api [EXTRACTED 1.00]
- **Mock data ecosystem (adapters, modes, RawListing dump)** — readme_collector, readme_avito_api_adapter, readme_mock_adapter, readme_mock_modes, readme_rawlisting [EXTRACTED 1.00]
- **Надёжная доставка событий: outbox, pending/XAUTOCLAIM, DLQ, AOF, лимит уведомлений** — docs_real_estate_pro_tz_transactional_outbox, docs_real_estate_pro_tz_pending_ownership, docs_contracts_listings_dead, docs_real_estate_pro_tz_aof_durability, docs_contracts_notification_limit [EXTRACTED 1.00]
- **Required CI checks for merge (require-ci ruleset)** — contributing_ci_checks, contributing_require_ci, contributing_gitleaks, contributing_python_ci [EXTRACTED 1.00]
- **Многоуровневая дедупликация: Redis-хэш Collector → уникальность PostgreSQL → журнал уведомлений notifier** — docs_contracts_dedup_source_external_id, docs_real_estate_pro_tz_url_hash, docs_real_estate_pro_tz_dedup_urls, docs_real_estate_pro_tz_фт_3_5, docs_real_estate_pro_tz_exactly_once [INFERRED 0.85]

## Communities (129 total, 31 thin omitted)

### Community 0 - "model_test.go"
Cohesion: 0.16
Nodes (20): Наблюдение: диспетчер читает все подписки на каждое событие, ParseSubscription(), TestParseSubscription(), TestParseSubscriptionCityWithSpace(), TestParseSubscriptionErrors(), TestParseSubscriptionStudio(), int64OrZero(), ParseEvent() (+12 more)

### Community 1 - "ListingFilters"
Cohesion: 0.22
Nodes (18): ListingFilters, test_listing_filters_accept_nonempty_text(), test_listing_filters_accept_valid_bounds(), test_listing_filters_accept_valid_choices(), test_listing_filters_accept_valid_country(), test_listing_filters_accept_valid_location(), test_listing_filters_accept_valid_pagination(), test_listing_filters_accept_valid_ranges() (+10 more)

### Community 2 - "testing.T"
Cohesion: 0.08
Nodes (102): New(), newTestBot(), TestStartEnsuresUser(), TestStartSubscribeStopStartScenario(), TestStopDeletesUserAndReportsCount(), TestStopDropsDeferredJobs(), TestStopRemovesAllSubscriptions(), TestStopWithoutCleanerStillWorks() (+94 more)

### Community 3 - "test_health.py"
Cohesion: 0.09
Nodes (12): check_redis(), health_client(), test_dependency_check_timeout(), scenario(), hanging_operation(), test_health_summary(), test_probe_external_cancellation_propagates(), scenario() (+4 more)

### Community 4 - "error_handlers.py"
Cohesion: 0.14
Nodes (5): http_error_handler(), internal_error_handler(), register_error_handlers(), validation_error_handler(), client()

### Community 5 - "Notifier CI Workflow"
Cohesion: 0.06
Nodes (40): run, runs-on, steps, GITHUB_TOKEN, NOTIFIER_TEST_DSN, NOTIFIER_TEST_REDIS_ADDR, POSTGRES_DB, POSTGRES_PASSWORD (+32 more)

### Community 6 - "steps"
Cohesion: 0.46
Nodes (8): steps, steps, steps, name, run, uses, with, python-version

### Community 7 - "Bot"
Cohesion: 0.15
Nodes (6): Bot, ChatCleaner, stubValidator, Validator, Describe(), ChatCleaner

### Community 8 - "Stream Consumer & Claim"
Cohesion: 0.23
Nodes (6): isBusyGroup(), NewConsumer(), streamLenWarnState(), TestStreamLenWarnStateHysteresis(), Consumer, Handler

### Community 9 - "Telegram API Client"
Cohesion: 0.19
Nodes (6): API, fakeAPI, NewClient(), Client, Message, Update

### Community 10 - "Dependabot Config"
Cohesion: 0.14
Nodes (14): prefix, patterns, patterns, docker, github-actions, day, interval, updates (+6 more)

### Community 11 - "Compose Environment Vars"
Cohesion: 0.21
Nodes (17): environment, environment, environment, ADMIN_KEY, CORE_API_LOG_LEVEL, CORE_API_URL, MOCK_MODE, POSTGRES_DB (+9 more)

### Community 12 - "Collector Requirements (TZ)"
Cohesion: 0.15
Nodes (15): POST /collect/run (ручной импорт), MOCK_MODE и MOCK_PUBLISH_MODE (batch/stream), RawListing — общий внутренний формат Collector, 3.4 Mock и источники, 4.5 Устройство Collector (адаптеры, нормализатор, batch/stream), 4.6 Необязательный mock-сервер источника, collector (httpx, Jinja2, HTMX, порт 8001), Синтетический дамп RawListing (≥500 объектов, ≥5 стран, ≥10 городов) (+7 more)

### Community 13 - "Event Delivery & Notification Rules"
Cohesion: 0.18
Nodes (14): Consumer group notifier (XREADGROUP, XAUTOCLAIM, XACK), listings.dead — некорректный payload (DLQ), Событие listings.new (Core API → Redis Streams → Notifier), Лимит 30 уведомлений за скользящий час, POST /api/v1/listings (Collector → Core API), 3.6 Параметры (TTL, лимиты, интервалы, часовой пояс), 4.3 Redis Streams и outbox (публикация, чтение, подтверждение, Telegram, хранение), 4.4 Поток данных (Collector → core → XADD → notifier → Telegram) (+6 more)

### Community 14 - "Team Process & Migrations"
Cohesion: 0.36
Nodes (8): Alembic migrations (schema core), Secrets via .env, SQL migrations (schema notifier), Pull request template, PR checklist (branch, contracts, migrations, schema isolation, secrets), POST /api/v1/listings, PostgreSQL (schemas core and notifier, port 5432), RawListing (internal data format)

### Community 15 - "Compose Logging"
Cohesion: 0.33
Nodes (9): logging, driver, options, logging, max-file, max-size, logging, logging (+1 more)

### Community 16 - "Core API Listings (TZ)"
Cohesion: 0.15
Nodes (14): GET /api/v1/listings (фильтры, сортировка, пагинация), GET /api/v1/listings/{id} (карточка), 3.5 Кэш (ключ с хэшем фильтров, TTL 600 с), 6.5 Границы ответственности команды, cache:version и инвалидация кэша, core-api (FastAPI, порт 8000), НФТ-10: p95 GET списка <300 мс с кэшем, <500 мс без, НФТ-12: Ошибки JSON API {error, message} (+6 more)

### Community 17 - "Client"
Cohesion: 0.11
Nodes (19): City, Client, Country, eventJSON, healthProber, fresh(), matchCity(), newMux() (+11 more)

### Community 18 - "Compose Infrastructure"
Cohesion: 0.15
Nodes (12): services, volumes, pgdata, redisdata, x-core-postgres, POSTGRES_DB, POSTGRES_HOST, POSTGRES_PASSWORD (+4 more)

### Community 19 - "Notifier Requirements (TZ)"
Cohesion: 0.17
Nodes (12): 3.1 Объявление и валидация, 4.2 Данные PostgreSQL (схемы core и notifier), notifier (Go, go-redis, Telegram Bot API, порт 8080), PostgreSQL (схемы core и notifier, порт 5432), Redis (кэш, SET, Streams, порт 6379), Telegram Bot API (канал доставки уведомлений), НФТ-5: Volumes для PostgreSQL и Redis, НФТ-6: Миграции (Alembic core, SQL notifier) до готовности сервисов (+4 more)

### Community 20 - "Team Ownership"
Cohesion: 0.22
Nodes (9): AvitoApiAdapter, POST /collect/run (background batch collection), Collector (Python, httpx, Jinja2, HTMX, port 8001), Daniil Matveev (Go notifier, Telegram, Compose), Dima Budaev (UI, mock data, manual testing), Ilya Golubev (PostgreSQL schemas, migrations), MockAdapter, Mock publish modes (batch / stream) (+1 more)

### Community 21 - "Core API Dependencies"
Cohesion: 0.18
Nodes (10): anyio 4.15.1, asyncpg 0.32.0, FastAPI 0.162.0, opentelemetry-api 1.45.1, pydantic 2.14.1, pydantic-settings 2.16.1, python-dotenv 1.2.4, SQLAlchemy[asyncio] 2.1.6 (+2 more)

### Community 23 - "Compose Service Ordering"
Cohesion: 0.29
Nodes (7): redis, build, depends_on, ports, restart, condition, notifier

### Community 24 - "Reference Data (TZ)"
Cohesion: 0.24
Nodes (10): GET /api/v1/cities?country=BY, GET /api/v1/countries, GET /api/v1/currencies, GET /api/v1/sources, Курс rate_to_usd и округление ROUND_HALF_UP до целого USD, 3.2 Валюты и время (Decimal, NUMERIC, UTC/Europe/Minsk), 3.7 Справочники стран и городов, ФТ-1.3: Нормализация (USD, HTML, комнаты, справочники) (+2 more)

### Community 25 - "Core API Concepts"
Cohesion: 0.20
Nodes (9): content_hash (cross-source duplicate linking), Core API (Python, FastAPI, SQLAlchemy, port 8000), listings.dead (dead-letter stream), POST /debug/event (X-Admin-Key protected demo event), GET /api/v1/listings (search, sorting, pagination), Listing cache (TTL 600s, cache:version, stale-after-Redis-failure), GET /api/v1/listings/{id} (listing card), listings.new event (Redis Streams) (+1 more)

### Community 26 - "Notifier Concepts"
Cohesion: 0.25
Nodes (9): redis (redis-py) 8.1.0, Consumer group delivery (XREADGROUP, XACK, XAUTOCLAIM), Docker Compose orchestration, GET /health (notifier health check), Notifier (Go, go-redis, Telegram Bot API, port 8080), Rate limits and deferral (10 subscriptions, 30 notifications/hour, defer to PostgreSQL), Redis 7+ (cache, dedup, Streams, port 6379), Telegram Bot API (+1 more)

### Community 27 - "Compose Healthchecks"
Cohesion: 0.50
Nodes (9): healthcheck, interval, retries, start_period, test, timeout, healthcheck, healthcheck (+1 more)

### Community 28 - "Collector Compose Service"
Cohesion: 0.25
Nodes (8): build, depends_on, logging, ports, restart, condition, core-api, collector

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

### Community 34 - "notifier"
Cohesion: 0.15
Nodes (11): HTTP, notifier, Запуск, Компоненты (`internal/`), Миграции, Переменные окружения, Статус, Тесты (+3 more)

### Community 50 - "postgres_test.go"
Cohesion: 0.10
Nodes (33): Subscription, TestMain(), sameText(), applyMigration(), TestMain(), collectSubscriptions(), derefStr(), scanSubscription() (+25 more)

### Community 51 - "main.py"
Cohesion: 0.11
Nodes (14): setup_logging(), lifespan(), root(), OutboxStore, publish_listing_event(), run_outbox_worker(), create_redis_client(), anyio_backend() (+6 more)

### Community 52 - "context.Context"
Cohesion: 0.11
Nodes (6): errStore, recordingStore, failingStore, userGateStore, Memory, Store

### Community 53 - "in_memory_listing_store.py"
Cohesion: 0.15
Nodes (10): _in_range(), _matches_filters(), build_listing_event(), OutboxEvent, ListingFields, CreateListingResult, ListingSearchResult, StoredListing (+2 more)

### Community 54 - "test_postgres_listing_store.py"
Cohesion: 0.16
Nodes (18): _get_listing(), PostgresListingStore, anyio_backend(), listing_data(), outbox_listing(), pg_engine(), _read_outbox_event(), search_store() (+10 more)

### Community 55 - "main.go"
Cohesion: 0.12
Nodes (17): checkResult, Connect(), dsnFromEnv(), quoteValue(), TestDsnFromEnv(), ConsumerName(), DefaultLenWarnThreshold(), DefaultWorkers() (+9 more)

### Community 56 - "test_listing_store.py"
Cohesion: 0.22
Nodes (13): InMemoryListingStore, anyio_backend(), listing(), test_concurrent_duplicates_create_one_listing_and_event(), test_create_saves_listing_and_event(), test_duplicate_preserves_original_listing_and_event(), test_event_build_failure_does_not_save_listing(), test_failed_publication_stays_pending_until_successful_retry() (+5 more)

### Community 57 - "with"
Cohesion: 0.08
Nodes (24): runs-on, steps, concurrency, cancel-in-progress, group, jobs, build-and-deploy, name (+16 more)

### Community 58 - "test_listings_http.py"
Cohesion: 0.10
Nodes (8): UnknownListingReferenceError, client(), payload(), redis_client(), test_create_returns_422_for_unknown_reference(), test_created_listing_is_published_by_worker(), test_list_listings_filters_by_area(), test_list_listings_sorts_by_created_at()

### Community 59 - "ListingStore"
Cohesion: 0.13
Nodes (8): ListingStore, create_listing(), get_listing(), get_listing_store(), list_listings(), _listing_data(), ListingCreateResponse, ListingPage

### Community 61 - "Event"
Cohesion: 0.17
Nodes (5): Event, Recipients(), Journal, MemoryJournal, mockJournal

### Community 62 - "health.py"
Cohesion: 0.24
Nodes (12): check_dependencies(), check_postgres(), health_summary(), live(), DependencyState, HealthChecks, HealthReport, LiveReport (+4 more)

### Community 63 - "postgres"
Cohesion: 0.11
Nodes (19): runs-on, services, runs-on, services, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, jobs (+11 more)

### Community 64 - "sync.Mutex"
Cohesion: 0.11
Nodes (7): blockingSender, hangingSender, limiterKey, MemoryLimiter, mockLimiter, serialLimiter, slowSender

### Community 65 - "parse_alter_table"
Cohesion: 0.22
Nodes (10): add_foreign_key(), entity_name(), parse_alter_table(), parse_column(), parse_create_table(), parse_references(), parse_table_constraint(), sane_type() (+2 more)

### Community 66 - "conftest.py"
Cohesion: 0.12
Nodes (4): run_alembic(), temporary_database(), main(), block()

### Community 67 - "Settings"
Cohesion: 0.18
Nodes (10): Settings, clean_settings_env(), test_database_url_accepts_asyncpg(), test_database_url_built_from_parts_escapes_special_characters(), test_database_url_rejects_wrong_driver(), test_database_url_takes_priority_over_parts(), test_database_url_validation_error_hides_password(), test_environment_overrides_dotenv() (+2 more)

### Community 68 - "test_reference_data.py"
Cohesion: 0.15
Nodes (4): upgrade(), create_db_engine(), anyio_backend(), references()

### Community 69 - "reference_data.py"
Cohesion: 0.21
Nodes (8): cities(), City, countries(), currencies(), Currency, NamedEntry, _rows(), sources()

### Community 70 - "DeferredJob"
Cohesion: 0.24
Nodes (6): jobKey(), DeferredJob, DeferredQueue, MemoryQueue, mockQueue, PgQueue

### Community 71 - "2. Сценарии"
Cohesion: 0.12
Nodes (15): 0. Когда прогон возможен, 1. Предусловия, 2. Сценарии, 3. Критерии закрытия issue #20, 4. Инструменты, S10. Безопасность и ошибки (НФТ-1, 2, 3, 12), S1. Стартовая конфигурация, S2. Прямая публикация (без collector, доступен раньше остальных) (+7 more)

### Community 72 - "bot.go"
Cohesion: 0.21
Nodes (3): retryAfterOf(), apiResponse, apiResponseParameters

### Community 73 - "dicts/client_test.go"
Cohesion: 0.21
Nodes (11): New(), newServer(), TestCachedHealthy(), TestCacheServesRepeatedCalls(), TestHealthy(), TestStaleCacheWhenCoreAPIDown(), TestUnavailableWithoutCache(), TestValidateCanonicalizesCountryAndCity() (+3 more)

### Community 74 - "gen-analysis-data.py"
Cohesion: 0.25
Nodes (5): S3. Полный поток (ТЗ 4.4), content_hash(), main(), _event_payload(), main()

### Community 75 - "sql-to-er.py"
Cohesion: 0.22
Nodes (7): main(), Model, parse_sql_text(), render(), split_statements(), split_top_level(), strip_comments()

### Community 76 - "Dispatcher"
Cohesion: 0.22
Nodes (5): logRateLimited(), Dispatcher, journalKey, Sender, RateLimitError

### Community 77 - "ListingCreate"
Cohesion: 0.30
Nodes (10): ListingCreate, payload(), test_invalid_value_is_rejected(), test_listing_photo_and_url_limits(), test_listing_text_limits(), test_minimal_listing_from_json(), test_optional_fields_accept_null(), test_photos_preserve_order_in_json() (+2 more)

### Community 78 - "migrate.go"
Cohesion: 0.30
Nodes (8): applied(), Apply(), migrationNames(), TestApplyOnEmptySchema(), TestApplyParallel(), TestVersionOfAndSort(), versionOf(), FS()

### Community 79 - "consumer_test.go"
Cohesion: 0.31
Nodes (11): runConsumer(), TestConsumerDeadLetterOnBadPayload(), TestConsumerDeadLetterOnMissingData(), TestConsumerDeliversAndAcks(), TestConsumerGroupCreateFailureIsFatal(), TestConsumerKeepsPendingWhenDeadLetterFails(), TestConsumerKeepsPendingWhenHandlerFails(), TestConsumerStaysSilentBelowStreamLenThreshold() (+3 more)

### Community 81 - "env.py"
Cohesion: 0.21
Nodes (4): do_run_migrations(), run_async_migrations(), run_migrations_offline(), run_migrations_online()

### Community 83 - "test_listing_filters_http.py"
Cohesion: 0.29
Nodes (6): client(), test_http_filters_defaults(), test_http_filters_explain_invalid_combinations(), test_http_filters_openapi_query_parameters(), test_http_filters_parse_query_parameters(), test_http_filters_return_validation_error()

### Community 84 - "Анализ запросов и индексов схемы core (НФТ-10, ТЗ 6.1)"
Cohesion: 0.18
Nodes (10): Анализ запросов и индексов схемы core (НФТ-10, ТЗ 6.1), Запросы (по contracts.md) и результаты, Как воспроизвести, Как воспроизвести, Методика, Наблюдение: available_at, Наблюдения (решить после реальных запросов, не менять молча), ПопBatch: была находка — уже внедрена (PR #72) (+2 more)

### Community 85 - "steps"
Cohesion: 0.27
Nodes (11): steps, steps, MIGRATION_TEST_DSN, steps, env, name, run, uses (+3 more)

### Community 86 - "env"
Cohesion: 0.22
Nodes (11): CORE_API_LOG_LEVEL, DATABASE_URL, MIGRATION_TEST_DSN, POSTGRES_DB, POSTGRES_HOST, POSTGRES_PASSWORD, POSTGRES_PORT, POSTGRES_USER (+3 more)

### Community 87 - "pytest"
Cohesion: 0.18
Nodes (11): pytest, image, options, ports, runs-on, services, image, options (+3 more)

### Community 88 - "smoke-flow.sh"
Cohesion: 0.40
Nodes (10): bad(), cleanup(), ok(), poll_count(), psql_q(), redis_cli(), redis_xlen(), smoke-flow.sh script (+2 more)

### Community 89 - "Таблицы"
Cohesion: 0.20
Nodes (10): cities (ТЗ 3.7: уникальность города внутри страны), city_name_aliases (ТЗ 4.2: «таблица либо поле по решению Ильи» — выбрана таблица), countries (ТЗ 3.7), currencies (ТЗ 3.2: исходные курсы задаются миграциями), listing_duplicates (ТЗ 3.3: связь «возможный дубль», обе записи сохраняются), listing_photos (решение: отдельная таблица, не массив/JSONB), listings (ТЗ 3.1 + 3.3), outbox_events (ТЗ 4.3: transactional outbox, объявление + задание в одной транзакции) (+2 more)

### Community 90 - "Быстрый старт по схеме core — для разработчиков"
Cohesion: 0.20
Nodes (10): Outbox (ТЗ 4.3), Быстрый старт по схеме core — для разработчиков, Дедупликация (ТЗ 3.3) — контракт для реализации, Деньги (ТЗ 3.2), Идентичности и массовая вставка (важно для дампа), Карта таблиц, Подъём базы, Роли (миграция 0002) (+2 more)

### Community 91 - "ci.yml"
Cohesion: 0.20
Nodes (9): concurrency, cancel-in-progress, group, name, on, pull_request, push, branches (+1 more)

### Community 92 - "notifier-p95.sh — p95 доставки уведомлений (НФТ-11)"
Cohesion: 0.20
Nodes (9): coreapi-p95.sh — p95 времени GET списка (НФТ-10), notifier-p95.sh — p95 доставки уведомлений (НФТ-11), scripts/bench — бенчмарки, Замер «без кэша» (< 500 мс) — вручную, Ограничения, Переменные окружения, Предусловия, Что делает скрипт (+1 more)

### Community 93 - "db-backup-restore.md"
Cohesion: 0.28
Nodes (4): backup.sh script, restore-verify.sh script, scripts, Планируется (набросок)

### Community 94 - "CICD.md"
Cohesion: 0.22
Nodes (6): .github, Workflows ([workflows/](workflows/)), Заметки, Скрипты и конфиги, version, check-md-links.sh script

### Community 95 - "docs-links.yml"
Cohesion: 0.22
Nodes (8): runs-on, steps, jobs, check, name, name, run, uses

### Community 96 - "telegram/client_test.go"
Cohesion: 0.33
Nodes (5): newTestClient(), TestOtherAPIErrorsNotRateLimited(), TestRateLimitedCarriesRetryAfter(), TestRateLimitedWithoutParametersUsesDefault(), TestSendMessageOK()

### Community 97 - "FormatEvent"
Cohesion: 0.25
Nodes (8): FormatEvent(), TestFormatEventGiantCurrencyUnconditionalTier(), TestFormatEventNewContract(), TestFormatEventNilAddressUnchanged(), TestFormatEventShortEventUnchanged(), TestFormatEventTruncatesGiantURL(), TestFormatEventTruncatesLongAddress(), truncateRunes()

### Community 98 - "reaper_test.go"
Cohesion: 0.50
Nodes (6): NewReaper(), TestReaperDeletesOnlyConfirmedBeforeMinPending(), TestReaperEmptyPendingUsesLastDelivered(), TestReaperNothingToCleanWhenGroupFresh(), TestReaperRaceNewPendingBetweenReads(), TestReaperSkipsAllWhenPendingAtHead()

### Community 99 - "core-api"
Cohesion: 0.29
Nodes (7): build, depends_on, ports, restart, condition, core-migrations, core-api

### Community 100 - "core-migrations"
Cohesion: 0.29
Nodes (7): build, command, depends_on, restart, postgres, condition, core-migrations

### Community 101 - "Отчёт приёмочного замера НФТ-11 (p95 задержки доставки)"
Cohesion: 0.29
Nodes (6): Вердикт, Границы этого замера, Отклонения в ходе замера (зафиксировано для честности), Отчёт приёмочного замера НФТ-11 (p95 задержки доставки), Результат, Условия замера (НФТ-10: «фиксируются машина, состав данных и фильтры»)

### Community 102 - "on"
Cohesion: 0.29
Nodes (7): on, pull_request, push, workflow_dispatch, paths, branches, paths

### Community 103 - "on"
Cohesion: 0.29
Nodes (7): on, pull_request, push, workflow_dispatch, paths, branches, paths

### Community 104 - "on"
Cohesion: 0.29
Nodes (7): on, pull_request, push, workflow_dispatch, paths, branches, paths

### Community 105 - "asyncpg"
Cohesion: 0.60
Nodes (3): _lifecycle(), test_migrations_full_cycle(), _verify_schema()

### Community 106 - "collector"
Cohesion: 0.33
Nodes (5): collector, Запуск, Планируется (набросок, по ТЗ), Статус, Что есть сейчас

### Community 107 - "Проект схемы `core`"
Cohesion: 0.33
Nodes (4): Индексы (ТЗ 4.2 + ревью PR #66), План реализации (после согласования), Проект схемы `core`, Решения по ревью PR #66 (2026-11-15)

### Community 108 - "core-api"
Cohesion: 0.33
Nodes (6): core-api, Запуск, Переменные окружения, Планируется (набросок, по ТЗ и плану Тимура), Тесты, Что есть сейчас

### Community 109 - "Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой)"
Cohesion: 0.33
Nodes (5): Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой), До демонстрации (подготовка, ~10 минут), Запасные ответы на вопросы комиссии, Кто что показывает, Порядок показа

### Community 111 - "Feature branch workflow"
Cohesion: 0.40
Nodes (3): CI required checks (notifier lint/test, gitleaks, docker compose), gitleaks (secret leak detection), require-ci ruleset

### Community 112 - "postgres"
Cohesion: 0.40
Nodes (5): image, ports, restart, volumes, postgres

### Community 113 - "Резервное копирование и проверка восстановления"
Cohesion: 0.40
Nodes (5): Дамп, Проверка восстановления (обязательно для каждого дампа), Расписание и хранение, Резервное копирование и проверка восстановления, Что покрывается

### Community 114 - "python-ci.yml"
Cohesion: 0.40
Nodes (4): concurrency, cancel-in-progress, group, name

### Community 115 - "jobs"
Cohesion: 0.40
Nodes (5): jobs, openapi-gate, ruff, runs-on, runs-on

### Community 116 - "strategy"
Cohesion: 0.50
Nodes (5): service, strategy, strategy, fail-fast, matrix

### Community 118 - "ER-диаграмма схемы core"
Cohesion: 0.50
Nodes (3): ER-диаграмма схемы core, Как перегенерировать, Схема notifier

### Community 120 - ".Validate"
Cohesion: 0.50
Nodes (3): isAlphaUpper2(), isHexDigit(), validUUID()

## Knowledge Gaps
- **61 isolated node(s):** `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов`, `ФТ-1.7: Веб-интерфейс Jinja2 + HTMX`, `Consumer group notifier (XREADGROUP, XAUTOCLAIM, XACK)` (+56 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 468 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **31 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `content_hash()` connect `gen-analysis-data.py` to `Быстрый старт по схеме core — для разработчиков`, `Анализ запросов и индексов схемы core (НФТ-10, ТЗ 6.1)`?**
  _High betweenness centrality (0.342) - this node is a cross-community bridge._
- **Are the 12 inferred relationships involving `testEvent()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`testEvent()` has 12 INFERRED edges - model-reasoned connections that need verification._
- **What connects `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов` to the rest of the system?**
  _61 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `testing.T` be split into smaller, more focused modules?**
  _Cohesion score 0.08409785932721713 - nodes in this community are weakly interconnected._
- **Why does `Схема notifier — анализ запросов (дополнение)` connect `Анализ запросов и индексов схемы core (НФТ-10, ТЗ 6.1)` to `model_test.go`?**
  _High betweenness centrality (0.330) - this node is a cross-community bridge._
- **Are the 7 inferred relationships involving `ListingFilters` (e.g. with `InMemoryListingStore` and `_matches_filters()`) actually correct?**
  _`ListingFilters` has 7 INFERRED edges - model-reasoned connections that need verification._
- **Should `test_health.py` be split into smaller, more focused modules?**
  _Cohesion score 0.08735632183908046 - nodes in this community are weakly interconnected._