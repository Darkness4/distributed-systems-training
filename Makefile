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

protoc-gen-go := $(shell which protoc-gen-go)
ifeq ($(protoc-gen-go),)
protoc-gen-go := $(shell go env GOPATH)/bin/protoc-gen-go
endif

protoc-gen-connect-go := $(shell which protoc-gen-connect-go)
ifeq ($(protoc-gen-connect-go),)
protoc-gen-connect-go := $(shell go env GOPATH)/bin/protoc-gen-connect-go
endif

buf := $(shell which buf)
ifeq ($(buf),)
buf := $(shell go env GOPATH)/bin/buf
endif

.PHONY: bin/distributed-systems-server
bin/distributed-systems-server: $(GO_SRCS)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$@" ./cmd/server/main.go

.PHONY: unit
unit:
	go test -race -covermode=atomic -tags=unit -timeout=30s ./...

$(golint):
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

$(protoc-gen-go):
	go install google.golang.org/protobuf/cmd/protoc-gen-go

$(protoc-gen-connect-go):
	go install connectrpc.com/connect/cmd/protoc-gen-connect-go

$(buf):
	go install github.com/bufbuild/buf/cmd/buf@latest

.PHONY: lint
lint: $(golint)
	$(golint) run ./...

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: protos
protos: $(protoc-gen-go) $(protoc-gen-connect-go) $(buf)
	$(buf) lint protos
	$(buf) generate protos

.PHONY: version
version:
	@echo TAG_NAME=${TAG_NAME}
	@echo TAG_NAME_DEV=${TAG_NAME_DEV}
	@echo VERSION=${VERSION}
