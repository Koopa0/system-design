.PHONY: help start stop server test

help:
	@echo "可用命令："
	@echo "  make start  - 啟動 NATS Server"
	@echo "  make stop   - 停止 NATS Server"
	@echo "  make server - 啟動 Scheduler"

start:
	docker-compose up -d

stop:
	docker-compose down

server:
	go run cmd/server/main.go
