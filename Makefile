.PHONY: dev build test lint docs migrate-up migrate-down migrate-down-one migrate-version migrate-create migrate-force

## Development

dev:
	$(HOME)/go/bin/air

build:
	go build -o bin/api ./cmd/api

test:
	go test ./... -race -count=1 -coverprofile=coverage.out -covermode=atomic

lint:
	golangci-lint run ./...

## Documentation

docs:
	$(HOME)/go/bin/swag init -g cmd/api/main.go --output docs
	$(HOME)/go/bin/swag fmt

## Database migrations (requires DATABASE_URL env var, except migrate-create)

migrate-up:
	migrate -path migrations -database "$$DATABASE_URL" up

migrate-down:
	migrate -path migrations -database "$$DATABASE_URL" down

migrate-down-one:
	migrate -path migrations -database "$$DATABASE_URL" down 1

migrate-version:
	migrate -path migrations -database "$$DATABASE_URL" version

migrate-create:
	@test -n "$(name)" || (echo "Usage: make migrate-create name=<migration_name>" && exit 1)
	migrate create -ext sql -dir migrations -seq $(name)

migrate-force:
	@test -n "$(version)" || (echo "Usage: make migrate-force version=<version_number>" && exit 1)
	migrate -path migrations -database "$$DATABASE_URL" force $(version)
