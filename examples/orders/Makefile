# Every target is documented with "## ...", so "make help" is always current.
.DEFAULT_GOAL := help

BIN      := bin
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X github.com/acme/orders/internal/buildinfo.version=$(VERSION)

.PHONY: help fmt fmt-check vet lint test cover vuln build run docker pr clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

fmt: ## Format all Go code
	golangci-lint fmt ./...

fmt-check: ## Fail if any Go file is not formatted
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run: make fmt"; exit 1; }

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

test: ## Run all tests with the race detector
	go test -race -count=1 ./...

cover: ## Write an HTML coverage report to coverage.html
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

vuln: ## Check dependencies and the standard library for known vulnerabilities
	go tool govulncheck ./...

build: ## Build both binaries into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)/ ./cmd/...

run: ## Run the service locally with readable logs
	go run ./cmd/ordersd -log-format text -log-level debug

docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t acme/ordersd:$(VERSION) .

pr: fmt-check vet lint test ## Everything CI runs; must pass before a pull request

clean: ## Remove build output
	rm -rf $(BIN) coverage.out coverage.html
