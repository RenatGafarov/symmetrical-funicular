.PHONY: all build run run-dry test test-cover test-integration lint fmt vet clean tidy docker-build docker-run help

APP_NAME := arbitragebot
CMD_DIR := ./cmd/arbitragebot
BUILD_DIR := ./bin
CONFIG_FILE := configs/config.yaml

all: lint test build

build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) $(CMD_DIR)

run: build
	@echo "Running $(APP_NAME)..."
	$(BUILD_DIR)/$(APP_NAME) --config $(CONFIG_FILE)

run-dry: build
	@echo "Running $(APP_NAME) in dry-run mode..."
	$(BUILD_DIR)/$(APP_NAME) --config $(CONFIG_FILE) --dry-run

test:
	@echo "Running tests..."
	go test -race -cover ./...

test-cover:
	@echo "Running tests with coverage..."
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-integration:
	@echo "Running integration tests..."
	go test -race -tags=integration ./tests/integration/...

lint:
	@echo "Running linter..."
	golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	go fmt ./...
	goimports -w .

vet:
	@echo "Running go vet..."
	go vet ./...

clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

tidy:
	@echo "Tidying modules..."
	go mod tidy

docker-build:
	@echo "Building Docker image..."
	docker build -t $(APP_NAME):latest .

docker-run:
	@echo "Running with Docker Compose..."
	docker-compose up -d

help:
	@echo "Available targets:"
	@echo "  all              - Run lint, test, and build"
	@echo "  build            - Build the application"
	@echo "  run              - Build and run the application"
	@echo "  run-dry          - Run in dry-run mode (paper trading)"
	@echo "  test             - Run tests with race detection"
	@echo "  test-cover       - Run tests with coverage report"
	@echo "  test-integration - Run integration tests"
	@echo "  lint             - Run golangci-lint"
	@echo "  fmt              - Format code with go fmt and goimports"
	@echo "  vet              - Run go vet"
	@echo "  clean            - Remove build artifacts"
	@echo "  tidy             - Tidy go modules"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-run       - Run with Docker Compose"
	@echo "  help             - Show this help"