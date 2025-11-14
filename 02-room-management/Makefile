.PHONY: all build test clean run

# 變數定義
BINARY_NAME=room-server
GO=go
GOFLAGS=-v

# 預設目標
all: clean build

# 建置
build:
	@echo "Building..."
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/server

# 執行
run: build
	@echo "Running..."
	./bin/$(BINARY_NAME)

# 測試
test:
	@echo "Testing..."
	$(GO) test -v ./...

# 清理
clean:
	@echo "Cleaning..."
	rm -rf bin/

# 開發模式（自動重載）
dev:
	@echo "Development mode..."
	$(GO) run ./cmd/server

# 格式化
fmt:
	@echo "Formatting..."
	$(GO) fmt ./...

# 檢查
lint:
	@echo "Linting..."
	golangci-lint run ./...

# 安裝依賴
deps:
	@echo "Installing dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# 顯示幫助
help:
	@echo "Available commands:"
	@echo "  make build   - Build the application"
	@echo "  make run     - Build and run the application"
	@echo "  make test    - Run tests"
	@echo "  make clean   - Clean build artifacts"
	@echo "  make dev     - Run in development mode"
	@echo "  make fmt     - Format code"
	@echo "  make lint    - Run linter"
	@echo "  make deps    - Install dependencies"
	@echo "  make help    - Show this help message"