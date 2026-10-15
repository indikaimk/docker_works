.PHONY: all build test lint clean install doctor

BINARY_NAME=docker-works
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.GitCommit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)"

all: lint test build

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/docker-works

test:
	go test -v -race -cover ./...

lint:
	go vet ./...
	@DIFF=$$(gofmt -s -l .); if [ -n "$$DIFF" ]; then echo "Files need formatting: $$DIFF"; exit 1; fi

doctor:
	go run $(LDFLAGS) ./cmd/docker-works doctor

clean:
	rm -rf bin/ dist/ coverage.out

install:
	sudo ./scripts/install.sh
