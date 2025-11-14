.PHONY: help run test clean docker-up docker-down

help:
	@echo "可用指令："
	@echo "  make run         - 執行服務"
	@echo "  make test        - 執行測試"
	@echo "  make docker-up   - 啟動 Redis"
	@echo "  make docker-down - 停止 Redis"
	@echo "  make clean       - 清理暫存檔案"

run:
	@echo "啟動服務..."
	go run cmd/server/main.go

test:
	@echo "執行測試..."
	go test -v -race ./...

docker-up:
	@echo "啟動 Redis..."
	docker-compose up -d
	@echo "等待 Redis 就緒..."
	@sleep 2

docker-down:
	@echo "停止 Redis..."
	docker-compose down

clean:
	@echo "清理暫存檔案..."
	go clean
	rm -f coverage.out

.DEFAULT_GOAL := help
