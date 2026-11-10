# syntax=docker/dockerfile:1.28.0

# ---- web: build the Svelte UI ----
FROM ghcr.io/philippdormann/bun:1.4.4@sha256:88fd80de9f2719f378c01171510ecc5283445e471ce72d8f6dd92a4767ab9127 AS web
WORKDIR /src
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web .
RUN bun run build

# ---- build: compile the Go binary with the UI embedded ----
FROM golang:1.27-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /internal/web/dist ./internal/web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/philippdormann/umago/server/internal/api.Version=${VERSION}" -o /umago ./cmd/umago

# ---- runtime: binary + CA certificates only ----
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 umago && mkdir -p /data && chown umago:umago /data
COPY --from=build /umago /usr/local/bin/umago
USER umago
VOLUME ["/data"]
EXPOSE 8080
ENV UMAGO_PORT=8080 UMAGO_DATABASE_PATH=/data/umago.db
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["umago"]
