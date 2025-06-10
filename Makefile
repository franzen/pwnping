BINARY_NAME=pwnping
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -s -w"

PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
BUILD_DIR=build

.PHONY: all clean help build-all $(PLATFORMS)

all: build-all

build-all: $(PLATFORMS)

$(PLATFORMS):
	$(eval GOOS=$(word 1,$(subst /, ,$@)))
	$(eval GOARCH=$(word 2,$(subst /, ,$@)))
	$(eval OUTPUT=$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)$(if $(findstring windows,$(GOOS)),.exe))
	@echo "Building for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(OUTPUT) .
	@echo "✓ Built: $(OUTPUT)"

build: 
	@echo "Building for current platform..."
	go build $(LDFLAGS) -o $(BINARY_NAME) .
	@echo "✓ Built: $(BINARY_NAME)"

linux-amd64:
	@$(MAKE) linux/amd64

linux-arm64:
	@$(MAKE) linux/arm64

darwin-amd64:
	@$(MAKE) darwin/amd64

darwin-arm64:
	@$(MAKE) darwin/arm64

windows-amd64:
	@$(MAKE) windows/amd64

windows-arm64:
	@$(MAKE) windows/arm64

run:
	@echo "Running $(BINARY_NAME)..."
	@sudo go run .

run-custom:
	@echo "Running $(BINARY_NAME) with custom router IP..."
	@echo "Usage: make run-custom ROUTER=10.0.0.1"
	@test -n "$(ROUTER)" || (echo "Error: ROUTER not specified" && exit 1)
	@sudo go run . -router=$(ROUTER)

test:
	@echo "Running tests..."
	go test -v ./...

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -f $(BINARY_NAME)
	@echo "✓ Clean complete"

deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy
	@echo "✓ Dependencies installed"

help:
	@echo "Available targets:"
	@echo "  make build         - Build for current platform"
	@echo "  make build-all     - Build for all platforms"
	@echo "  make linux-amd64   - Build for Linux AMD64"
	@echo "  make linux-arm64   - Build for Linux ARM64" 
	@echo "  make darwin-amd64  - Build for macOS AMD64"
	@echo "  make darwin-arm64  - Build for macOS ARM64 (Apple Silicon)"
	@echo "  make windows-amd64 - Build for Windows AMD64"
	@echo "  make windows-arm64 - Build for Windows ARM64"
	@echo "  make run           - Run the application (requires sudo)"
	@echo "  make run-custom    - Run with custom router IP (e.g., make run-custom ROUTER=10.0.0.1)"
	@echo "  make test          - Run tests"
	@echo "  make deps          - Download dependencies"
	@echo "  make clean         - Remove build artifacts"
	@echo "  make help          - Show this help message"

.DEFAULT_GOAL := help