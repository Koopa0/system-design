.PHONY: help start stop server consumer test bench clean

help: ## 顯示幫助信息
	@echo "可用命令："
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

start: ## 啟動 NATS Server
	docker-compose up -d
	@echo "⏳ 等待 NATS Server 啟動..."
	@sleep 3
	@echo "✅ NATS Server 已啟動"
	@echo "📊 監控面板: http://localhost:8222"

stop: ## 停止 NATS Server
	docker-compose down

server: ## 啟動 HTTP API Server
	go run cmd/server/main.go

consumer: ## 啟動 Consumer（Queue Group 模式）
	go run cmd/consumer/main.go --group order-processor --id 1

consumer-multi: ## 啟動多個 Consumer（負載均衡演示）
	@echo "啟動 3 個 Consumer..."
	@go run cmd/consumer/main.go --group order-processor --id 1 & \
	 go run cmd/consumer/main.go --group order-processor --id 2 & \
	 go run cmd/consumer/main.go --group order-processor --id 3
	@echo "✅ 3 個 Consumer 已啟動（Queue Group: order-processor）"

demo: ## 演示模式（自動發送消息）
	DEMO_MODE=true go run cmd/server/main.go

test: ## 運行測試
	go test ./internal/... -v

bench: ## 性能測試
	@echo "使用 NATS Bench 進行性能測試..."
	@echo "安裝 NATS CLI: go install github.com/nats-io/natscli/nats@latest"
	nats bench orders --msgs 10000 --size 1024 --pub 5 --sub 5

clean: ## 清理資源
	docker-compose down -v
	rm -rf nats-data/

logs: ## 查看 NATS 日誌
	docker-compose logs -f nats

status: ## 查看 Stream 狀態
	@echo "需要安裝 NATS CLI: go install github.com/nats-io/natscli/nats@latest"
	nats stream ls
	nats stream info ORDERS

init: ## 初始化專案依賴
	go mod download
	@echo "✅ 依賴已下載"
