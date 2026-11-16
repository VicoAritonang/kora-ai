# Builds the api and worker binaries into distroless images.
#   docker build -f deploy/docker/go.Dockerfile --target api -t kora-api .
#   docker build -f deploy/docker/go.Dockerfile --target worker -t kora-worker .
FROM golang:1.27-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG BUILD=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    go build -trimpath -ldflags="-s -w" -o /out/eval ./cmd/eval

FROM gcr.io/distroless/static-debian12:nonroot AS base
WORKDIR /app
# Read-only runtime artifacts: persona pack, guardrails policy and catalog,
# curated norm files (for `worker import-norms`), golden set.
COPY persona/packs ./persona/packs
COPY guardrails ./guardrails
COPY data ./data
COPY eval/golden ./eval/golden
ENV PERSONA_PACK=/app/persona/packs/kora.persona \
    GUARDRAILS_POLICY=/app/guardrails/policy.yaml \
    GUARDRAILS_TOOLS=/app/guardrails/tools.yaml
USER nonroot

FROM base AS api
COPY --from=build /out/api /app/api
EXPOSE 8080
ENTRYPOINT ["/app/api"]

FROM base AS worker
COPY --from=build /out/worker /app/worker
ENTRYPOINT ["/app/worker"]

FROM base AS eval
COPY --from=build /out/eval /app/eval
ENTRYPOINT ["/app/eval"]
