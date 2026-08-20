BINARY      := anexia
PKG         := github.com/ProbstenHias/anexia-cli
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(PKG)/internal/cli.version=$(VERSION) \
	-X $(PKG)/internal/cli.commit=$(COMMIT) \
	-X $(PKG)/internal/cli.date=$(DATE)

GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0
GOFUMPT       := mvdan.cc/gofumpt@v0.9.1
GOIMPORTS     := golang.org/x/tools/cmd/goimports@v0.39.0
GORELEASER    := github.com/goreleaser/goreleaser/v2@v2.17.1

.PHONY: all build install test cover lint fmt fmt-check vet tidy ci release-snapshot clean

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

install:
	go install -trimpath -ldflags '$(LDFLAGS)' .

test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go run $(GOLANGCI_LINT) run

fmt:
	go run $(GOFUMPT) -w .
	go run $(GOIMPORTS) -w -local $(PKG) .

fmt-check:
	@out=$$(go run $(GOFUMPT) -l .; go run $(GOIMPORTS) -l -local $(PKG) .); \
	if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

tidy:
	go mod tidy
	git diff --exit-code go.mod go.sum

ci: fmt-check vet lint test

release-snapshot:
	go run $(GORELEASER) release --snapshot --clean

clean:
	rm -rf bin dist coverage.out coverage.html
