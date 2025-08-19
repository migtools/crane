# Makefile for Crane
#
# This Makefile provides multiple build options to handle different network environments
# and build requirements.

.PHONY: build clean test help vendor-init vendor-build build-with-retries check-deps

# Default Go settings
GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
BINARY_NAME ?= crane
BUILD_DIR ?= ./bin

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Build flags
LDFLAGS = -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT_HASH) -X main.buildTime=$(BUILD_TIME)"

# Default build target
build: check-deps
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build for multiple platforms
build-all: check-deps
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 main.go
	GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 main.go
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 main.go
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe main.go
	@echo "Multi-platform build complete"

# Build with network retries and extended timeouts
build-with-retries: check-deps
	@echo "Building with network retry logic..."
	@mkdir -p $(BUILD_DIR)
	@for i in 1 2 3; do \
		echo "Build attempt $$i/3..."; \
		if timeout 300 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) main.go; then \
			echo "Build successful on attempt $$i"; \
			break; \
		else \
			echo "Build attempt $$i failed, retrying..."; \
			sleep 10; \
		fi; \
	done

# Initialize vendor directory
vendor-init:
	@echo "Initializing vendor directory..."
	$(GO) mod vendor
	@echo "Vendor directory created. You can now build offline using 'make vendor-build'"

# Build using vendor directory (offline build)
vendor-build: vendor-init
	@echo "Building using vendor directory (offline)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -mod=vendor $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "Offline build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build with direct module fetching (bypassing proxy)
build-direct: check-deps
	@echo "Building with direct module fetching..."
	@mkdir -p $(BUILD_DIR)
	GOPROXY=direct GOSUMDB=off $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "Direct build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build with custom proxy
build-with-proxy:
	@if [ -z "$(PROXY_URL)" ]; then \
		echo "Error: PROXY_URL environment variable must be set"; \
		echo "Usage: make build-with-proxy PROXY_URL=http://your-proxy:port"; \
		exit 1; \
	fi
	@echo "Building with proxy: $(PROXY_URL)"
	@mkdir -p $(BUILD_DIR)
	HTTP_PROXY=$(PROXY_URL) HTTPS_PROXY=$(PROXY_URL) $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "Proxy build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Check dependencies
check-deps:
	@echo "Checking Go installation..."
	@$(GO) version || (echo "Go is not installed or not in PATH" && exit 1)
	@echo "Go version check passed"

# Download dependencies with retries
deps-download:
	@echo "Downloading dependencies with retries..."
	@for i in 1 2 3; do \
		echo "Download attempt $$i/3..."; \
		if timeout 300 $(GO) mod download; then \
			echo "Dependencies downloaded successfully on attempt $$i"; \
			break; \
		else \
			echo "Download attempt $$i failed, retrying..."; \
			sleep 10; \
		fi; \
	done

# Run tests
test: check-deps
	@echo "Running tests..."
	$(GO) test -v ./...

# Run tests with coverage
test-coverage: check-deps
	@echo "Running tests with coverage..."
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -rf vendor/
	@echo "Clean complete"

# Install binary to GOPATH/bin or GOBIN
install: build
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(LDFLAGS) .
	@echo "Installation complete"

# Display help
help:
	@echo "Crane Build System"
	@echo "=================="
	@echo ""
	@echo "Available targets:"
	@echo "  build              - Build crane binary for current platform"
	@echo "  build-all          - Build for multiple platforms"
	@echo "  build-with-retries - Build with network retry logic"
	@echo "  build-direct       - Build bypassing module proxy"
	@echo "  build-with-proxy   - Build using HTTP proxy (set PROXY_URL)"
	@echo "  vendor-init        - Create vendor directory for offline builds"
	@echo "  vendor-build       - Build using vendor directory (offline)"
	@echo "  deps-download      - Download dependencies with retries"
	@echo "  test               - Run tests"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  install            - Install binary to GOPATH/bin"
	@echo "  clean              - Clean build artifacts"
	@echo "  help               - Show this help message"
	@echo ""
	@echo "Environment variables:"
	@echo "  GO                 - Go binary to use (default: go)"
	@echo "  GOOS               - Target OS (default: current OS)"
	@echo "  GOARCH             - Target architecture (default: current arch)"
	@echo "  BINARY_NAME        - Output binary name (default: crane)"
	@echo "  BUILD_DIR          - Build output directory (default: ./bin)"
	@echo "  PROXY_URL          - HTTP proxy URL for build-with-proxy target"
	@echo ""
	@echo "Network troubleshooting builds:"
	@echo "  make vendor-build      # For completely offline builds"
	@echo "  make build-direct      # Bypass Go module proxy"
	@echo "  make build-with-retries # Retry on network timeouts"
	@echo "  make build-with-proxy PROXY_URL=http://proxy:8080  # Use corporate proxy"

.DEFAULT_GOAL := help