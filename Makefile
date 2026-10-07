APP_NAME := ai-content-farm

.PHONY: run build test fmt up down shorts-deps studio-deps

run:
	bash scripts/run-local.sh

shorts-deps:
	python3 -m venv .venv
	.venv/bin/pip install -r requirements-shorts.txt

studio-deps:
	python3 -m venv .venv
	.venv/bin/pip install -r requirements-studio.lock -r requirements-shorts.txt

build:
	go build -p=1 -o bin/api ./cmd/api

test:
	go test -p=1 ./...

fmt:
	gofmt -w cmd internal

up:
	bash scripts/docker-start.sh

down:
	docker compose down
