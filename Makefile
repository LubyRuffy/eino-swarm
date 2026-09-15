.DEFAULT_GOAL := help
SHELL := /bin/bash

# dist/ is committed so a fresh clone can `go run ./cmd/zwai desktop` without a
# Node toolchain. Rebuild it with `make frontend` after touching frontend/src.
FRONTEND := frontend
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: help
help:
	@echo "make run        open the app in a native window"
	@echo "make web        serve the app in a browser"
	@echo "make mock       run on the scripted offline provider"
	@echo "make build      build ./bin/zwai"
	@echo "make frontend   rebuild frontend/dist (needed after editing frontend/src)"
	@echo "make dev        vite dev server against a running zwai web"
	@echo "make test       go test -race -cover ./... plus the front-end unit tests"
	@echo "make e2e        Playwright end-to-end tests on the offline provider"
	@echo "make check      formatting, vet, test, e2e"

.PHONY: run
run:
	go run ./cmd/zwai desktop

.PHONY: web
web:
	go run ./cmd/zwai web

.PHONY: mock
mock:
	go run ./cmd/zwai web --mock

.PHONY: build
build: frontend
	go build -ldflags "$(LDFLAGS)" -o bin/zwai ./cmd/zwai

.PHONY: node_modules
node_modules:
	@test -d $(FRONTEND)/node_modules || (cd $(FRONTEND) && npm install)

.PHONY: frontend
frontend: node_modules
	cd $(FRONTEND) && npm run build

.PHONY: dev
dev: node_modules
	cd $(FRONTEND) && npm run dev

.PHONY: test
test: test-go test-web

.PHONY: test-go
test-go:
	go test -race -cover ./...

.PHONY: test-web
test-web: node_modules
	cd $(FRONTEND) && npm run test

.PHONY: e2e
e2e: node_modules
	cd $(FRONTEND) && npx playwright install chromium && npm run e2e

.PHONY: fmt
fmt:
	gofmt -w $$(git ls-files '*.go')

# check must not rewrite the tree it is checking, so it reports instead of fixing
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed (run make fmt):"; echo "$$unformatted"; exit 1; fi

.PHONY: vet
vet:
	go vet ./...

.PHONY: check
check: fmt-check vet test e2e

.PHONY: clean
clean:
	rm -rf bin $(FRONTEND)/test-results $(FRONTEND)/playwright-report $(FRONTEND)/.e2e-data
