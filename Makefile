# System Design 專案 Makefile
# 提供本地開發和 CI 檢查命令

.PHONY: help
help: ## 顯示幫助信息
	@echo "System Design 專案開發命令："
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ============================================
# 依賴安裝
# ============================================
.PHONY: install-tools
install-tools: ## 安裝開發工具（golangci-lint, gosec, sqlc 等）
	@echo "📦 安裝開發工具..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
	@echo "✅ 工具安裝完成"

# ============================================
# 代碼質量檢查
# ============================================
.PHONY: lint
lint: ## 運行 golangci-lint 檢查所有代碼
	@echo "🔍 運行 golangci-lint..."
	golangci-lint run --config=.golangci.yml ./...

.PHONY: lint-fix
lint-fix: ## 自動修復可修復的問題
	@echo "🔧 自動修復代碼問題..."
	golangci-lint run --config=.golangci.yml --fix ./...

.PHONY: fmt
fmt: ## 格式化所有 Go 代碼
	@echo "✨ 格式化代碼..."
	gofmt -w -s .
	@if command -v goimports > /dev/null 2>&1; then \
		goimports -w .; \
	else \
		echo "⚠️  goimports 未安裝，跳過 import 整理"; \
	fi

.PHONY: fmt-check
fmt-check: ## 檢查代碼格式（不修改）
	@echo "🔍 檢查代碼格式..."
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "❌ 以下文件格式不正確："; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "✅ 代碼格式正確"

# ============================================
# 安全檢查
# ============================================
.PHONY: security
security: ## 運行安全掃描（gosec + govulncheck）
	@echo "🔒 安全掃描..."
	@echo "→ gosec（代碼安全）"
	gosec -fmt=json -out=gosec-report.json ./... || true
	gosec ./...
	@echo ""
	@echo "→ govulncheck（依賴漏洞）"
	govulncheck ./...

.PHONY: vuln
vuln: ## 檢查依賴漏洞
	@echo "🔍 檢查依賴漏洞..."
	govulncheck ./...

# ============================================
# 測試
# ============================================
.PHONY: test
test: ## 運行所有單元測試
	@echo "🧪 運行單元測試..."
	go test -v -race ./...

.PHONY: test-coverage
test-coverage: ## 運行測試並生成覆蓋率報告
	@echo "📊 生成測試覆蓋率..."
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ 覆蓋率報告：coverage.html"

.PHONY: test-short
test-short: ## 運行短測試（跳過慢速測試）
	@echo "⚡ 運行快速測試..."
	go test -v -short ./...

# ============================================
# 構建驗證
# ============================================
.PHONY: build
build: ## 構建所有專案
	@echo "🔨 構建所有專案..."
	@for dir in 01-counter-service 02-room-management 03-url-shortener; do \
		echo "→ 構建 $$dir"; \
		cd $$dir && go build -v -o /tmp/$$dir-app ./cmd/server/main.go && cd ..; \
	done
	@echo "✅ 構建完成"

.PHONY: build-counter
build-counter: ## 構建 Counter Service
	@echo "🔨 構建 Counter Service..."
	cd 01-counter-service && go build -v -o /tmp/counter-app ./cmd/server/main.go

.PHONY: build-room
build-room: ## 構建 Room Management
	@echo "🔨 構建 Room Management..."
	cd 02-room-management && go build -v -o /tmp/room-app ./cmd/server/main.go

.PHONY: build-url
build-url: ## 構建 URL Shortener
	@echo "🔨 構建 URL Shortener..."
	cd 03-url-shortener && go build -v -o /tmp/url-app ./cmd/server/main.go

# ============================================
# SQL 驗證
# ============================================
.PHONY: sqlc-verify
sqlc-verify: ## 驗證 sqlc 生成的代碼是最新的
	@echo "🗄️  驗證 sqlc 生成的代碼..."
	@cd 01-counter-service && \
	if [ -f sqlc.yaml ]; then \
		sqlc generate && \
		git diff --exit-code || (echo "❌ sqlc 代碼不是最新的，請運行 'make sqlc-generate'" && exit 1); \
	fi
	@echo "✅ sqlc 代碼是最新的"

.PHONY: sqlc-generate
sqlc-generate: ## 生成 sqlc 代碼
	@echo "⚙️  生成 sqlc 代碼..."
	@cd 01-counter-service && \
	if [ -f sqlc.yaml ]; then \
		sqlc generate; \
	fi
	@echo "✅ sqlc 代碼生成完成"

# ============================================
# 代碼複雜度分析
# ============================================
.PHONY: complexity
complexity: ## 檢查代碼複雜度
	@echo "📊 分析代碼複雜度..."
	gocyclo -over 15 . || echo "⚠️  某些函數複雜度較高"

# ============================================
# 依賴管理
# ============================================
.PHONY: tidy
tidy: ## 整理 Go modules
	@echo "🧹 整理 Go modules..."
	go mod tidy

.PHONY: verify
verify: ## 驗證 Go modules
	@echo "✅ 驗證 Go modules..."
	go mod verify

.PHONY: download
download: ## 下載依賴
	@echo "📥 下載依賴..."
	go mod download

# ============================================
# 組合命令（CI 流程）
# ============================================
.PHONY: pre-commit
pre-commit: fmt lint test-short ## 提交前檢查（快速）
	@echo "✅ Pre-commit 檢查通過"

.PHONY: ci-local
ci-local: fmt-check lint test sqlc-verify build security ## 本地運行完整 CI 流程
	@echo "✅ 所有 CI 檢查通過"

.PHONY: ci-quick
ci-quick: fmt-check lint test-short build ## 快速 CI 檢查
	@echo "✅ 快速 CI 檢查通過"

# ============================================
# 清理
# ============================================
.PHONY: clean
clean: ## 清理構建產物和緩存
	@echo "🧹 清理..."
	go clean -cache -testcache -modcache
	rm -f coverage.out coverage.html
	rm -f gosec-report.json
	find . -name "*.test" -delete
	find . -name "*.out" -delete
	@echo "✅ 清理完成"

# ============================================
# Docker（可選）
# ============================================
.PHONY: docker-up
docker-up: ## 啟動所有服務（PostgreSQL + Redis）
	@echo "🐳 啟動 Docker 服務..."
	docker-compose up -d

.PHONY: docker-down
docker-down: ## 停止所有服務
	@echo "🐳 停止 Docker 服務..."
	docker-compose down

# ============================================
# 默認目標
# ============================================
.DEFAULT_GOAL := help
