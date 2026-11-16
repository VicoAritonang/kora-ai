# `openpersona serve` for user memory (optional, compose profile "memory").
# Memory stays disabled for real users until OpenPersona supports scope
# deletion (TECHNICAL_SPEC §5).
FROM golang:1.27-alpine AS build
RUN apk add --no-cache git
ARG COMMIT=00c329503be8
RUN git clone -q https://github.com/VicoAritonang/open-persona /src && cd /src && git checkout -q ${COMMIT} && \
    CGO_ENABLED=0 go build -o /out/openpersona ./cmd/openpersona

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/openpersona /openpersona
VOLUME /data
EXPOSE 8090
ENTRYPOINT ["/openpersona", "serve", "-addr", ":8090", "-db", "/data/mem.db"]
