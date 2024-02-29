GO_SRCS := $(shell find . -type f -name '*.go' -a ! \( -name 'zz_generated*' -o -name '*_test.go' \))
GO_TESTS := $(shell find . -type f -name '*_test.go')
TAG_NAME = $(shell git describe --tags --abbrev=0 --exact-match 2>/dev/null)
TAG_NAME_DEV = $(shell git describe --tags --abbrev=0 2>/dev/null)
GIT_COMMIT = $(shell git rev-parse --short=7 HEAD)
VERSION = $(or $(TAG_NAME),$(and $(TAG_NAME_DEV),$(TAG_NAME_DEV)-dev),$(GIT_COMMIT))

golint := $(shell which golangci-lint)
ifeq ($(golint),)
golint := $(shell go env GOPATH)/bin/golangci-lint
endif

.PHONY: bin/distributed-systems-server
bin/distributed-systems-server: $(GO_SRCS)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$@" ./cmd/server/main.go

.PHONY: unit
unit:
	go test -race -covermode=atomic -tags=unit -timeout=30s ./...

$(golint):
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: lint
lint: $(golint)
	$(golint) run ./...

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: version
version:
	@echo TAG_NAME=${TAG_NAME}
	@echo TAG_NAME_DEV=${TAG_NAME_DEV}
	@echo VERSION=${VERSION}
