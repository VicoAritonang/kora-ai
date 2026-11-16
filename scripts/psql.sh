#!/usr/bin/env sh
# Runs psql in Docker against DB_URL (or NEON_DB_URL from .env), with db/
# mounted at /db. Example: scripts/psql.sh -f /db/schema.sql
set -e
ROOT="$(cd "$(dirname "$0")/.." && (pwd -W 2>/dev/null || pwd))"
URL="${DB_URL:-$(sed -n 's/^NEON_DB_URL=//p' "$ROOT/.env" 2>/dev/null | tr -d '\r"'"'" | sed 's/^psql //' | head -1)}"
[ -n "$URL" ] || { echo "DB_URL or NEON_DB_URL is required" >&2; exit 1; }
MSYS_NO_PATHCONV=1 docker run --rm -i -v "$ROOT/db:/db:ro" postgres:17 psql "$URL" -v ON_ERROR_STOP=1 "$@"
