SHELL := /bin/sh

GO ?= go
NPM ?= npm
DOCKER ?= docker
COMPOSE ?= $(DOCKER) compose
IMAGE ?= xhs-downloader:local
BIN_DIR ?= bin

.PHONY: toolchains go-test go-vet web-ci web-build test build run docker-build compose-config compose-up compose-down

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

web-build: web-ci
	$(NPM) --prefix web run build

test: go-test go-vet web-build

build: web-build
	mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o "$(BIN_DIR)/xhs-api" ./cmd/api

run: build
	HOST=0.0.0.0 PORT=5556 WEB_DIST_DIR=web/dist XHS_VOLUME_DIR=Volume "$(BIN_DIR)/xhs-api"

docker-build:
	$(DOCKER) build -t "$(IMAGE)" .

compose-config:
	$(COMPOSE) config

compose-up:
	$(COMPOSE) up --build -d

compose-down:
	$(COMPOSE) down
