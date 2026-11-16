#!/usr/bin/env sh
# Trains persona/packs/kora.persona from persona/sources with the pinned
# OpenPersona commit (same as go.mod), inside Docker. Needs OPENAI_API_KEY in
# .env (OpenPersona's extractor default: openai:gpt-4o-mini; override with
# OPENPERSONA_EXTRACT_MODEL). After training, review the pack:
#   scripts/train-persona.sh inspect
set -e
ROOT="$(cd "$(dirname "$0")/.." && (pwd -W 2>/dev/null || pwd))"
COMMIT=00c329503be8
key() { sed -n "s/^$1=//p" "$ROOT/.env" 2>/dev/null | tr -d '\r"'"'" | head -1; }
OPENAI_API_KEY="${OPENAI_API_KEY:-$(key OPENAI_API_KEY)}"
OPENROUTER_API_KEY="${OPENROUTER_API_KEY:-$(key OPENROUTER_API_KEY)}"
MODE="${1:-train}"
MSYS_NO_PATHCONV=1 docker run --rm \
  -v koraai-gomod:/go/pkg/mod -v koraai-gocache:/root/.cache/go-build \
  -v "$ROOT/persona:/persona" \
  -e OPENAI_API_KEY="$OPENAI_API_KEY" -e OPENROUTER_API_KEY="$OPENROUTER_API_KEY" \
  -e OPENPERSONA_EXTRACT_MODEL="${OPENPERSONA_EXTRACT_MODEL:-openai:gpt-4o-mini}" \
  -e MODE="$MODE" -e COMMIT="$COMMIT" \
  golang:1.27-alpine sh -c '
    set -e
    apk add --no-cache git >/dev/null
    git clone -q https://github.com/VicoAritonang/open-persona /src && cd /src && git checkout -q "$COMMIT"
    CGO_ENABLED=0 go build -o /usr/local/bin/openpersona ./cmd/openpersona
    if [ "$MODE" = "train" ]; then
      openpersona train -spec /persona/spec.json -src /persona/sources -out /persona/packs/kora.persona
    fi
    openpersona inspect -pack /persona/packs/kora.persona | head -60
  '
