APP=stageflow
BINARY=bin/$(APP)
GO_FILES=$(shell find . -name '*.go' -not -path './third_party/*')

.PHONY: run run-sandbox build build-sandbox test lint migrate-up migrate-down compose-up compose-down

run:
	go run ./cmd/stageflow

run-sandbox:
	go run ./cmd/stageflow-sandbox

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/stageflow

build-sandbox:
	mkdir -p bin
	go build -o bin/stageflow-sandbox ./cmd/stageflow-sandbox

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l $(GO_FILES))" || (echo 'gofmt reported unformatted files'; gofmt -l $(GO_FILES); exit 1)
	go test ./...

migrate-up:
	docker compose exec -T postgres psql -U stageflow -d stageflow -f /migrations/000001_init_stageflow_schema.up.sql

migrate-down:
	docker compose exec -T postgres psql -U stageflow -d stageflow -f /migrations/000001_init_stageflow_schema.down.sql

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down
