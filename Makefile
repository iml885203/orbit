.PHONY: build ui install clean test test-go test-ui test-ui-check test-ui-lint test-ui-unit test-e2e test-journeys test-journey-first-five-minutes test-journey-local-first-adoption test-journey-project-context-switch test-journey-recovery test-journey-startup-readiness test-install test-docs test-release release-check lint lint-filenames check-neutral setup fmt gen-types verify-types kafka-producer-image preflight vulncheck notice test-notice

# GOEXE is ".exe" on Windows, empty elsewhere. Without it the Windows build
# lands at bin/orbit and the daemon's os.Executable() self-exec fails with
# "executable file not found" — CreateProcess needs the .exe extension.
BINARY := orbit
GOEXE := $(shell go env GOEXE)
BUILD_DIR := ./bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"
# Go package tests and Vitest run together in `make test`. Capping Go's
# package fan-out avoids both runners oversubscribing the same machine.
GO_TEST_PARALLELISM ?= 4

ui:
	pnpm --dir ui run build

build: ui
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)$(GOEXE) ./cmd/orbit

# Install the dev build over the release installer's location. This target is
# intentionally opt-in and preserves the replaced binary for rollback.
install: build
	@set -eu; \
	install_dir="$(HOME)/.local/bin"; \
	target="$$install_dir/$(BINARY)$(GOEXE)"; \
	staged="$$target.new"; \
	mkdir -p "$$install_dir"; \
	cp "$(BUILD_DIR)/$(BINARY)$(GOEXE)" "$$staged"; \
	chmod +x "$$staged"; \
	codesign -s - -f "$$staged" 2>/dev/null || true; \
	if [ -f "$$target" ]; then cp -p "$$target" "$$target.prev"; fi; \
	mv -f "$$staged" "$$target"; \
	echo "Installed development build: $$target"; \
	"$$target" version; \
	echo "If Orbit is already running, restart it with: orbit daemon restart"

clean:
	rm -rf $(BUILD_DIR)

kafka-producer-image:
	docker build -f cmd/kafka-producer-sidecar/Dockerfile -t orbit-kafka-producer:local .

test:
	$(MAKE) -j2 test-go test-ui-unit

test-go:
	go test -p $(GO_TEST_PARALLELISM) ./...

test-ui:
	$(MAKE) test-ui-unit

test-ui-check:
	pnpm --dir ui run check

test-ui-lint:
	pnpm --dir ui run lint

test-ui-unit:
	pnpm --dir ui run test

# End-to-end tests against a real daemon + Docker. Uses the freshly built
# ./bin/orbit (skips tests if they detect container name collisions).
test-e2e: build
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) go test -tags=e2e -v -count=1 ./app/ -run E2E

# Installed-user journeys share one freshly built binary. Keep these out of the
# inner loop: they intentionally use real Git repositories, processes, and
# Docker to prove the product works beyond package boundaries.
test-journeys: build
	$(MAKE) -j2 test-journey-first-five-minutes test-journey-project-context-switch
	$(MAKE) -j4 test-journey-local-first-adoption test-journey-recovery test-journey-startup-readiness test-journey-runtime-adoption

test-journey-first-five-minutes:
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) ./scripts/test-first-five-minutes.sh

test-journey-local-first-adoption:
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) ./scripts/test-local-first-adoption.sh

test-journey-project-context-switch:
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) ./scripts/test-project-context-switch.sh

test-journey-recovery:
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) go test -tags=e2e -count=1 ./app -run '^TestE2E_(StatusBeforeInitPointsDirectlyToSetup|DaemonBackedCommandsChooseSetupOrStartupWithoutTransportDetails|ProjectSchemaMigrationDoesNotLoop|LiteralSingleServicePortInjectsPORT|UpdateReconnectsTheRunningEnvironment|StaleDaemonMetadataNeverKillsUnrelatedProcess|UpInfraReconcilesExternalRestart|CrashedServiceRecoveryIsLinearAndPreservesHealthyDependency|UpAppliesChangedConfigAndPreservesRunningIntent)$$'

test-journey-startup-readiness:
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) ./scripts/test-startup-readiness.sh

test-journey-runtime-adoption:
	ORBIT_BIN=$(abspath $(BUILD_DIR)/$(BINARY)$(GOEXE)) ./scripts/test-runtime-adoption.sh

test-install:
	@./scripts/test-install.sh
	@./scripts/test-uninstall.sh

test-docs:
	@ORBIT_DOCS_ONLY=1 ./scripts/test-first-five-minutes.sh
	@ORBIT_DOCS_ONLY=1 ./scripts/test-local-first-adoption.sh
	@ORBIT_DOCS_ONLY=1 ./scripts/test-project-context-switch.sh
	@./scripts/test-plugin-contract.sh
	@test ! -d docs/examples/mini-shop
	@grep -F 'https://github.com/iml885203/orbit-examples/tree/main/mini-shop' README.md >/dev/null
	@grep -F 'https://github.com/iml885203/orbit-examples/tree/main/mini-shop' README.zh-TW.md >/dev/null

test-release:
	@version="$$(node -e 'const fs=require("fs"); process.stdout.write(JSON.parse(fs.readFileSync(process.argv[1])).version)' plugins/orbit-agent/.codex-plugin/plugin.json)"; \
	./scripts/verify-release-candidate.sh "v$$version"

release-check:
	@./scripts/verify-release-candidate.sh "$(RELEASE_VERSION)"

# Called-symbol scan: an unreachable CVE in a transitive module is reported by
# govulncheck but does not fail the build, so a release is never blocked by a
# vulnerability this binary cannot reach.
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

notice:
	@./scripts/gen-notice.sh

# Not in preflight: go-licenses resolves every module and takes minutes, which
# would tax every commit to catch a dependency change. The CI job runs it.
test-notice:
	@generated="$$(mktemp -t notice)"; \
	trap 'rm -f "$$generated"' EXIT; \
	./scripts/gen-notice.sh "$$generated" >/dev/null; \
	if ! diff -u NOTICE "$$generated"; then \
		echo "NOTICE is stale — run 'make notice' and commit the result" >&2; \
		exit 1; \
	fi; \
	echo "NOTICE matches current dependencies"

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
	$(MAKE) ui
	$(MAKE) -j4 test-go test-ui-check test-ui-lint test-ui-unit
	go build ./...
	go vet ./...
	$(MAKE) test-install
	$(MAKE) test-docs
	$(MAKE) test-release
	$(MAKE) verify-types
	$(MAKE) check-neutral
	@echo "preflight OK - matches the CI gate"
