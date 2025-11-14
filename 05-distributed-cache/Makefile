.PHONY: help run test clean

help:
	@echo "可用指令："
	@echo "  make run    - 執行服務"
	@echo "  make test   - 執行測試"
	@echo "  make clean  - 清理暫存檔案"

run:
	@echo "啟動服務..."
	go run cmd/server/main.go

test:
	@echo "執行測試..."
	go test -v -race ./...

clean:
	@echo "清理暫存檔案..."
	go clean
	rm -f coverage.out

.DEFAULT_GOAL := help
