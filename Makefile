.PHONY: help up down clean logs build test it run image lint fmt proto smoke seed

help:
	@echo "Targets:"
	@echo "  up       Start dependencies (Postgres, Redis, observability)"
	@echo "  down     Stop containers (keep volumes)"
	@echo "  clean    Stop and remove volumes"
	@echo "  build    go build -trimpath -o bin/shortener ./cmd/shortener"
	@echo "  test     Unit tests (no Docker)"
	@echo "  it       Integration tests (Testcontainers spin up real Postgres + Redis)"
	@echo "  run      Run the binary against local dependencies"
	@echo "  proto    Regenerate .pb.go from api/proto/**/*.proto"
	@echo "  image    Build a slim container image"
	@echo "  lint     go vet + staticcheck (if installed)"
	@echo "  fmt      gofmt -w"
	@echo "  smoke    k6 load test — create + redirect"
	@echo "  seed     POST 5 sample links"

up:
	docker compose up -d
	@echo "→ Prometheus: http://localhost:9090"
	@echo "→ Grafana:    http://localhost:3000"
	@echo "→ Jaeger:     http://localhost:16686"

down:
	docker compose down

clean:
	docker compose down -v

logs:
	docker compose logs -f --tail=100

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/shortener ./cmd/shortener

test:
	go test -race -count=1 -short ./...

it:
	go test -race -count=1 -tags=integration ./test/integration/...

run:
	go run ./cmd/shortener

# The generated code is committed, so `go build` and the Dockerfile need no
# protoc. Regenerate after editing the .proto, with the plugin versions that
# match go.mod: protoc-gen-go v1.34.2, protoc-gen-go-grpc v1.5.1.
# module=... places the files at the go_package path (pkg/api/v1).
proto:
	protoc \
	  --go_out=. --go_opt=module=github.com/ISch-1/url-shortener \
	  --go-grpc_out=. --go-grpc_opt=module=github.com/ISch-1/url-shortener \
	  -I api/proto \
	  api/proto/shortener/v1/shortener.proto

image:
	docker build -t url-shortener:local .

lint:
	go vet ./...
	@which staticcheck > /dev/null && staticcheck ./... || echo "staticcheck not installed — skipping"

fmt:
	gofmt -w -s .

smoke:
	k6 run load/create-and-redirect.js

seed:
	@for i in 1 2 3 4 5; do \
	  curl -sf -X POST http://localhost:8080/api/v1/links \
	    -H 'Content-Type: application/json' \
	    -H 'X-Api-Key: demo-key' \
	    -d "{\"originalUrl\":\"https://example.com/page-$$i\"}" | jq ; \
	done
