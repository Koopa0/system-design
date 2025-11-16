.PHONY: run build test clean help

# 運行節點 1
run:
	go run cmd/node/main.go -id=node-1 -port=8080 -n=3 -w=2 -r=2

# 運行節點 2
run2:
	go run cmd/node/main.go -id=node-2 -port=8081 -n=3 -w=2 -r=2

# 運行節點 3
run3:
	go run cmd/node/main.go -id=node-3 -port=8082 -n=3 -w=2 -r=2

# 構建
build:
	go build -o bin/kvstore-node cmd/node/main.go

# 測試
test:
	go test -v ./internal/...

# 基準測試
bench:
	go test -bench=. -benchmem ./internal/

# 清理
clean:
	rm -rf bin/
	go clean

# 幫助
help:
	@echo "Available targets:"
	@echo "  run   - Run node 1 (port 8080)"
	@echo "  run2  - Run node 2 (port 8081)"
	@echo "  run3  - Run node 3 (port 8082)"
	@echo "  build - Build the binary"
	@echo "  test  - Run tests"
	@echo "  bench - Run benchmarks"
	@echo "  clean - Clean build artifacts"
