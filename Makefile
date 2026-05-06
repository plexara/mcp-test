# mcp-test Makefile
#
# Common targets:
#   make build        # build the binary into ./bin/mcp-test
#   make ui           # build the React SPA into internal/ui/dist (embedded by build)
#   make test         # go test -race -count=1
#   make verify       # full CI-equivalent: tools-check, fmt, vet, embed-clean, test, lint, security, coverage
#   make dev          # postgres in compose + go run with live config
#
# Run `make help` to see every target.

SHELL := /bin/bash

BINARY_NAME := mcp-test

# Resolve VERSION to the latest annotated/lightweight git tag (e.g. v1.2.3),
# falling back to v0.0.0 when no tag exists yet. Append "-dirty" if the
# working tree has uncommitted changes.
VERSION    ?= $(shell \
    tag=$$(git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0); \
    git diff --quiet HEAD -- 2>/dev/null && echo $$tag || echo $$tag-dirty)
GIT_SHA    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-X github.com/plexara/mcp-test/pkg/build.Version=$(VERSION) \
                     -X github.com/plexara/mcp-test/pkg/build.Commit=$(GIT_SHA) \
                     -X github.com/plexara/mcp-test/pkg/build.Date=$(BUILD_DATE)"

CMD_DIR      := ./cmd/mcp-test
BUILD_DIR    := ./bin
UI_DIR       := ./ui
UI_EMBED_DIR := ./internal/ui/dist

# Pinned tool versions; keep in sync with .github/workflows/ci.yml.
GOLANGCI_LINT_VERSION := v2.11.4
GOSEC_VERSION         := v2.25.0

# Project-local tool dir. We pin lint/security tools here rather than
# relying on whatever's on $PATH; otherwise a developer's brew-installed
# (or newer GOPATH-installed) binary can pass `make verify` while the
# pinned CI version fails. The directory lives under $(BUILD_DIR) so it's
# already gitignored via /bin/.
TOOLS_DIR := $(abspath $(BUILD_DIR)/tools)

GO       := go
GOTEST   := $(GO) test
GOBUILD  := $(GO) build
GOMOD    := $(GO) mod
GOFMT    := gofmt
GOLINT   := $(TOOLS_DIR)/golangci-lint
GOSEC    := $(TOOLS_DIR)/gosec
GOVULN   := $(TOOLS_DIR)/govulncheck

.PHONY: all build test test-short bench fmt fmt-check vet tidy clean help dev-secrets \
        ui ui-dev ui-clean embed-clean \
        lint security gosec govulncheck \
        coverage coverage-gate coverage-report \
        dead-code mutate semgrep \
        verify tools-check tools-install \
        dev dev-anon dev-up dev-wait dev-ui-if-needed dev-down dev-logs \
        docker docs docs-serve screenshots run version

## all: Build, test, lint
all: build test lint

## build: Build the binary into ./bin/mcp-test
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Binary built: $(BUILD_DIR)/$(BINARY_NAME)"

## test: Run unit tests with race detector
test:
	@echo "Running tests..."
	$(GOTEST) -race -count=1 ./...

## test-short: Skip integration / long tests (-short)
test-short:
	$(GOTEST) -short -count=1 ./...

## bench: Run benchmarks
bench:
	$(GOTEST) -run=^$$ -bench=. -benchmem ./...

## fmt: Apply gofmt -s
fmt:
	@echo "Running gofmt..."
	$(GOFMT) -s -w .

## fmt-check: Fail if gofmt would change anything
fmt-check:
	@echo "Checking gofmt..."
	@out="$$($(GOFMT) -s -l .)"; \
	if [ -n "$$out" ]; then \
		echo "FAIL: files need 'make fmt':"; echo "$$out"; exit 1; \
	fi
	@echo "gofmt clean."

## vet: go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## tidy: go mod tidy
tidy:
	$(GOMOD) tidy

## ui: Build the SPA into internal/ui/dist for embedding
ui:
	@echo "Building UI..."
	cd $(UI_DIR) && pnpm install --frozen-lockfile && pnpm build
	@rm -rf $(UI_EMBED_DIR)
	@cp -R $(UI_DIR)/dist $(UI_EMBED_DIR)
	@echo "UI built and copied to $(UI_EMBED_DIR)."

## ui-dev: Run Vite dev server (proxies /api to localhost:8080)
ui-dev:
	cd $(UI_DIR) && pnpm dev

## ui-clean: Remove UI build artifacts
ui-clean:
	@rm -rf $(UI_DIR)/dist $(UI_DIR)/node_modules

## embed-clean: Reset internal/ui/dist to .gitkeep only (matches a clean CI checkout)
embed-clean:
	@echo "Cleaning UI embed directory..."
	@rm -rf $(UI_EMBED_DIR)
	@mkdir -p $(UI_EMBED_DIR)
	@touch $(UI_EMBED_DIR)/.gitkeep

## lint: golangci-lint run (pinned version from $(TOOLS_DIR))
lint: tools-check
	@echo "Running golangci-lint $(GOLANGCI_LINT_VERSION)..."
	$(GOLINT) run --timeout=5m

## gosec: Static security analyzer (pinned version from $(TOOLS_DIR))
gosec: tools-check
	@echo "Running gosec $(GOSEC_VERSION)..."
	$(GOSEC) -quiet ./...

## govulncheck: Known-vulnerability scan
govulncheck: tools-check
	@echo "Running govulncheck..."
	$(GOVULN) ./...

## security: gosec + govulncheck
security: gosec govulncheck

## codeql: Run the same CodeQL security-and-quality suite CI runs.
##         Requires the codeql CLI on PATH (brew install codeql or
##         download from https://github.com/github/codeql-cli-binaries).
##         Heavy (~3 min on first run, ~1 min cached). Not part of
##         `make verify` by default; run before opening a PR.
##
##         The config file at .github/codeql/codeql-config.yml is
##         the single source of truth for query exclusions; this
##         target uses the same file CI does so local results match.
CODEQL_DB     ?= $(BUILD_DIR)/codeql-db
CODEQL_RESULT ?= $(BUILD_DIR)/codeql-results.sarif
codeql:
	@command -v codeql >/dev/null 2>&1 || { \
		echo "FAIL: codeql CLI not on PATH."; \
		echo "  brew install codeql"; \
		echo "  (or fetch from https://github.com/github/codeql-cli-binaries/releases)"; \
		exit 1; \
	}
	@echo "Building CodeQL database (Go) at $(CODEQL_DB)..."
	@rm -rf $(CODEQL_DB)
	@mkdir -p $(BUILD_DIR)
	codeql database create $(CODEQL_DB) --language=go --source-root=. --overwrite
	@echo "Analyzing with security-and-quality + project config..."
	codeql database analyze $(CODEQL_DB) \
		codeql/go-queries:codeql-suites/go-security-and-quality.qls \
		--format=sarif-latest \
		--output=$(CODEQL_RESULT) \
		--threads=0 \
		--sarif-category=/language:go
	@echo ""
	@echo "Filtering against .github/codeql/codeql-config.yml exclusions..."
	@./scripts/codeql-gate.sh $(CODEQL_RESULT) .github/codeql/codeql-config.yml
	@echo "CodeQL: clean."

COVERAGE_MIN ?= 80

## coverage: Run tests and produce a per-package coverage profile.
##            Each package contributes coverage for its own statements;
##            cross-package union is intentionally NOT used here because
##            -coverpkg=./... interacts poorly with `go test ./...` profile
##            merging in this Go version, producing fractional coverage
##            entries for packages under test.
coverage:
	@echo "Running coverage..."
	$(GOTEST) -race -coverprofile=coverage.out -covermode=atomic ./...
	@$(GO) tool cover -func=coverage.out | tail -1

## coverage-gate: Fail if coverage of testable packages is below COVERAGE_MIN (default 80)
coverage-gate: coverage
	@./scripts/coverage-gate.sh coverage.out $(COVERAGE_MIN)

## coverage-report: Print per-package coverage and flag low-coverage funcs
coverage-report: coverage
	@echo ""
	@echo "=== Functions with 0% coverage (excluding postgres-dependent packages) ==="
	@$(GO) tool cover -func=coverage.out | grep -Ev "(cmd/mcp-test|pkg/apikeys|pkg/audit/postgres|pkg/database)" | awk '{gsub(/%/,"",$$3); if ($$3+0 == 0 && $$1 != "total:") print $$0}' || true
	@echo ""
	@echo "=== Functions below 60% (excluding 0%) ==="
	@$(GO) tool cover -func=coverage.out | grep -Ev "(cmd/mcp-test|pkg/apikeys|pkg/audit/postgres|pkg/database)" | awk '{gsub(/%/,"",$$3); if ($$3+0 < 60.0 && $$3+0 > 0 && $$1 != "total:") print $$0}' || true
	@echo "=== End coverage report ==="

## dead-code: Find unreachable functions (golang.org/x/tools/cmd/deadcode)
dead-code:
	@echo "Running deadcode..."
	deadcode ./...

## semgrep: SAST via Semgrep (Go ruleset)
semgrep:
	@echo "Running semgrep..."
	semgrep scan --config p/golang --error --quiet .

## mutate: Mutation testing via gremlins
mutate:
	@echo "Running gremlins (mutation testing)..."
	gremlins unleash --workers 4 --tags-filter "!integration" ./...

## tools-install: Install lint/security tools at the pinned versions into $(TOOLS_DIR).
##                 Idempotent; the stamp file skips reinstall when the pinned
##                 versions are already there. Delete bin/tools/.installed to
##                 force a reinstall (or just bump the pinned vars).
TOOLS_STAMP := $(TOOLS_DIR)/.installed-$(GOLANGCI_LINT_VERSION)-$(GOSEC_VERSION)
tools-install: $(TOOLS_STAMP)

$(TOOLS_STAMP):
	@echo "Installing pinned tools into $(TOOLS_DIR)..."
	@mkdir -p $(TOOLS_DIR)
	@rm -f $(TOOLS_DIR)/.installed-* 2>/dev/null || true
	GOBIN=$(TOOLS_DIR) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN=$(TOOLS_DIR) $(GO) install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	GOBIN=$(TOOLS_DIR) $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	@touch $@

## tools-check: Verify pinned lint/security tools are present at the right versions.
##              Auto-installs (via tools-install) when missing or stale.
tools-check: tools-install
	@echo "Tools pinned at $(TOOLS_DIR):"
	@echo "  golangci-lint: $$($(GOLINT) --version 2>/dev/null | head -1)"
	@echo "  gosec:         $$($(GOSEC) --version 2>/dev/null | head -1)"
	@echo "  govulncheck:   $$(test -x $(GOVULN) && echo present || echo MISSING)"
	@# Optional tools (informational); warn but don't fail.
	@which deadcode > /dev/null 2>&1 || echo "  (optional) deadcode not installed: go install golang.org/x/tools/cmd/deadcode@latest"
	@which semgrep  > /dev/null 2>&1 || echo "  (optional) semgrep  not installed: pip3 install semgrep"
	@which gremlins > /dev/null 2>&1 || echo "  (optional) gremlins not installed: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest"

## verify: Full CI-equivalent suite. Fails on any error including <80% coverage.
verify: tools-check fmt-check vet embed-clean test lint security coverage-gate coverage-report
	@echo ""
	@echo "=== verify: all checks passed ==="
	@# Pre-commit gate sentinel: record the current diff hash so the
	@# review-gate hook knows verify is green for this exact tree state.
	@mkdir -p .claude
	@{ git diff --cached HEAD 2>/dev/null; git diff 2>/dev/null; } \
		| shasum -a 256 | cut -c1-16 > .claude/.last-verify-passed

## dev: One-command full local stack; postgres + keycloak in docker, binary in foreground.
##      Builds the SPA into the embed dir if dist/index.html is missing so the
##      portal renders on first run. Generates .env.dev with random secrets on
##      first run (gitignored); subsequent runs reuse those so sessions persist.
dev: dev-secrets dev-up dev-wait dev-ui-if-needed
	@. ./.env.dev && \
	echo "" && \
	echo "Starting mcp-test (config: configs/mcp-test.live.yaml)..." && \
	echo "  Portal:    http://localhost:8080/portal/   (sign in with dev/dev or paste API key)" && \
	echo "  MCP:       http://localhost:8080/         (X-API-Key: \$$MCPTEST_DEV_KEY)" && \
	echo "  Keycloak:  http://localhost:8081/         (admin/admin)" && \
	echo "  API key:   $$MCPTEST_DEV_KEY" && \
	echo "" && \
	$(GO) run $(LDFLAGS) $(CMD_DIR) --config configs/mcp-test.live.yaml

## dev-anon: Run anonymous-mode dev binary (no Keycloak, no auth); fastest iteration
dev-anon: dev-secrets
	@. ./.env.dev && docker compose -f docker-compose.dev.yml up -d postgres
	@. ./.env.dev && $(GO) run $(CMD_DIR) --config configs/mcp-test.dev.yaml

## dev-secrets: Generate .env.dev with random cookie secret + dev API key on first run.
##              Re-run-safe; only writes if .env.dev is missing.
dev-secrets:
	@if [ ! -f .env.dev ]; then \
		echo "Generating .env.dev with random secrets (gitignored)..."; \
		printf 'export MCPTEST_COOKIE_SECRET=%s\nexport MCPTEST_DEV_KEY=%s\n' \
			"$$(head -c 48 /dev/urandom | base64 | tr -d '\n')" \
			"mcptest_$$(head -c 24 /dev/urandom | base64 | tr -d '\n=+/' | head -c 32)" \
			> .env.dev; \
		chmod 600 .env.dev; \
	fi

## dev-up: Start the dev stack (postgres + keycloak) without the binary.
##         Depends on dev-secrets because docker compose interpolates the
##         MCPTEST_COOKIE_SECRET reference at parse time even when the
##         mcp-test service isn't being started.
dev-up: dev-secrets
	@. ./.env.dev && docker compose -f docker-compose.dev.yml up -d postgres keycloak

## dev-wait: Block until postgres and keycloak are reachable.
##           Sources .env.dev because `docker compose exec` re-parses the
##           compose file and its interpolated MCPTEST_COOKIE_SECRET
##           reference must resolve.
dev-wait: dev-secrets
	@echo "Waiting for Postgres..."
	@. ./.env.dev && until docker compose -f docker-compose.dev.yml exec -T postgres pg_isready -U mcp >/dev/null 2>&1; do sleep 1; done
	@echo "Waiting for Keycloak realm..."
	@until curl -fs http://localhost:8081/realms/mcp-test/.well-known/openid-configuration >/dev/null 2>&1; do sleep 2; done
	@echo "Stack ready."

## dev-ui-if-needed: Build the SPA if internal/ui/dist/index.html is missing.
dev-ui-if-needed:
	@if [ ! -f $(UI_EMBED_DIR)/index.html ]; then \
		$(MAKE) ui; \
	fi

## dev-down: Stop the dev stack
dev-down: dev-secrets
	@. ./.env.dev && docker compose -f docker-compose.dev.yml down

## dev-logs: Tail compose logs
dev-logs: dev-secrets
	@. ./.env.dev && docker compose -f docker-compose.dev.yml logs -f --tail=100

## docker: Build the docker image (matches the goreleaser pipeline).
##         Builds the binary first, copies it where the goreleaser-style
##         Dockerfile expects it, then runs buildx for linux/amd64.
docker: build
	@mkdir -p linux/amd64
	@cp $(BUILD_DIR)/$(BINARY_NAME) linux/amd64/$(BINARY_NAME)
	docker buildx build --platform linux/amd64 \
		--build-arg TARGETARCH=amd64 \
		-t $(BINARY_NAME):$(VERSION) \
		--load .
	@rm -rf linux/

## docs: Build the documentation site (requires mkdocs-material).
docs:
	mkdocs build --strict

## docs-serve: Serve the documentation site locally. Default port is 8001
##             so it doesn't collide with the binary on 8080. Override with
##             DOCS_PORT. Bind address can be overridden with DOCS_HOST.
DOCS_HOST ?= 127.0.0.1
DOCS_PORT ?= 8001
docs-serve:
	mkdocs serve -a $(DOCS_HOST):$(DOCS_PORT)

## screenshots: Capture portal screenshots (light + dark) for the docs site.
##              Seeds mock audit data via the configured database, then drives
##              Playwright through every portal page. Requires the binary to
##              be running on $(SHOTS_BASE_URL) (default http://localhost:8080).
##              Re-run after any portal UI change.
SHOTS_BASE_URL ?= http://localhost:8080
SHOTS_API_KEY  ?= devkey-please-change
screenshots:
	@command -v node >/dev/null || { echo "node is required"; exit 1; }
	@cd scripts/screenshots && \
	    (test -d node_modules || npm install) && \
	    MCPTEST_BASE_URL=$(SHOTS_BASE_URL) MCPTEST_DEV_KEY=$(SHOTS_API_KEY) node screenshots.mjs

## run: Build and run
run: build
	$(BUILD_DIR)/$(BINARY_NAME) --config configs/mcp-test.dev.yaml

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

## version: Show resolved version metadata
version:
	@echo "Binary:     $(BINARY_NAME)"
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(GIT_SHA)"
	@echo "Build date: $(BUILD_DATE)"
	@echo "Go:         $$($(GO) version | cut -d ' ' -f 3)"

## help: Show this help
help:
	@echo "$(BINARY_NAME) Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
