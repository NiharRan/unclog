VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/NiharRan/unclog/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build test lint run

build:
	go build -ldflags "$(LDFLAGS)" -o bin/unclog .

test:
	go test ./...

lint:
	golangci-lint run ./...

run:
	go run . $(ARGS)
