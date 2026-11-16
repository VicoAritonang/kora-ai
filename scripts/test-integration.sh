#!/usr/bin/env sh
# Runs the store integration tests against a throwaway Postgres in Docker
# with db/schema.sql + db/seed.sql applied. Never touches Neon.
set -e
ROOT="$(cd "$(dirname "$0")/.." && (pwd -W 2>/dev/null || pwd))"
NET=kora-it
docker network create $NET >/dev/null 2>&1 || true
docker rm -f kora-it-pg >/dev/null 2>&1 || true
docker run -d --name kora-it-pg --network $NET -e POSTGRES_PASSWORD=test -e POSTGRES_DB=kora postgres:17 >/dev/null
trap 'docker rm -f kora-it-pg >/dev/null 2>&1' EXIT
until docker exec kora-it-pg pg_isready -U postgres -d kora >/dev/null 2>&1; do sleep 1; done
sleep 2
MSYS_NO_PATHCONV=1 docker run --rm --network $NET -v "$ROOT/db:/db:ro" postgres:17 \
  psql postgres://postgres:test@kora-it-pg:5432/kora -q -v ON_ERROR_STOP=1 -f /db/schema.sql -f /db/seed.sql >/dev/null
GO_DOCKER_ARGS="--network $NET -e TEST_DB_URL=postgres://postgres:test@kora-it-pg:5432/kora?sslmode=disable" \
  "$ROOT/scripts/go.sh" test -tags integration -v ./internal/store/...
