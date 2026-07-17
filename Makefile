.PHONY: dev build test lint swagger docker-up docker-down docker-migrate-up \
        migrate-up migrate-down migrate-down-all migrate-version migrate-create migrate-force \
        seed-notifications clear-notifications

DOCKER_DATABASE_URL ?= postgres://nicoflow:BaNa9406%24@localhost:5432/nicoflow?sslmode=disable

# Resolve DATABASE_URL at recipe time without expanding it. `include .env` lets
# Make expand a `$` in the value (a `$` in the DB password becomes `$@` etc.),
# and `source .env` lets the *shell* do the same. So we read the literal line
# from .env with sed and never let either layer touch the `$`. An already-set
# environment variable wins; otherwise we fall back to the .env value.
db_url = $${DATABASE_URL:-$$(sed -n 's/^DATABASE_URL=//p' .env 2>/dev/null)}

## Development

dev:
	$(HOME)/go/bin/air

build:
	go build -o bin/api ./cmd/api

# -timeout 20m: the auth suite is intentionally bcrypt-heavy (cost 12) and, with
# integration tests under -race, approaches Go's default 10m per-package wall on
# slower CI runners. 20m gives headroom without masking a real hang.
test:
	go test ./... -race -count=1 -timeout 20m -coverprofile=coverage.out -covermode=atomic

test-integration:
	go test ./... -tags=integration -race -count=1 -timeout 20m -v

lint:
	golangci-lint run ./...

# Regenerate the OpenAPI/Swagger docs from handler annotations into docs/.
swagger:
	$(HOME)/go/bin/swag init -g cmd/api/main.go -o docs --parseInternal

## Local test data

# Seed a spread of notifications (4 unread + 1 read) for the oldest user, or a
# specific one: `make seed-notifications EMAIL=you@example.com`. Lets you eyeball
# the bell + panel without the 08:00-local cron. See scripts/seed_notifications.sh.
seed-notifications:
	./scripts/seed_notifications.sh $(EMAIL)

# Wipe every notification (all users) from the docker Postgres — reset the bell.
clear-notifications:
	docker compose exec -T postgres psql -U nicoflow -d nicoflow -c "DELETE FROM notifications;"

## Docker Compose

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-migrate-up:
	migrate -path migrations -database "$(DOCKER_DATABASE_URL)" up

## Database migrations
#
# How DATABASE_URL is resolved (see db_url above):
#   - LOCAL: read from .env at recipe time (no `include`/`source`, so a literal
#     `$` in the password survives). `make migrate-up` etc. "just work".
#   - STAGING/PROD: Render injects DATABASE_URL as a real env var, so it wins
#     over .env (which doesn't exist on Render). Migrations run automatically as
#     a Render pre-deploy command — you do NOT run `make migrate-*` against prod.
#     The canonical pre-deploy command is:
#         migrate -path migrations -database "$DATABASE_URL" up
#   - To run a migration against staging/prod manually from your machine, pass
#     the URL inline (use sslmode=require for managed PG):
#         DATABASE_URL='<render-pg-url>' make migrate-up
#   - NEVER run migrate-down / migrate-down-all against staging or prod.
#
# Gotcha: if you ever `source .env` in a plain shell, a raw `$` in the password
# breaks it ("too many colons in address"). Either URL-encode it (`$` -> %24) or
# rely on these make targets, which read the value literally.

migrate-up:
	migrate -path migrations -database "$(db_url)" up

migrate-down:
	migrate -path migrations -database "$(db_url)" down 1

migrate-down-all:
	migrate -path migrations -database "$(db_url)" down

migrate-version:
	migrate -path migrations -database "$(db_url)" version

migrate-create:
	@test -n "$(name)" || (echo "Usage: make migrate-create name=<migration_name>" && exit 1)
	migrate create -ext sql -dir migrations -seq $(name)

migrate-force:
	@test -n "$(version)" || (echo "Usage: make migrate-force version=<version_number>" && exit 1)
	migrate -path migrations -database "$(db_url)" force $(version)
