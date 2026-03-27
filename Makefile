.PHONY: dev build test lint migrate rollback

dev:
	$(HOME)/go/bin/air

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -race -cover

lint:
	golangci-lint run

migrate:
	migrate -path migrations -database "$$DB_URL" up

rollback:
	migrate -path migrations -database "$$DB_URL" down 1
