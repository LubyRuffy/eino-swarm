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
	@echo "make mobile-android-release  Android APK/AAB into bin/"
	@echo "make desktop-release  macOS zwai.app zip into bin/ (darwin host)"
	@echo "make release      Mac zip + Android APK onto one GitHub Release (needs gh)"
	@echo "make check      formatting, vet, test, e2e"
	@echo "make docs-check  validate feature and contract references"

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

.PHONY: desktop-release
desktop-release: frontend
	go run ./internal/desktop/pack -version "$(VERSION)" -o bin

# One tag, both installers. VERSION is major.minor.patch. gh must be logged in.
# A missing Release is created. An existing one is updated. Other releases stay.
.PHONY: release
release:
	go run ./internal/release/cmd -version "$(VERSION)" -check
	$(MAKE) desktop-release
	@test -d mobile/node_modules || (cd mobile && npm install)
	cd mobile && ANDROID_ARTIFACT=apk VERSION="$(VERSION)" npm run cap:android-release
	go run ./internal/release/cmd -version "$(VERSION)" -dir bin

.PHONY: mobile-android-release
mobile-android-release:
	@test -d mobile/node_modules || (cd mobile && npm install)
	cd mobile && VERSION="$(VERSION)" npm run cap:android-release

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

.PHONY: docs-check
docs-check:
	python3 tools/docs_check.py
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_audit_pending_batch.py'

.PHONY: check
check: fmt-check vet docs-check test e2e

.PHONY: clean
clean:
	rm -rf bin $(FRONTEND)/test-results $(FRONTEND)/playwright-report $(FRONTEND)/.e2e-data
