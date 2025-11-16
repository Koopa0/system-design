.PHONY: run build test clean

run:
	go run cmd/server/main.go

build:
	go build -o bin/server cmd/server/main.go

test:
	go test -v ./internal/...

bench:
	go test -bench=. -benchmem ./internal/...

clean:
	rm -rf bin/
	go clean

help:
	@echo "Available targets:"
	@echo "  run   - Run the server"
	@echo "  build - Build the binary"
	@echo "  test  - Run tests"
	@echo "  bench - Run benchmarks"
	@echo "  clean - Clean build artifacts"
