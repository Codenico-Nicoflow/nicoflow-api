.PHONY: dev build test lint docs migrate-up migrate-down migrate-down-one migrate-version migrate-create migrate-force

dev:
	$(HOME)/go/bin/air

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -race -cover

lint:
	golangci-lint run

docs:
	$(HOME)/go/bin/swag init -g cmd/server/main.go --output docs
	$(HOME)/go/bin/swag fmt

## Database migrations (requires DB_URL env var, except migrate-create)

migrate-up:
	migrate -path migrations -database "$$DB_URL" up

migrate-down:
	migrate -path migrations -database "$$DB_URL" down

migrate-down-one:
	migrate -path migrations -database "$$DB_URL" down 1

migrate-version:
	migrate -path migrations -database "$$DB_URL" version

migrate-create:
	@test -n "$(name)" || (echo "Usage: make migrate-create name=<migration_name>" && exit 1)
	migrate create -ext sql -dir migrations -seq $(name)

migrate-force:
	@test -n "$(version)" || (echo "Usage: make migrate-force version=<version_number>" && exit 1)
	migrate -path migrations -database "$$DB_URL" force $(version)
