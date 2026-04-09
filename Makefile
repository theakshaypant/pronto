VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS  = -ldflags "-X github.com/theakshaypant/pronto/internal/cli.Version=$(VERSION) \
                      -X github.com/theakshaypant/pronto/internal/cli.Commit=$(COMMIT) \
                      -X github.com/theakshaypant/pronto/internal/cli.Date=$(DATE)"

.PHONY: build build-cli build-action test vet lint clean

## build: Build both CLI and Action binaries
build: build-cli build-action

## build-cli: Build the CLI binary
build-cli:
	go build $(LDFLAGS) -o dist/pronto ./cmd/cli

## build-action: Build the Action binary
build-action:
	go build -o dist/pronto-action ./cmd/pronto

## test: Run all tests
test:
	go test ./...

## vet: Run go vet
vet:
	go vet ./...

## lint: Run go vet and build
lint: vet build

## clean: Remove build artifacts
clean:
	rm -rf dist/

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
