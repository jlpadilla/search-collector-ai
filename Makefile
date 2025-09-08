# Search Collector AI Makefile

# Variables
BINARY_NAME=search-collector-ai
MAIN_PACKAGE=./cmd/...
PKG_LIST=$(shell go list ./... | grep -v /vendor/)
GO_FILES=$(shell find . -name '*.go' | grep -v /vendor/ | grep -v _test.go)

# Build info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# Go build flags
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildDate=$(BUILD_DATE)"

.PHONY: help
help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) .

.PHONY: build-linux
build-linux: ## Build the binary for Linux
	@echo "Building $(BINARY_NAME) for Linux..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux .

.PHONY: install
install: ## Install the binary
	@echo "Installing $(BINARY_NAME)..."
	go install $(LDFLAGS) .

.PHONY: clean
clean: ## Remove build artifacts
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME) $(BINARY_NAME)-linux
	@go clean

.PHONY: deps
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

.PHONY: deps-update
deps-update: ## Update dependencies
	@echo "Updating dependencies..."
	go get -u ./...
	go mod tidy

.PHONY: test
test: ## Run tests
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: test-coverage
test-coverage: test ## Run tests and show coverage
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: bench
bench: ## Run benchmarks
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

.PHONY: lint
lint: ## Run linters
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run; \
	fi

.PHONY: fmt
fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

.PHONY: check
check: fmt vet lint test ## Run all checks (format, vet, lint, test)

.PHONY: run
run: ## Run the application
	@echo "Running $(BINARY_NAME)..."
	go run .

.PHONY: docker-build
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) .
	docker tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

.PHONY: docker-run
docker-run: ## Run Docker container
	@echo "Running Docker container..."
	docker run --rm -it $(BINARY_NAME):latest

.PHONY: generate
generate: ## Run go generate
	@echo "Running go generate..."
	go generate ./...

.PHONY: vendor
vendor: ## Create vendor directory
	@echo "Creating vendor directory..."
	go mod vendor

.PHONY: dev-setup
dev-setup: ## Set up development environment
	@echo "Setting up development environment..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go mod download

.PHONY: ci
ci: check ## Run CI pipeline (all checks)
	@echo "CI pipeline completed successfully"

# Development helpers
.PHONY: watch
watch: ## Watch for changes and rebuild (requires entr)
	@echo "Watching for changes..."
	@if command -v entr >/dev/null 2>&1; then \
		find . -name '*.go' | entr -r make run; \
	else \
		echo "entr not found. Install with: brew install entr (macOS) or apt-get install entr (Ubuntu)"; \
	fi

.PHONY: profile
profile: ## Run with CPU profiling
	@echo "Running with CPU profiling..."
	go run . -cpuprofile=cpu.prof

.PHONY: mem-profile
mem-profile: ## Run with memory profiling
	@echo "Running with memory profiling..."
	go run . -memprofile=mem.prof

# Default target
.DEFAULT_GOAL := help