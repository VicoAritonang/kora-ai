#!/usr/bin/env sh
# Runs the Go toolchain inside Docker, so the host needs no Go install.
# Usage: scripts/go.sh test ./...    |   scripts/go.sh mod tidy
set -e
ROOT="$(cd "$(dirname "$0")/.." && (pwd -W 2>/dev/null || pwd))"
MSYS_NO_PATHCONV=1 docker run --rm \
  -v koraai-gomod:/go/pkg/mod -v koraai-gocache:/root/.cache/go-build \
  -v "$ROOT:/src" -w /src \
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 ${GO_DOCKER_ARGS} \
  golang:1.27-alpine go "$@"
