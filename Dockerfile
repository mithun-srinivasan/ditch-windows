# syntax=docker/dockerfile:1
# The demo uses the real restic pipeline and files module, synthetic fixtures for
# Windows-only modules, and the same OpenTUI interface as the Windows build.
FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=demo
ARG COMMIT=unknown
ARG DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/ditch-windows ./cmd/ditch-windows

FROM node:26-bookworm-slim AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM node:26-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates restic \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 demo \
    && mkdir -p /demo /app \
    && chown demo:demo /demo /app

WORKDIR /app
COPY ui/package.json ui/package-lock.json ./
RUN npm ci --omit=dev
COPY --from=ui /ui/dist ./dist
COPY --from=build /out/ditch-windows /usr/local/bin/ditch-windows
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh

# The demo command generates its own published, disposable passphrase for the
# synthetic repository. Never use this image or password for real data.
ENV DITCH_DEMO_DIR=/demo \
    DITCH_RESTIC_BINARY=/usr/bin/restic \
    DITCH_TUI_BIN=/app/dist/index.js \
    DITCH_UI_DIR=/app \
    TERM=xterm-256color

USER demo
WORKDIR /home/demo

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
