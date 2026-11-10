# Contributing

[English](CONTRIBUTING.md) | [Русский](CONTRIBUTING.ru.md)

Educational team project. The rules below are mandatory for all contributors.

## Areas of responsibility

| Who    | Area |
|--------|------|
| Daniil | notifier (Go), Telegram bot, docker-compose |
| Ilya   | PostgreSQL, migrations (Alembic for `core`, SQL for `notifier`) |
| Dima   | UI, mock data, manual testing |
| Timur  | core-api, collector (Python) |

Do not touch someone else's area: changes there require the owner's consent (a review in the PR or a discussion in an issue).

## Branches and commits

- Work happens in a feature branch, then a PR into `main`. Committing directly to `main` is not allowed.
- Commit style: `feat(notifier): ...`, `fix(core-api): ...`, `ci: ...`, `docs: ...`.
- PRs are merged with a **merge commit** (not squash) — team decision dated 29.10.2026.
- Before opening a PR: the branch must be up to date with `main`; the `require-ci` ruleset enforces this.

## What must not change without team agreement

Per section 6.5 of the requirements spec:

- the contracts in `docs/contracts.md`: the `listings.new` event,
  `POST /api/v1/listings`, the `RawListing` format;
- the database schema changes **only via migrations**:
  - schema `core` — Alembic;
  - schema `notifier` — SQL migrations (author: Ilya, applied by Daniil):
    files in `notifier/migrations/*.sql` are embedded into the binary and
    applied automatically at notifier startup; the file name pattern is
    `000N_description.sql` and they run in lexicographic order; each file
    opens and commits its own transaction (`BEGIN`/`COMMIT`) — the runner
    executes the whole file with a single `Exec`;
- every service works only with its own Postgres schema; other services' data
  is accessed via the API or events;
- secrets live only in `.env` (the repo ships `.env.example`; CI checks for
  leaked secrets with gitleaks).

A contract change is discussed in an issue first; the code comes after the plan.

## CI

A PR into `main` runs `ci` (it also runs on pushes to `main`):

- `notifier`: gofmt, `go vet`, `go test -race`;
- `gitleaks`: secret leak detection;
- `docker`: `docker compose config` + `docker compose build`.

These three checks are required for the merge (the `require-ci` ruleset).

`python-ci` (ruff and tests) is triggered by changes in `core-api/`, `collector/`, `requirements-lint.txt` or the workflow itself — on pushes to any branch and on PRs into `main`. It is not a required check, but a green PR is expected practice.

A merge also requires an approval from the owner of the affected area.

## Local run

```bash
cp .env.example .env   # fill in the secrets
docker compose up --build
```

Pre-commit checks (notifier):

```bash
cd notifier && gofmt -l . && go vet ./... && go test ./...
```

### PostgreSQL/Redis tests

Some notifier tests run against PostgreSQL and Redis. CI has no postgres/redis
services, so these tests are skipped there — that is expected. To run them
locally:

```bash
docker compose up -d postgres redis
# once: create a dedicated test database (default credentials app/app;
# adjust if you changed POSTGRES_USER, POSTGRES_PASSWORD or POSTGRES_PORT in .env)
docker compose exec postgres psql -U app -d realestate -c "CREATE DATABASE realestate_test;"
cd notifier
NOTIFIER_TEST_DSN='host=127.0.0.1 port=5432 user=app password=app dbname=realestate_test' go test ./...
```
