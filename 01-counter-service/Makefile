.PHONY: all build test clean run docker-build docker-run migrate sqlc

# 變數定義
BINARY_NAME=counter-service
DOCKER_IMAGE=counter-service:latest
GO=go
GOFLAGS=-v
TEST_FLAGS=-race -coverprofile=coverage.out -covermode=atomic

# 預設目標
all: clean sqlc build test

# 建置二進位檔案
build:
	@echo "建置應用程式..."
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/server

# 執行測試
test: test-unit test-integration test-benchmark

# 單元測試
test-unit:
	@echo "執行單元測試..."
	$(GO) test $(TEST_FLAGS) ./test/unit/...

# 整合測試
test-integration:
	@echo "執行整合測試..."
	$(GO) test $(TEST_FLAGS) -tags=integration ./test/integration/...

# 基準測試
test-benchmark:
	@echo "執行基準測試..."
	$(GO) test -bench=. -benchmem ./test/benchmark/...

# 測試覆蓋率報告
coverage:
	@echo "生成測試覆蓋率報告..."
	$(GO) test $(TEST_FLAGS) ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "覆蓋率報告已生成: coverage.html"

# 清理
clean:
	@echo "清理建置檔案..."
	rm -rf bin/ coverage.out coverage.html
	rm -rf internal/db internal/sqlc

# 執行應用程式
run: build
	@echo "執行應用程式..."
	./bin/$(BINARY_NAME)

# Docker 相關
docker-build:
	@echo "建置 Docker 映像..."
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	@echo "執行 Docker 容器..."
	docker-compose up -d

docker-stop:
	@echo "停止 Docker 容器..."
	docker-compose down

# 資料庫遷移
migrate-up:
	@echo "執行資料庫遷移..."
	migrate -path migrations -database "$${DATABASE_URL}" up

migrate-down:
	@echo "回滾資料庫遷移..."
	migrate -path migrations -database "$${DATABASE_URL}" down 1

migrate-reset:
	@echo "重置資料庫..."
	migrate -path migrations -database "$${DATABASE_URL}" drop -f
	migrate -path migrations -database "$${DATABASE_URL}" up

# SQL 程式碼生成
sqlc:
	@echo "生成 sqlc 程式碼..."
	sqlc generate

# 開發工具
dev-setup:
	@echo "安裝開發工具..."
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# 程式碼檢查
lint:
	@echo "執行程式碼檢查..."
	golangci-lint run ./...

# 格式化程式碼
fmt:
	@echo "格式化程式碼..."
	$(GO) fmt ./...
	goimports -w .

# 檢查依賴
deps:
	@echo "下載依賴..."
	$(GO) mod download
	$(GO) mod tidy

# 檢查安全性問題
security:
	@echo "檢查安全性問題..."
	gosec ./...

# 效能分析
profile:
	@echo "執行效能分析..."
	$(GO) test -cpuprofile=cpu.prof -memprofile=mem.prof -bench=. ./test/benchmark/...
	$(GO) tool pprof cpu.prof
	$(GO) tool pprof mem.prof

# CI/CD 相關
ci: deps lint test coverage

# 發布版本
release: clean sqlc build
	@echo "建置發布版本..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -a -installsuffix cgo -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -a -installsuffix cgo -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -a -installsuffix cgo -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/server

# 顯示幫助
help:
	@echo "可用的指令："
	@echo "  make build          - 建置應用程式"
	@echo "  make test           - 執行所有測試"
	@echo "  make test-unit      - 執行單元測試"
	@echo "  make test-integration - 執行整合測試"
	@echo "  make test-benchmark - 執行基準測試"
	@echo "  make coverage       - 生成測試覆蓋率報告"
	@echo "  make clean          - 清理建置檔案"
	@echo "  make run            - 執行應用程式"
	@echo "  make docker-build   - 建置 Docker 映像"
	@echo "  make docker-run     - 執行 Docker 容器"
	@echo "  make migrate-up     - 執行資料庫遷移"
	@echo "  make sqlc           - 生成 sqlc 程式碼"
	@echo "  make lint           - 執行程式碼檢查"
	@echo "  make fmt            - 格式化程式碼"
	@echo "  make deps           - 下載依賴"
	@echo "  make help           - 顯示此幫助訊息"