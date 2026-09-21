.DEFAULT_GOAL := help
SHELL := /bin/bash

# dist/ is a Vite artefact, not source. `go run ./cmd/zwai desktop` (and
# web) rebuild it when the TypeScript sources changed. `make frontend`
# is the same incremental build via `go generate ./frontend`.
FRONTEND := frontend
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: help
help:
	@echo "make run        open the app in a native window"
	@echo "make web        serve the app in a browser"
	@echo "make mock       run on the scripted offline provider"
	@echo "make build      build ./bin/zwai"
	@echo "make frontend   rebuild frontend/dist if the sources changed"
	@echo "make dev        vite dev server against a running zwai web"
	@echo "make test       go test -race -cover ./... plus the front-end and phone unit tests"
	@echo "make e2e        Playwright end-to-end tests on the offline provider"
	@echo "make mobile-sync  copy the phone web bundle into the iOS/Android apps"
	@echo "make mobile-ios   open the iOS app in Xcode"
	@echo "make mobile-android  open the Android app in Android Studio"
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
frontend:
	go generate ./$(FRONTEND)

.PHONY: dev
dev: node_modules
	cd $(FRONTEND) && npm run dev

.PHONY: test
test: test-go test-web test-mobile

.PHONY: test-go
test-go:
	go test -race -cover -timeout 20m ./...

.PHONY: test-web
test-web: node_modules
	cd $(FRONTEND) && npm run test

.PHONY: test-mobile
test-mobile:
	@test -d mobile/node_modules || (cd mobile && npm install)
	cd mobile && npm test

.PHONY: mobile-sync
mobile-sync:
	@test -d mobile/node_modules || (cd mobile && npm install)
	cd mobile && npm run cap:sync

.PHONY: mobile-ios
mobile-ios: mobile-sync
	cd mobile && npx cap open ios

.PHONY: mobile-android
mobile-android: mobile-sync
	cd mobile && npx cap open android

.PHONY: e2e
e2e: frontend
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
