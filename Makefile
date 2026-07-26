.PHONY: build ui install clean test test-ui test-e2e test-install lint lint-filenames check-neutral setup fmt gen-types verify-types kafka-producer-image preflight

# GOEXE is ".exe" on Windows, empty elsewhere. Without it the Windows build
# lands at bin/orbit and the daemon's os.Executable() self-exec fails with
# "executable file not found" — CreateProcess needs the .exe extension.
BINARY := orbit
GOEXE := $(shell go env GOEXE)
BUILD_DIR := ./bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

ui:
	pnpm --dir ui run build

build: ui
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)$(GOEXE) ./cmd/orbit

# Install the dev build over the stable install.sh location. Opt-in: use
# this only when you want your local build to become your daily-driver
# orbit (it overwrites the installer's binary, but all state lives under
# ~/.orbit and is shared either way).
install: build
	@mkdir -p $(HOME)/.local/bin
	cp $(BUILD_DIR)/$(BINARY)$(GOEXE) $(HOME)/.local/bin/$(BINARY)$(GOEXE)
	@codesign -s - -f $(HOME)/.local/bin/$(BINARY)$(GOEXE) 2>/dev/null || true
	@echo "Installed $(HOME)/.local/bin/$(BINARY)$(GOEXE)"

clean:
	rm -rf $(BUILD_DIR)

kafka-producer-image:
	docker build -f cmd/kafka-producer-sidecar/Dockerfile -t orbit-kafka-producer:local .

test: test-ui
	go test ./... -v

# Frontend checks — typecheck, lint, unit tests. Part of `make test` so the
# standard verification path covers the dashboard, not just the Go side.
test-ui:
	pnpm --dir ui run check
	pnpm --dir ui run lint
	pnpm --dir ui run test

# End-to-end tests against a real daemon + Docker. Uses the freshly built
# ./bin/orbit (skips tests if they detect container name collisions).
test-e2e: build
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) go test -tags=e2e -v -count=1 ./app/ -run E2E

test-install:
	@./scripts/test-install.sh

lint: lint-filenames
	golangci-lint run ./...

# Zero-brand gate — core production code must stay feature-set neutral.
check-neutral:
	@./scripts/check-neutral.sh

# Enforce filename conventions from docs/CODE_CONVENTIONS.md §4.
lint-filenames:
	@./scripts/check-filenames.sh

run:
	go run ./cmd/orbit up

fmt:
	gofmt -w .

# Regenerate ui/src/lib/types/*.gen.ts from Go structs (one file per package;
# ui/src/lib/types.gen.ts is a hand-maintained barrel — see tygo.yaml).
# Pinned version: tygo truncate-writes per package, so behavior changes in
# newer releases could silently alter the output layout.
gen-types:
	go run github.com/gzuidhof/tygo@v0.2.21 generate

# CI helper: fail if generated types are out of sync with Go structs.
verify-types:
	@./scripts/verify-types.sh

setup:
	@echo "Installing git hooks..."
	@cp scripts/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Done. Pre-commit hook installed."

dev:
	go run ./cmd/orbit up --config envs/example.yaml

# Everything the CI pipeline gates on, runnable locally before a push.
preflight:
	pnpm --dir ui install --frozen-lockfile
	$(MAKE) ui test
	go build ./...
	go vet ./...
	$(MAKE) test-install
	$(MAKE) verify-types
	$(MAKE) check-neutral
	@echo "preflight OK - matches the CI gate"
