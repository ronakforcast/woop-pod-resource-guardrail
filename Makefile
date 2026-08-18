.PHONY: test vet build

GOCACHE ?= /tmp/woop-guardrail-go-cache
GOMODCACHE ?= /tmp/woop-guardrail-go-mod

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

vet:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...

build:
	mkdir -p bin
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) CGO_ENABLED=0 go build -o bin/guardrail ./cmd/guardrail
