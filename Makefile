.PHONY: build test test-race lint vet clean help

# Build variables
BINARY_NAME := zion-node
BUILD_DIR := release
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/zion-protocol/zion-node/internal/daemon.Version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.buildTime=$(BUILD_TIME)

## build: Build the zion-node binary
build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/zion-node
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME) ($(VERSION))"

## test: Run unit tests
test:
	go test ./internal/... ./pkg/... -count=1 -timeout 120s

## test-race: Run tests with race detector
test-race:
	go test ./internal/... ./pkg/... -race -count=1 -timeout 180s

## test-verbose: Run tests with verbose output
test-verbose:
	go test ./internal/... ./pkg/... -v -count=1 -timeout 120s

## lint: Run golangci-lint
lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "Install golangci-lint: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...

## vet: Run go vet
vet:
	go vet ./...

## fmt: Format code
fmt:
	gofmt -w .

## fmt-check: Check code formatting (for CI)
fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "Files need formatting:"; gofmt -l .; exit 1; }

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR) dist/
	rm -rf logs/

## snapshot: Build a local snapshot release (no publish)
snapshot:
	goreleaser release --snapshot --clean

## help: Show this help
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
