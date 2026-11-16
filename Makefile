# Task runner (PROJECT_STRUCTURE §7). Go runs inside Docker via scripts/go.sh,
# so the host only needs Docker and Node. On Windows without make, run the
# commands after each target directly in Git Bash.
GO := scripts/go.sh

.PHONY: dev up down generate schema migrate-up migrate-down lint test test-integration eval build persona index web-dev

dev: up web-dev                      ## Compose (redis, api, worker) + web with reload

up:                                  ## Start redis, api, worker, web in Docker
	docker compose up --build -d redis api worker

down:
	docker compose down

web-dev:
	cd web && npm run dev

generate: schema                     ## All generators (schema SQL, TS API types)
	cd web && npm run generate

schema:                              ## db/schema.sql, db/seed.sql and the goose migration
	node scripts/build-schema.mjs

migrate-up:                          ## Apply schema + seed to $$DB_URL (or NEON_DB_URL) with psql in Docker
	scripts/psql.sh -f /db/schema.sql && scripts/psql.sh -f /db/seed.sql

migrate-down:
	scripts/psql.sh -c "select 1" && echo "Down: run the '-- +goose Down' section of migrations/20261115000001_init.sql"

lint:
	$(GO) vet ./...
	cd web && npm run typecheck

test:                                ## Unit tests (no network, no database)
	$(GO) test ./...
	cd web && npm test

test-integration:                    ## Store tests against a throwaway Postgres
	scripts/test-integration.sh

eval:                                ## Golden set with real models (costs a few cents)
	$(GO) run ./cmd/eval

index:                               ## Refresh the Destination Cost Index once
	$(GO) run ./cmd/worker refresh-index

persona:                             ## Retrain persona/packs/kora.persona from persona/sources
	scripts/train-persona.sh

build:                               ## Build images
	docker compose build
