VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/0x46616c6b/openaleph-cli/internal/cmd.version=$(VERSION)

.PHONY: build test lint vet snapshot

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/oa ./cmd/oa

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

snapshot:
	goreleaser release --snapshot --clean
