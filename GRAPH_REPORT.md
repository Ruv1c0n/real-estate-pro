# Graph Report - real-estate-pro  (2026-11-18)

## Corpus Check
- 107 files · ~76,862 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 13 file(s) not represented in the graph (top: (none) 10, .example 1, .ini 1)

## Summary
- 1475 nodes · 3545 edges · 106 communities (74 shown, 32 thin omitted)
- Extraction: 84% EXTRACTED · 16% INFERRED · 0% AMBIGUOUS · INFERRED: 552 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `a9a77f76`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- main.go
- ListingFilters
- context.Context
- test_health.py
- main.py
- jobs
- env
- testing.T
- Stream Consumer & Claim
- Bot
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
- .Validate
- sql-to-er.py
- postgres_test.go
- Таблицы
- in_memory_listing_store.py
- with
- InMemoryListingStore
- ListingStore
- health.py
- ListingCreate
- Event
- Settings
- DeferredJob
- Dispatcher
- asyncio
- 2. Сценарии
- error_handlers.py
- consumer_test.go
- test_listings_http.py
- postgres
- steps
- python-ci.yml
- Reaper
- fakeRedis
- test_listing_filters_http.py
- smoke-flow.sh
- ci.yml
- notifier-p95.sh — p95 доставки уведомлений (НФТ-11)
- test_error_handlers.py
- .github/README.md
- docs-links.yml
- check_postgres
- postgres
- steps
- reaper_test.go
- Отчёт приёмочного замера НФТ-11 (p95 задержки доставки)
- steps
- on
- 0001_core_schema.py
- collector
- Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой)
- .XRange
- .DropChatJobs
- Feature branch workflow
- redis
- ER-диаграмма схемы core
- env
- env
- syncBuf
- MemoryJournal
- notifier-p95.sh
- ui-headless.sh
- scripts/tools — инструменты-заделы
- coreapi-p95.sh
- pgbench-run.sh

## God Nodes (most connected - your core abstractions)
1. `testEvent()` - 46 edges
2. `NewDispatcher()` - 37 edges
3. `ListingFilters` - 36 edges
4. `NewMemory()` - 35 edges
5. `Event` - 29 edges
6. `mustAddUser()` - 25 edges
7. `Subscription` - 24 edges
8. `Client` - 23 edges
9. `InMemoryListingStore` - 22 edges
10. `resetTestDB()` - 22 edges

## Surprising Connections (you probably didn't know these)
- `Дедупликация (ТЗ 3.3) — контракт для реализации` --references--> `content_hash()`  [INFERRED]
  docs/db-quickstart-for-devs.md → scripts/db/gen-analysis-data.py
- `S3. Полный поток (ТЗ 4.4)` --references--> `content_hash()`  [INFERRED]
  docs/qa/e2e-protocol.md → scripts/db/gen-analysis-data.py
- `Запросы (по contracts.md) и результаты` --references--> `content_hash()`  [INFERRED]
  docs/query-analysis.md → scripts/db/gen-analysis-data.py
- `Методика` --references--> `content_hash()`  [INFERRED]
  docs/query-analysis.md → scripts/db/gen-analysis-data.py
- `Планируется (набросок, по ТЗ и плану Тимура)` --references--> `ListingFilters`  [INFERRED]
  core-api/README.md → core-api/app/listing_filters.py

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Поток данных листинга: Collector → Core API → outbox → Redis Stream → Notifier → Telegram** — docs_real_estate_pro_tz_collector, docs_contracts_post_api_v1_listings, docs_real_estate_pro_tz_transactional_outbox, docs_contracts_listings_new, docs_real_estate_pro_tz_notifier, docs_real_estate_pro_tz_telegram [EXTRACTED 1.00]
- **Listing delivery pipeline (collector -> core-api -> listings.new -> notifier -> Telegram)** — readme_collector, readme_core_api, readme_listings_new, readme_notifier, readme_telegram_bot_api [EXTRACTED 1.00]
- **Mock data ecosystem (adapters, modes, RawListing dump)** — readme_collector, readme_avito_api_adapter, readme_mock_adapter, readme_mock_modes, readme_rawlisting [EXTRACTED 1.00]
- **Надёжная доставка событий: outbox, pending/XAUTOCLAIM, DLQ, AOF, лимит уведомлений** — docs_real_estate_pro_tz_transactional_outbox, docs_real_estate_pro_tz_pending_ownership, docs_contracts_listings_dead, docs_real_estate_pro_tz_aof_durability, docs_contracts_notification_limit [EXTRACTED 1.00]
- **Required CI checks for merge (require-ci ruleset)** — contributing_ci_checks, contributing_require_ci, contributing_gitleaks, contributing_python_ci [EXTRACTED 1.00]
- **Многоуровневая дедупликация: Redis-хэш Collector → уникальность PostgreSQL → журнал уведомлений notifier** — docs_contracts_dedup_source_external_id, docs_real_estate_pro_tz_url_hash, docs_real_estate_pro_tz_dedup_urls, docs_real_estate_pro_tz_фт_3_5, docs_real_estate_pro_tz_exactly_once [INFERRED 0.85]

## Communities (106 total, 32 thin omitted)

### Community 0 - "main.go"
Cohesion: 0.05
Nodes (86): checkResult, FormatEvent(), TestFormatEventGiantCurrencyUnconditionalTier(), TestFormatEventNewContract(), TestFormatEventNilAddressUnchanged(), TestFormatEventShortEventUnchanged(), TestFormatEventTruncatesGiantURL(), TestFormatEventTruncatesLongAddress() (+78 more)

### Community 1 - "ListingFilters"
Cohesion: 0.22
Nodes (18): ListingFilters, test_listing_filters_accept_nonempty_text(), test_listing_filters_accept_valid_bounds(), test_listing_filters_accept_valid_choices(), test_listing_filters_accept_valid_country(), test_listing_filters_accept_valid_location(), test_listing_filters_accept_valid_pagination(), test_listing_filters_accept_valid_ranges() (+10 more)

### Community 2 - "context.Context"
Cohesion: 0.08
Nodes (9): errStore, recordingStore, Describe(), Limiter, Memory, Store, failingStore, mockLimiter (+1 more)

### Community 3 - "test_health.py"
Cohesion: 0.12
Nodes (7): health_client(), test_dependency_check_timeout(), test_health_summary(), test_probe_external_cancellation_propagates(), test_ready(), test_redis_unexpected_exception_is_reported(), clients()

### Community 4 - "main.py"
Cohesion: 0.22
Nodes (5): create_db_engine(), setup_logging(), lifespan(), root(), create_redis_client()

### Community 5 - "jobs"
Cohesion: 0.18
Nodes (11): run, runs-on, runs-on, jobs, docker, gitleaks, notifier, defaults (+3 more)

### Community 6 - "env"
Cohesion: 0.07
Nodes (39): CORE_API_LOG_LEVEL, DATABASE_URL, POSTGRES_DB, POSTGRES_HOST, POSTGRES_PASSWORD, POSTGRES_PORT, POSTGRES_USER, REDIS_URL (+31 more)

### Community 7 - "testing.T"
Cohesion: 0.08
Nodes (80): Validator, New(), newTestBot(), TestStartEnsuresUser(), TestStartSubscribeStopStartScenario(), TestStopDeletesUserAndReportsCount(), TestStopDropsDeferredJobs(), TestStopRemovesAllSubscriptions() (+72 more)

### Community 8 - "Stream Consumer & Claim"
Cohesion: 0.23
Nodes (6): isBusyGroup(), NewConsumer(), streamLenWarnState(), TestStreamLenWarnStateHysteresis(), Consumer, Handler

### Community 9 - "Bot"
Cohesion: 0.14
Nodes (8): API, Bot, ChatCleaner, fakeAPI, Update, NewClient(), Client, Message

### Community 10 - "Dependabot Config"
Cohesion: 0.14
Nodes (14): prefix, patterns, patterns, docker, github-actions, day, interval, updates (+6 more)

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
Cohesion: 0.36
Nodes (8): Alembic migrations (schema core), Secrets via .env, SQL migrations (schema notifier), Pull request template, PR checklist (branch, contracts, migrations, schema isolation, secrets), POST /api/v1/listings, PostgreSQL (schemas core and notifier, port 5432), RawListing (internal data format)

### Community 15 - "Compose Logging"
Cohesion: 0.20
Nodes (14): logging, build, logging, ports, restart, driver, options, logging (+6 more)

### Community 16 - "Core API Listings (TZ)"
Cohesion: 0.15
Nodes (14): GET /api/v1/listings (фильтры, сортировка, пагинация), GET /api/v1/listings/{id} (карточка), 3.5 Кэш (ключ с хэшем фильтров, TTL 600 с), 6.5 Границы ответственности команды, cache:version и инвалидация кэша, core-api (FastAPI, порт 8000), НФТ-10: p95 GET списка <300 мс с кэшем, <500 мс без, НФТ-12: Ошибки JSON API {error, message} (+6 more)

### Community 17 - "Client"
Cohesion: 0.08
Nodes (25): City, Country, eventJSON, healthProber, fresh(), Client, matchCity(), newMux() (+17 more)

### Community 18 - "Compose Infrastructure"
Cohesion: 0.17
Nodes (11): image, ports, restart, volumes, services, postgres, volumes, pgdata (+3 more)

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
Cohesion: 0.22
Nodes (10): depends_on, postgres, redis, build, depends_on, ports, restart, condition (+2 more)

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

### Community 34 - "notifier"
Cohesion: 0.15
Nodes (11): HTTP, notifier, Запуск, Компоненты (`internal/`), Миграции, Переменные окружения, Статус, Тесты (+3 more)

### Community 50 - "sql-to-er.py"
Cohesion: 0.06
Nodes (36): _alembic(), _app_dsn(), _components(), _dsn(), _head_revision(), _lifecycle(), test_migrations_full_cycle(), _verify_schema() (+28 more)

### Community 51 - "postgres_test.go"
Cohesion: 0.08
Nodes (43): ParseSubscription(), TestParseSubscription(), TestParseSubscriptionCityWithSpace(), TestParseSubscriptionErrors(), TestParseSubscriptionStudio(), baseEvent(), TestMatchAll(), TestRecipientsDedupsChats() (+35 more)

### Community 52 - "Таблицы"
Cohesion: 0.04
Nodes (40): core-api, Запуск, Переменные окружения, Планируется (набросок, по ТЗ и плану Тимура), Тесты, Что есть сейчас, cities (ТЗ 3.7: уникальность города внутри страны), city_name_aliases (ТЗ 4.2: «таблица либо поле по решению Ильи» — выбрана таблица) (+32 more)

### Community 53 - "in_memory_listing_store.py"
Cohesion: 0.16
Nodes (10): build_listing_event(), _in_range(), _matches_filters(), OutboxEvent, CreateListingResult, ListingSearchResult, StoredListing, ListingCreateResponse (+2 more)

### Community 54 - "with"
Cohesion: 0.08
Nodes (24): runs-on, steps, concurrency, cancel-in-progress, group, jobs, build-and-deploy, name (+16 more)

### Community 55 - "InMemoryListingStore"
Cohesion: 0.26
Nodes (11): InMemoryListingStore, anyio_backend(), listing(), test_concurrent_duplicates_create_one_listing_and_event(), test_create_saves_listing_and_event(), test_duplicate_preserves_original_listing_and_event(), test_event_build_failure_does_not_save_listing(), test_get_unknown_listing_returns_none() (+3 more)

### Community 56 - "ListingStore"
Cohesion: 0.15
Nodes (6): ListingStore, create_listing(), get_listing(), get_listing_store(), list_listings(), _listing_data()

### Community 57 - "health.py"
Cohesion: 0.27
Nodes (11): check_dependencies(), health_summary(), live(), DependencyState, HealthChecks, HealthReport, LiveReport, ReadinessError (+3 more)

### Community 58 - "ListingCreate"
Cohesion: 0.19
Nodes (9): ListingCreate, payload(), test_invalid_value_is_rejected(), test_minimal_listing_from_json(), test_optional_fields_accept_null(), test_photos_preserve_order_in_json(), test_published_at_is_converted_to_utc(), test_required_field_cannot_be_omitted() (+1 more)

### Community 59 - "Event"
Cohesion: 0.17
Nodes (4): Event, Recipients(), logRateLimited(), mockJournal

### Community 60 - "Settings"
Cohesion: 0.18
Nodes (10): Settings, clean_settings_env(), test_database_url_accepts_asyncpg(), test_database_url_built_from_parts_escapes_special_characters(), test_database_url_rejects_wrong_driver(), test_database_url_takes_priority_over_parts(), test_database_url_validation_error_hides_password(), test_environment_overrides_dotenv() (+2 more)

### Community 61 - "DeferredJob"
Cohesion: 0.24
Nodes (6): DeferredQueue, jobKey(), DeferredJob, MemoryQueue, mockQueue, PgQueue

### Community 62 - "Dispatcher"
Cohesion: 0.22
Nodes (4): Journal, Dispatcher, journalKey, Sender

### Community 63 - "asyncio"
Cohesion: 0.16
Nodes (4): do_run_migrations(), run_async_migrations(), run_migrations_offline(), run_migrations_online()

### Community 64 - "2. Сценарии"
Cohesion: 0.12
Nodes (15): 0. Когда прогон возможен, 1. Предусловия, 2. Сценарии, 3. Критерии закрытия issue #20, 4. Инструменты, S10. Безопасность и ошибки (НФТ-1, 2, 3, 12), S1. Стартовая конфигурация, S2. Прямая публикация (без collector, доступен раньше остальных) (+7 more)

### Community 65 - "error_handlers.py"
Cohesion: 0.23
Nodes (4): http_error_handler(), internal_error_handler(), register_error_handlers(), validation_error_handler()

### Community 66 - "consumer_test.go"
Cohesion: 0.31
Nodes (11): runConsumer(), TestConsumerDeadLetterOnBadPayload(), TestConsumerDeadLetterOnMissingData(), TestConsumerDeliversAndAcks(), TestConsumerGroupCreateFailureIsFatal(), TestConsumerKeepsPendingWhenDeadLetterFails(), TestConsumerKeepsPendingWhenHandlerFails(), TestConsumerStaysSilentBelowStreamLenThreshold() (+3 more)

### Community 67 - "test_listings_http.py"
Cohesion: 0.18
Nodes (4): client(), payload(), test_list_listings_filters_by_area(), test_list_listings_sorts_by_created_at()

### Community 68 - "postgres"
Cohesion: 0.17
Nodes (12): runs-on, services, jobs, alembic, notifier-sql, name, runs-on, services (+4 more)

### Community 69 - "steps"
Cohesion: 0.26
Nodes (12): steps, steps, notifier-pg-tests, runs-on, steps, steps, name, run (+4 more)

### Community 70 - "python-ci.yml"
Cohesion: 0.17
Nodes (11): concurrency, cancel-in-progress, group, name, on, pull_request, push, workflow_dispatch (+3 more)

### Community 73 - "test_listing_filters_http.py"
Cohesion: 0.29
Nodes (6): client(), test_http_filters_defaults(), test_http_filters_explain_invalid_combinations(), test_http_filters_openapi_query_parameters(), test_http_filters_parse_query_parameters(), test_http_filters_return_validation_error()

### Community 74 - "smoke-flow.sh"
Cohesion: 0.40
Nodes (10): bad(), cleanup(), ok(), poll_count(), psql_q(), redis_cli(), redis_xlen(), smoke-flow.sh script (+2 more)

### Community 75 - "ci.yml"
Cohesion: 0.20
Nodes (9): concurrency, cancel-in-progress, group, name, on, pull_request, push, branches (+1 more)

### Community 76 - "notifier-p95.sh — p95 доставки уведомлений (НФТ-11)"
Cohesion: 0.20
Nodes (9): coreapi-p95.sh — p95 времени GET списка (НФТ-10), notifier-p95.sh — p95 доставки уведомлений (НФТ-11), scripts/bench — бенчмарки, Замер «без кэша» (< 500 мс) — вручную, Ограничения, Переменные окружения, Предусловия, Что делает скрипт (+1 more)

### Community 78 - ".github/README.md"
Cohesion: 0.22
Nodes (6): version, .github, Workflows ([workflows/](workflows/)), Заметки, Скрипты и конфиги, check-md-links.sh script

### Community 79 - "docs-links.yml"
Cohesion: 0.22
Nodes (8): name, on, pull_request, push, workflow_dispatch, paths, branches, paths

### Community 80 - "check_postgres"
Cohesion: 0.32
Nodes (5): check_postgres(), check_redis(), scenario(), hanging_operation(), scenario()

### Community 81 - "postgres"
Cohesion: 0.25
Nodes (8): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, env, image, options, ports, postgres

### Community 82 - "steps"
Cohesion: 0.32
Nodes (8): steps, steps, name, run, uses, with, working-directory, python-version

### Community 83 - "reaper_test.go"
Cohesion: 0.50
Nodes (6): NewReaper(), TestReaperDeletesOnlyConfirmedBeforeMinPending(), TestReaperEmptyPendingUsesLastDelivered(), TestReaperNothingToCleanWhenGroupFresh(), TestReaperRaceNewPendingBetweenReads(), TestReaperSkipsAllWhenPendingAtHead()

### Community 84 - "Отчёт приёмочного замера НФТ-11 (p95 задержки доставки)"
Cohesion: 0.29
Nodes (6): Вердикт, Границы этого замера, Отклонения в ходе замера (зафиксировано для честности), Отчёт приёмочного замера НФТ-11 (p95 задержки доставки), Результат, Условия замера (НФТ-10: «фиксируются машина, состав данных и фильтры»)

### Community 85 - "steps"
Cohesion: 0.29
Nodes (7): runs-on, steps, jobs, check, name, run, uses

### Community 86 - "on"
Cohesion: 0.29
Nodes (7): on, pull_request, push, workflow_dispatch, paths, branches, paths

### Community 88 - "collector"
Cohesion: 0.33
Nodes (5): collector, Запуск, Планируется (набросок, по ТЗ), Статус, Что есть сейчас

### Community 89 - "Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой)"
Cohesion: 0.33
Nodes (5): Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой), До демонстрации (подготовка, ~10 минут), Запасные ответы на вопросы комиссии, Кто что показывает, Порядок показа

### Community 92 - "Feature branch workflow"
Cohesion: 0.40
Nodes (3): CI required checks (notifier lint/test, gitleaks, docker compose), gitleaks (secret leak detection), require-ci ruleset

### Community 93 - "redis"
Cohesion: 0.40
Nodes (5): services, image, options, ports, redis

### Community 94 - "ER-диаграмма схемы core"
Cohesion: 0.50
Nodes (3): ER-диаграмма схемы core, Как перегенерировать, Схема notifier

### Community 95 - "env"
Cohesion: 0.50
Nodes (4): GITHUB_TOKEN, NOTIFIER_TEST_DSN, NOTIFIER_TEST_REDIS_ADDR, env

### Community 96 - "env"
Cohesion: 0.50
Nodes (4): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, env

## Knowledge Gaps
- **61 isolated node(s):** `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов`, `ФТ-1.7: Веб-интерфейс Jinja2 + HTMX`, `Consumer group notifier (XREADGROUP, XAUTOCLAIM, XACK)` (+56 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 419 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **32 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Компоненты (`internal/`)` connect `notifier` to `main.go`, `context.Context`, `postgres_test.go`?**
  _High betweenness centrality (0.154) - this node is a cross-community bridge._
- **Are the 12 inferred relationships involving `testEvent()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`testEvent()` has 12 INFERRED edges - model-reasoned connections that need verification._
- **What connects `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов` to the rest of the system?**
  _61 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `main.go` be split into smaller, more focused modules?**
  _Cohesion score 0.05148795465281058 - nodes in this community are weakly interconnected._
- **Why does `Postgres` connect `postgres_test.go` to `main.go`, `notifier`, `context.Context`, `Dispatcher`?**
  _High betweenness centrality (0.053) - this node is a cross-community bridge._
- **Are the 29 inferred relationships involving `NewDispatcher()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`NewDispatcher()` has 29 INFERRED edges - model-reasoned connections that need verification._
- **Should `context.Context` be split into smaller, more focused modules?**
  _Cohesion score 0.08484848484848485 - nodes in this community are weakly interconnected._