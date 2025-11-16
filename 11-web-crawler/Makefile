.PHONY: run build test clean

run:
	go run cmd/crawler/main.go --seed https://example.com

build:
	go build -o bin/crawler cmd/crawler/main.go

test:
	go test -v ./internal/...

bench:
	go test -bench=. -benchmem ./internal/bloomfilter

clean:
	rm -rf bin/
	go clean

help:
	@echo "Available targets:"
	@echo "  run   - Run the crawler"
	@echo "  build - Build the binary"
	@echo "  test  - Run tests"
	@echo "  bench - Run benchmarks"
	@echo "  clean - Clean build artifacts"
