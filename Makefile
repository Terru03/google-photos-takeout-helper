GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: test lint build build-cli build-gui windows-dist fmt

test:
	$(GO) test ./internal/fixer ./internal/cli

lint:
	$(GOLANGCI_LINT) run ./internal/fixer/... ./internal/cli/...

fmt:
	$(GO) fmt ./...

build: build-cli build-gui

build-cli:
	$(GO) build ./cmd/gtf-cli

build-gui:
	$(GO) build ./cmd

windows-dist:
	powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
