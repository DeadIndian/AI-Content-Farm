APP_NAME := ai-content-farm

.PHONY: run build test fmt up down shorts-deps

run:
	bash scripts/run-local.sh

shorts-deps:
	python3 -m venv .venv
	.venv/bin/pip install -r requirements-shorts.txt

build:
	go build -o bin/api ./cmd/api

test:
	go test ./...

fmt:
	gofmt -w cmd internal

up:
	bash scripts/docker-start.sh

down:
	docker compose down
