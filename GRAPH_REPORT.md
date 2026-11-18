# Graph Report - real-estate-pro  (2026-11-18)

## Corpus Check
- 101 files · ~71,039 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 13 file(s) not represented in the graph (top: (none) 10, .example 1, .ini 1)

## Summary
- 1393 nodes · 3388 edges · 106 communities (78 shown, 28 thin omitted)
- Extraction: 84% EXTRACTED · 16% INFERRED · 0% AMBIGUOUS · INFERRED: 542 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `a0a6c4c6`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- event.go
- ListingFilters
- testing.T
- test_health.py
- Core API Bootstrap & Errors
- Notifier CI Workflow
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
- context.Context
- Event
- main.go
- test_migrations.py
- Subscription
- postgres_test.go
- listings.py
- with
- InMemoryListingStore
- in_memory_listing_store.py
- health.py
- main_test.go
- go_pkg_time
- DeferredJob
- bot_test.go
- Settings
- ListingCreate
- 2. Сценарии
- asyncio
- dicts/client_test.go
- main.py
- consumer_test.go
- test_listings_http.py
- Reaper
- test_listing_filters_http.py
- smoke-flow.sh
- Таблицы
- Быстрый старт по схеме core — для разработчиков
- ci.yml
- migrations.yml
- db-backup-restore.md
- .github/README.md
- docs-links.yml
- telegram/client_test.go
- postgres
- reaper_test.go
- Отчёт приёмочного замера НФТ-11 (p95 задержки доставки)
- on
- notifier-sql
- go_pkg_strings
- 0001_core_schema.py
- collector
- Проект схемы `core`
- core-api
- Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой)
- .XRange
- Бенчмарк доставки notifier (НФТ-11)
- Feature branch workflow
- Резервное копирование и проверка восстановления
- docs/README.md
- syncBuf
- notifier-p95.sh
- ui-headless.sh
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
- `Планируется (набросок, по ТЗ и плану Тимура)` --references--> `ListingFilters`  [INFERRED]
  core-api/README.md → core-api/app/listing_filters.py
- `Событие listings.new (Core API → Redis Streams → Notifier)` --shares_data_with--> `Redis (кэш, SET, Streams, порт 6379)`  [INFERRED]
  docs/contracts.md → docs/Real_Estate_Pro_TZ.pdf
- `ФТ-3.7: GET /health notifier и POST /debug/event` --references--> `Событие listings.new (Core API → Redis Streams → Notifier)`  [INFERRED]
  docs/Real_Estate_Pro_TZ.pdf → docs/contracts.md
- `ФТ-3.5: Журнал дедупликации уведомлений (user_id, listing_id)` --semantically_similar_to--> `Дедупликация по паре (source, external_id)`  [INFERRED] [semantically similar]
  docs/Real_Estate_Pro_TZ.pdf → docs/contracts.md
- `Компоненты (`internal/`)` --references--> `ParseEvent()`  [INFERRED]
  notifier/README.md → notifier/internal/model/event.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Поток данных листинга: Collector → Core API → outbox → Redis Stream → Notifier → Telegram** — docs_real_estate_pro_tz_collector, docs_contracts_post_api_v1_listings, docs_real_estate_pro_tz_transactional_outbox, docs_contracts_listings_new, docs_real_estate_pro_tz_notifier, docs_real_estate_pro_tz_telegram [EXTRACTED 1.00]
- **Listing delivery pipeline (collector -> core-api -> listings.new -> notifier -> Telegram)** — readme_collector, readme_core_api, readme_listings_new, readme_notifier, readme_telegram_bot_api [EXTRACTED 1.00]
- **Mock data ecosystem (adapters, modes, RawListing dump)** — readme_collector, readme_avito_api_adapter, readme_mock_adapter, readme_mock_modes, readme_rawlisting [EXTRACTED 1.00]
- **Надёжная доставка событий: outbox, pending/XAUTOCLAIM, DLQ, AOF, лимит уведомлений** — docs_real_estate_pro_tz_transactional_outbox, docs_real_estate_pro_tz_pending_ownership, docs_contracts_listings_dead, docs_real_estate_pro_tz_aof_durability, docs_contracts_notification_limit [EXTRACTED 1.00]
- **Required CI checks for merge (require-ci ruleset)** — contributing_ci_checks, contributing_require_ci, contributing_gitleaks, contributing_python_ci [EXTRACTED 1.00]
- **Многоуровневая дедупликация: Redis-хэш Collector → уникальность PostgreSQL → журнал уведомлений notifier** — docs_contracts_dedup_source_external_id, docs_real_estate_pro_tz_url_hash, docs_real_estate_pro_tz_dedup_urls, docs_real_estate_pro_tz_фт_3_5, docs_real_estate_pro_tz_exactly_once [INFERRED 0.85]

## Communities (106 total, 28 thin omitted)

### Community 0 - "event.go"
Cohesion: 0.14
Nodes (22): ParseSubscription(), TestParseSubscription(), TestParseSubscriptionCityWithSpace(), TestParseSubscriptionErrors(), TestParseSubscriptionStudio(), int64OrZero(), isAlphaUpper2(), isHexDigit() (+14 more)

### Community 1 - "ListingFilters"
Cohesion: 0.22
Nodes (18): ListingFilters, test_listing_filters_accept_nonempty_text(), test_listing_filters_accept_valid_bounds(), test_listing_filters_accept_valid_choices(), test_listing_filters_accept_valid_country(), test_listing_filters_accept_valid_location(), test_listing_filters_accept_valid_pagination(), test_listing_filters_accept_valid_ranges() (+10 more)

### Community 2 - "testing.T"
Cohesion: 0.07
Nodes (105): New(), newTestBot(), TestStartEnsuresUser(), TestStartSubscribeStopStartScenario(), TestStopDeletesUserAndReportsCount(), TestStopDropsDeferredJobs(), TestStopRemovesAllSubscriptions(), TestStopWithoutCleanerStillWorks() (+97 more)

### Community 3 - "test_health.py"
Cohesion: 0.10
Nodes (11): check_postgres(), check_redis(), health_client(), test_dependency_check_timeout(), scenario(), hanging_operation(), test_health_summary(), test_probe_external_cancellation_propagates() (+3 more)

### Community 4 - "Core API Bootstrap & Errors"
Cohesion: 0.13
Nodes (5): http_error_handler(), internal_error_handler(), register_error_handlers(), validation_error_handler(), client()

### Community 5 - "Notifier CI Workflow"
Cohesion: 0.06
Nodes (40): run, runs-on, steps, GITHUB_TOKEN, NOTIFIER_TEST_DSN, NOTIFIER_TEST_REDIS_ADDR, POSTGRES_DB, POSTGRES_PASSWORD (+32 more)

### Community 6 - "Python CI Workflow"
Cohesion: 0.06
Nodes (38): concurrency, cancel-in-progress, group, CORE_API_LOG_LEVEL, POSTGRES_DB, POSTGRES_HOST, POSTGRES_PASSWORD, POSTGRES_PORT (+30 more)

### Community 7 - "Bot"
Cohesion: 0.15
Nodes (6): Bot, ChatCleaner, stubValidator, Validator, Describe(), ChatCleaner

### Community 8 - "Stream Consumer & Claim"
Cohesion: 0.23
Nodes (6): isBusyGroup(), NewConsumer(), streamLenWarnState(), TestStreamLenWarnStateHysteresis(), Consumer, Handler

### Community 9 - "Telegram API Client"
Cohesion: 0.19
Nodes (6): API, fakeAPI, Update, NewClient(), Client, Message

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
Cohesion: 0.09
Nodes (22): City, Country, eventJSON, healthProber, fresh(), Client, matchCity(), newMux() (+14 more)

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
Cohesion: 0.17
Nodes (10): HTTP, notifier, Запуск, Миграции, Переменные окружения, Статус, Тесты, docs/contracts.md (inter-service contracts) (+2 more)

### Community 50 - "context.Context"
Cohesion: 0.10
Nodes (7): errStore, recordingStore, Limiter, Store, failingStore, mockLimiter, userGateStore

### Community 51 - "Event"
Cohesion: 0.13
Nodes (8): Event, Recipients(), Journal, logRateLimited(), Dispatcher, journalKey, MemoryJournal, Sender

### Community 52 - "main.go"
Cohesion: 0.12
Nodes (13): checkResult, ConsumerName(), DefaultLenWarnThreshold(), DefaultWorkers(), TestConsumerName(), DefaultCleanupInterval(), env(), envPositiveDuration() (+5 more)

### Community 53 - "test_migrations.py"
Cohesion: 0.12
Nodes (11): clients(), _alembic(), _app_dsn(), _components(), _dsn(), _head_revision(), _lifecycle(), test_migrations_full_cycle() (+3 more)

### Community 54 - "Subscription"
Cohesion: 0.13
Nodes (8): Subscription, sameText(), Memory, collectSubscriptions(), derefStr(), scanSubscription(), Компоненты (`internal/`), Postgres

### Community 55 - "postgres_test.go"
Cohesion: 0.22
Nodes (25): applyMigration(), assertSameInt(), assertSameInt64(), assertSameSub(), assertTablesEmpty(), mustAdd(), mustAddUser(), mustByChat() (+17 more)

### Community 56 - "listings.py"
Cohesion: 0.15
Nodes (10): ListingStore, StoredListing, create_listing(), get_listing(), get_listing_store(), list_listings(), _listing_data(), ListingCreateResponse (+2 more)

### Community 57 - "with"
Cohesion: 0.08
Nodes (24): runs-on, steps, concurrency, cancel-in-progress, group, jobs, build-and-deploy, name (+16 more)

### Community 58 - "InMemoryListingStore"
Cohesion: 0.27
Nodes (11): InMemoryListingStore, anyio_backend(), listing(), test_concurrent_duplicates_create_one_listing_and_event(), test_create_saves_listing_and_event(), test_duplicate_preserves_original_listing_and_event(), test_event_build_failure_does_not_save_listing(), test_get_unknown_listing_returns_none() (+3 more)

### Community 59 - "in_memory_listing_store.py"
Cohesion: 0.19
Nodes (6): build_listing_event(), _in_range(), _matches_filters(), OutboxEvent, CreateListingResult, ListingSearchResult

### Community 60 - "health.py"
Cohesion: 0.27
Nodes (11): check_dependencies(), health_summary(), live(), DependencyState, HealthChecks, HealthReport, LiveReport, ReadinessError (+3 more)

### Community 62 - "go_pkg_time"
Cohesion: 0.19
Nodes (11): applied(), Apply(), migrationNames(), TestApplyOnEmptySchema(), TestApplyParallel(), TestMain(), TestVersionOfAndSort(), versionOf() (+3 more)

### Community 63 - "DeferredJob"
Cohesion: 0.20
Nodes (5): DeferredQueue, jobKey(), DeferredJob, mockQueue, PgQueue

### Community 64 - "bot_test.go"
Cohesion: 0.18
Nodes (11): FormatEvent(), TestFormatEventGiantCurrencyUnconditionalTier(), TestFormatEventNewContract(), TestFormatEventNilAddressUnchanged(), TestFormatEventShortEventUnchanged(), TestFormatEventTruncatesGiantURL(), TestFormatEventTruncatesLongAddress(), truncateRunes() (+3 more)

### Community 65 - "Settings"
Cohesion: 0.18
Nodes (10): Settings, clean_settings_env(), test_database_url_accepts_asyncpg(), test_database_url_built_from_parts_escapes_special_characters(), test_database_url_rejects_wrong_driver(), test_database_url_takes_priority_over_parts(), test_database_url_validation_error_hides_password(), test_environment_overrides_dotenv() (+2 more)

### Community 66 - "ListingCreate"
Cohesion: 0.20
Nodes (8): ListingCreate, payload(), test_invalid_value_is_rejected(), test_minimal_listing_from_json(), test_optional_fields_accept_null(), test_photos_preserve_order_in_json(), test_published_at_is_converted_to_utc(), test_required_field_cannot_be_omitted()

### Community 67 - "2. Сценарии"
Cohesion: 0.12
Nodes (16): 0. Когда прогон возможен, 1. Предусловия, 2. Сценарии, 3. Критерии закрытия issue #20, 4. Инструменты, S10. Безопасность и ошибки (НФТ-1, 2, 3, 12), S1. Стартовая конфигурация, S2. Прямая публикация (без collector, доступен раньше остальных) (+8 more)

### Community 68 - "asyncio"
Cohesion: 0.16
Nodes (4): do_run_migrations(), run_async_migrations(), run_migrations_offline(), run_migrations_online()

### Community 69 - "dicts/client_test.go"
Cohesion: 0.21
Nodes (11): New(), newServer(), TestCachedHealthy(), TestCacheServesRepeatedCalls(), TestHealthy(), TestStaleCacheWhenCoreAPIDown(), TestUnavailableWithoutCache(), TestValidateCanonicalizesCountryAndCity() (+3 more)

### Community 70 - "main.py"
Cohesion: 0.22
Nodes (5): create_db_engine(), setup_logging(), lifespan(), root(), create_redis_client()

### Community 71 - "consumer_test.go"
Cohesion: 0.31
Nodes (11): runConsumer(), TestConsumerDeadLetterOnBadPayload(), TestConsumerDeadLetterOnMissingData(), TestConsumerDeliversAndAcks(), TestConsumerGroupCreateFailureIsFatal(), TestConsumerKeepsPendingWhenDeadLetterFails(), TestConsumerKeepsPendingWhenHandlerFails(), TestConsumerStaysSilentBelowStreamLenThreshold() (+3 more)

### Community 72 - "test_listings_http.py"
Cohesion: 0.18
Nodes (4): client(), payload(), test_list_listings_filters_by_area(), test_list_listings_sorts_by_created_at()

### Community 74 - "test_listing_filters_http.py"
Cohesion: 0.29
Nodes (6): client(), test_http_filters_defaults(), test_http_filters_explain_invalid_combinations(), test_http_filters_openapi_query_parameters(), test_http_filters_parse_query_parameters(), test_http_filters_return_validation_error()

### Community 75 - "smoke-flow.sh"
Cohesion: 0.40
Nodes (10): bad(), cleanup(), ok(), poll_count(), psql_q(), redis_cli(), redis_xlen(), smoke-flow.sh script (+2 more)

### Community 76 - "Таблицы"
Cohesion: 0.20
Nodes (10): cities (ТЗ 3.7: уникальность города внутри страны), city_name_aliases (ТЗ 4.2: «таблица либо поле по решению Ильи» — выбрана таблица), countries (ТЗ 3.7), currencies (ТЗ 3.2: исходные курсы задаются миграциями), listing_duplicates (ТЗ 3.3: связь «возможный дубль», обе записи сохраняются), listing_photos (решение: отдельная таблица, не массив/JSONB), listings (ТЗ 3.1 + 3.3), outbox_events (ТЗ 4.3: transactional outbox, объявление + задание в одной транзакции) (+2 more)

### Community 77 - "Быстрый старт по схеме core — для разработчиков"
Cohesion: 0.20
Nodes (10): Outbox (ТЗ 4.3), Быстрый старт по схеме core — для разработчиков, Дедупликация (ТЗ 3.3) — контракт для реализации, Деньги (ТЗ 3.2), Идентичности и массовая вставка (важно для дампа), Карта таблиц, Подъём базы, Роли (миграция 0002) (+2 more)

### Community 78 - "ci.yml"
Cohesion: 0.20
Nodes (9): concurrency, cancel-in-progress, group, name, on, pull_request, push, branches (+1 more)

### Community 79 - "migrations.yml"
Cohesion: 0.20
Nodes (9): jobs, name, on, pull_request, push, workflow_dispatch, paths, branches (+1 more)

### Community 80 - "db-backup-restore.md"
Cohesion: 0.28
Nodes (4): backup.sh script, restore-verify.sh script, scripts, Планируется (набросок)

### Community 81 - ".github/README.md"
Cohesion: 0.22
Nodes (6): version, .github, Workflows ([workflows/](workflows/)), Заметки, Скрипты и конфиги, check-md-links.sh script

### Community 82 - "docs-links.yml"
Cohesion: 0.22
Nodes (8): runs-on, steps, jobs, check, name, name, run, uses

### Community 83 - "telegram/client_test.go"
Cohesion: 0.33
Nodes (5): newTestClient(), TestOtherAPIErrorsNotRateLimited(), TestRateLimitedCarriesRetryAfter(), TestRateLimitedWithoutParametersUsesDefault(), TestSendMessageOK()

### Community 84 - "postgres"
Cohesion: 0.25
Nodes (8): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, env, image, options, ports, postgres

### Community 85 - "reaper_test.go"
Cohesion: 0.50
Nodes (6): NewReaper(), TestReaperDeletesOnlyConfirmedBeforeMinPending(), TestReaperEmptyPendingUsesLastDelivered(), TestReaperNothingToCleanWhenGroupFresh(), TestReaperRaceNewPendingBetweenReads(), TestReaperSkipsAllWhenPendingAtHead()

### Community 86 - "Отчёт приёмочного замера НФТ-11 (p95 задержки доставки)"
Cohesion: 0.29
Nodes (6): Вердикт, Границы этого замера, Отклонения в ходе замера (зафиксировано для честности), Отчёт приёмочного замера НФТ-11 (p95 задержки доставки), Результат, Условия замера (НФТ-10: «фиксируются машина, состав данных и фильтры»)

### Community 87 - "on"
Cohesion: 0.29
Nodes (7): on, pull_request, push, workflow_dispatch, paths, branches, paths

### Community 88 - "notifier-sql"
Cohesion: 0.29
Nodes (7): notifier-sql, runs-on, services, steps, name, run, uses

### Community 89 - "go_pkg_strings"
Cohesion: 0.43
Nodes (4): Connect(), dsnFromEnv(), quoteValue(), TestDsnFromEnv()

### Community 91 - "collector"
Cohesion: 0.33
Nodes (5): collector, Запуск, Планируется (набросок, по ТЗ), Статус, Что есть сейчас

### Community 92 - "Проект схемы `core`"
Cohesion: 0.33
Nodes (4): Индексы (ТЗ 4.2 + ревью PR #66), План реализации (после согласования), Проект схемы `core`, Решения по ревью PR #66 (2026-11-15)

### Community 93 - "core-api"
Cohesion: 0.33
Nodes (6): core-api, Запуск, Переменные окружения, Планируется (набросок, по ТЗ и плану Тимура), Тесты, Что есть сейчас

### Community 94 - "Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой)"
Cohesion: 0.33
Nodes (5): Демо-сценарий сдачи (черновик Даниила; порядок показа согласуется с командой), До демонстрации (подготовка, ~10 минут), Запасные ответы на вопросы комиссии, Кто что показывает, Порядок показа

### Community 96 - "Бенчмарк доставки notifier (НФТ-11)"
Cohesion: 0.33
Nodes (5): Бенчмарк доставки notifier (НФТ-11), Ограничения, Переменные окружения, Предусловия, Что делает скрипт

### Community 97 - "Feature branch workflow"
Cohesion: 0.40
Nodes (3): CI required checks (notifier lint/test, gitleaks, docker compose), gitleaks (secret leak detection), require-ci ruleset

### Community 98 - "Резервное копирование и проверка восстановления"
Cohesion: 0.40
Nodes (5): Дамп, Проверка восстановления (обязательно для каждого дампа), Расписание и хранение, Резервное копирование и проверка восстановления, Что покрывается

## Knowledge Gaps
- **61 isolated node(s):** `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов`, `ФТ-1.7: Веб-интерфейс Jinja2 + HTMX`, `Consumer group notifier (XREADGROUP, XAUTOCLAIM, XACK)` (+56 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 393 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **28 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `notifier` connect `notifier` to `Subscription`?**
  _High betweenness centrality (0.155) - this node is a cross-community bridge._
- **Are the 12 inferred relationships involving `testEvent()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`testEvent()` has 12 INFERRED edges - model-reasoned connections that need verification._
- **What connects `notifier`, `4.6 Необязательный mock-сервер источника`, `НФТ-3: X-Admin-Key для /debug/*, /collect/run и статусов` to the rest of the system?**
  _61 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `event.go` be split into smaller, more focused modules?**
  _Cohesion score 0.14153846153846153 - nodes in this community are weakly interconnected._
- **Why does `Компоненты (`internal/`)` connect `Subscription` to `event.go`, `notifier`?**
  _High betweenness centrality (0.152) - this node is a cross-community bridge._
- **Are the 29 inferred relationships involving `NewDispatcher()` (e.g. with `TestPgDispatcherRedeliveredOnceAfterDBFailure()` and `TestPgDispatcherDoubleDelivery()`) actually correct?**
  _`NewDispatcher()` has 29 INFERRED edges - model-reasoned connections that need verification._
- **Should `testing.T` be split into smaller, more focused modules?**
  _Cohesion score 0.07460524409676952 - nodes in this community are weakly interconnected._