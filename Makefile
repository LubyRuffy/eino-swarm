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
	@echo "make mobile-ios-release  resume the allocated TestFlight batch"
	@echo "make release-check  read-only release preflight"
	@echo "make mobile-android  open the Android app in Android Studio"
	@echo "make mobile-android-release  Android APK/AAB into bin/"
	@echo "make desktop-release  macOS zwai.app zip into bin/ (darwin host)"
	@echo "make release      publish only affected, pending Mac/Android/iOS platforms"
	@echo "make check      formatting, vet, test, e2e"
	@echo "make docs-check  validate feature and contract references"
	@echo "make eval EVAL_PROVIDER=<id>  opt-in task evals against a real model (not in check)"
	@echo "make native-smoke APP=... EVIDENCE=...  one isolated ci-macOS round, 60s budget"
	@echo "make native-smoke-prepare  build the native AX helper once with the candidate"

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
	go test -race -cover -timeout 25m ./...

.PHONY: test-web
test-web: node_modules
	cd $(FRONTEND) && npm run test

.PHONY: test-mobile
test-mobile:
	@test -d mobile/node_modules || (cd mobile && npm install)
	cd mobile && npm run lint
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

# One allocated batch/version; only required platforms run, with independent gates.
.PHONY: release release-check mobile-ios-release
release:
	python3 tools/release.py --version "$(VERSION)"

release-check:
	python3 tools/release.py --version "$(VERSION)" --check

mobile-ios-release:
	python3 tools/release.py --version "$(VERSION)" --platform ios

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
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_*.py'

# Opt-in task-completion evals against a real model; never part of `check`.
# The engine serves the API without the UI, so a plain build is enough.
EVAL_RUNS ?= 2
.PHONY: eval
eval:
	@test -n "$(EVAL_PROVIDER)" || (echo "EVAL_PROVIDER=<provider id in ~/.zwai-swarm/config.yaml> is required" >&2; exit 2)
	go build -ldflags "$(LDFLAGS)" -o bin/zwai-eval ./cmd/zwai
	python3 tools/agent_eval.py --provider "$(EVAL_PROVIDER)" --runs "$(EVAL_RUNS)" $(if $(EVAL_TASKS),--tasks "$(EVAL_TASKS)") $(if $(EVAL_MODEL),--model "$(EVAL_MODEL)")

# The candidate is built/transferred once; each invocation still owns fresh state.
.PHONY: native-smoke-prepare
native-smoke-prepare:
	python3 "$(CURDIR)/tools/native_macos_smoke.py" --prepare-ax "$(CURDIR)/bin/native-macos-ax"

.PHONY: native-smoke
native-smoke:
	@test -n "$(APP)" -a -n "$(EVIDENCE)" || (echo "APP and a new EVIDENCE directory are required" >&2; exit 2)
	python3 "$(CURDIR)/tools/native_macos_smoke.py" --case --app "$(APP)" --evidence "$(EVIDENCE)" --timeout 50 --budget 60 --ax "$(CURDIR)/bin/native-macos-ax"

.PHONY: check
check: fmt-check vet docs-check test e2e

.PHONY: clean
clean:
	rm -rf bin $(FRONTEND)/test-results $(FRONTEND)/playwright-report $(FRONTEND)/.e2e-data
