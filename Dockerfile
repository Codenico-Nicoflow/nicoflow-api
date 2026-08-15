# Stage 1 — builder
FROM --platform=linux/amd64 golang:1.26.6-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-w -s' -o nicoflow-api ./cmd/api

# Stage 2 — runtime
FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/nicoflow-api /nicoflow-api
# Migrations are embedded into the binary (go:embed in migrations/embed.go) and
# applied on boot, so they don't need to be copied into the runtime image.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/nicoflow-api"]
