.PHONY: build test lint install clean

build:
	go build -o bin/digg-ai-pp-cli ./cmd/digg-ai-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/digg-ai-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/digg-ai-pp-mcp ./cmd/digg-ai-pp-mcp

install-mcp:
	go install ./cmd/digg-ai-pp-mcp

build-all: build build-mcp
