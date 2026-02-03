# Variables
BIN_DIR := bin
LINTER := github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2

.PHONY: all build clean setup lint fmt test

all: build

# Setup development tools
setup:
	@echo "🛠️  Installing tools..."
	go install $(LINTER)
	@echo "✅ Tools installed."

# Build binary
build:
	@echo "🏗️  Building docod CLI..."
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/docod cmd/docod/main.go
	@echo "✅ Build complete. Binary in $(BIN_DIR)/docod"

# Format Code
fmt:
	@echo "✨ Formatting code..."
	go fmt ./...
	@echo "✅ Code formatted."

# Run Lint
lint:
	@echo "🔍 Running Linter..."
	golangci-lint run ./...
	@echo "✅ Lint passed."

# Run Tests
test:
	@echo "🧪 Running Tests..."
	go test -v ./...
	@echo "✅ Tests passed."

# Clean build artifacts
clean:
	@echo "🧹 Cleaning up..."
	rm -rf $(BIN_DIR) docod.db
	@echo "✅ Cleaned."
