BINARY := tl
GOBIN ?= $(shell go env GOPATH)/bin

.PHONY: build install test fmt

build:
	go build -o $(BINARY) .

install:
	go build -o $(GOBIN)/tl .

test:
	go test ./...

fmt:
	gofmt -l -w .
