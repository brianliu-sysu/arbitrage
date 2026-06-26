.PHONY: build test lint vet docker-run

BINARY := arbitrage

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/arbitrage/

test:
	go test ./... -count=1 -timeout 30s

test-race:
	go test -race ./... -count=1 -timeout 60s

vet:
	go vet ./...

lint:
	go vet ./...

cover:
	go test ./... -coverprofile=coverage.out -count=1
	go tool cover -html=coverage.out -o coverage.html

docker-build:
	docker build -t arbitrage:latest .

docker-run: docker-build
	docker-compose up -d

clean:
	rm -f $(BINARY) coverage.out coverage.html

run:
	go run ./cmd/arbitrage/ -config config.yaml

.PHONY: migrate-up migrate-down
migrate-up:
	go run ./cmd/migrate/ -db "$$ARBITRAGE_DB_URL" up
migrate-down:
	go run ./cmd/migrate/ -db "$$ARBITRAGE_DB_URL" down
