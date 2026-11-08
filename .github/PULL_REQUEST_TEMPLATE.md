<!--
Title: `feat(notifier): ...` / `fix(core-api): ...` — conventional commit style
-->

## What

<!-- Short description. Reference the issue if there is one: Closes #NN -->

## How to test

<!-- Steps: `docker compose up --build`, `cd notifier && go test ./...`, etc. -->

## Checklist

- [ ] PR is from a feature branch (an issue reference is welcome but not required)
- [ ] Contracts (`listings.new`, `POST /api/v1/listings`, `RawListing`) are unchanged — or the change was agreed with the team
- [ ] DB changes are only migrations: Alembic for schema `core`, SQL for `notifier`
- [ ] The service touches only its own Postgres schema; other services' data via API/events
- [ ] Secrets are only in `.env`, nothing added to the repo
