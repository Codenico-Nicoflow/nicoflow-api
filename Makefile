.PHONY: dev build test lint docker-up docker-down docker-migrate-up \
        migrate-up migrate-down migrate-down-all migrate-version migrate-create migrate-force

DOCKER_DATABASE_URL ?= postgres://nicoflow:BaNa9406%24@localhost:5432/nicoflow?sslmode=disable

## Development

dev:
	$(HOME)/go/bin/air

build:
	go build -o bin/api ./cmd/api

test:
	go test ./... -race -count=1 -coverprofile=coverage.out -covermode=atomic

test-integration:
	go test ./... -tags=integration -race -count=1 -v

lint:
	golangci-lint run ./...

## Docker Compose

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-migrate-up:
	migrate -path migrations -database "$(DOCKER_DATABASE_URL)" up

## Database migrations (requires DATABASE_URL env var, except migrate-create)

migrate-up:
	migrate -path migrations -database "$$DATABASE_URL" up

migrate-down:
	migrate -path migrations -database "$$DATABASE_URL" down 1

migrate-down-all:
	migrate -path migrations -database "$$DATABASE_URL" down

migrate-version:
	migrate -path migrations -database "$$DATABASE_URL" version

migrate-create:
	@test -n "$(name)" || (echo "Usage: make migrate-create name=<migration_name>" && exit 1)
	migrate create -ext sql -dir migrations -seq $(name)

migrate-force:
	@test -n "$(version)" || (echo "Usage: make migrate-force version=<version_number>" && exit 1)
	migrate -path migrations -database "$$DATABASE_URL" force $(version)
