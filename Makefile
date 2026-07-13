SHELL := /bin/sh

GO ?= go
NPM ?= npm
DOCKER ?= docker
COMPOSE ?= $(DOCKER) compose
IMAGE ?= xhs-downloader:local
BIN_DIR ?= bin

.PHONY: toolchains go-test go-vet web-ci web-test web-build web-check test build run docker-build compose-config compose-up compose-down

toolchains:
	$(GO) version
	python --version
	node --version
	$(NPM) --version
	$(DOCKER) version

go-test:
	$(GO) test ./...

go-vet:
	$(GO) vet ./...

web-ci:
	$(NPM) --prefix web ci

web-test: web-ci
	$(NPM) --prefix web test

web-build: web-ci
	$(NPM) --prefix web run build

web-check:
	$(NPM) --prefix web ci
	$(NPM) --prefix web test
	$(NPM) --prefix web run build

test: go-test go-vet web-check

build: web-build
	mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o "$(BIN_DIR)/xhs-api" ./cmd/api

run: build
	@test -n "$$XHS_ADMIN_PASSWORD" || { echo "XHS_ADMIN_PASSWORD is required" >&2; exit 1; }
	HOST=0.0.0.0 \
	PORT="$${PORT:-5556}" \
	WEB_DIST_DIR=web/dist \
	XHS_VOLUME_DIR=Volume \
	XHS_DATABASE_PATH="$${XHS_DATABASE_PATH:-Volume/Data/xhs.sqlite3}" \
	XHS_SECRET_KEY_PATH="$${XHS_SECRET_KEY_PATH:-}" \
	XHS_ADMIN_USERNAME="$${XHS_ADMIN_USERNAME:-admin}" \
	XHS_ADMIN_PASSWORD="$$XHS_ADMIN_PASSWORD" \
	XHS_ADMIN_SESSION_TTL="$${XHS_ADMIN_SESSION_TTL:-12h}" \
	XHS_SESSION_COOKIE_SECURE="$${XHS_SESSION_COOKIE_SECURE:-false}" \
	XHS_MAX_MEDIA_BYTES="$${XHS_MAX_MEDIA_BYTES:-2147483648}" \
	"$(BIN_DIR)/xhs-api"

docker-build:
	$(DOCKER) build -t "$(IMAGE)" .

compose-config:
	$(COMPOSE) config

compose-up:
	$(COMPOSE) up --build -d

compose-down:
	$(COMPOSE) down
