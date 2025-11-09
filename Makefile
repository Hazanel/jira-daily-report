# Makefile for JIRA Daily Report Generator

.PHONY: help build run clean test fmt vet lint

# Default target
help:
	@echo "JIRA Daily Report Generator"
	@echo ""
	@echo "Available targets:"
	@echo "  build    - Build the binary"
	@echo "  run      - Run the application"
	@echo "  clean    - Remove build artifacts"
	@echo "  test     - Run tests"
	@echo "  fmt      - Format code"
	@echo "  vet      - Run go vet"
	@echo "  lint     - Run linters"
	@echo "  install  - Install the binary to GOPATH/bin"

# Build the binary
build:
	@echo "Building jira_update..."
	go build -o jira_update main.go
	@echo "Build complete"

# Run the application
run: build
	@echo "Running jira_update..."
	./jira_update

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f jira_update
	@echo "✅ Clean complete"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "✅ Format complete"

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...
	@echo "✅ Vet complete"

# Run all checks
check: fmt vet
	@echo "✅ All checks passed"

# Install to GOPATH/bin
install: build
	@echo "Installing to GOPATH/bin..."
	go install
	@echo "✅ Installation complete"

# Create go.mod if it doesn't exist
init:
	@if [ ! -f go.mod ]; then \
		echo "Initializing Go module..."; \
		go mod init jira_update; \
		go mod tidy; \
		echo "✅ Go module initialized"; \
	else \
		echo "go.mod already exists"; \
	fi

