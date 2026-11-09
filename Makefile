SHELL := /bin/bash
.DEFAULT_GOAL := help

# Pinned tool versions. Never float these: a moving tool turns a green build red
# with no code change, and nobody here reads code to find out why.
OAPI_CODEGEN     := github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
# golangci-lint runs from a pinned release binary when one is installed, and
# from its published image otherwise. Not `go install`: that resolves 30-odd
# analyser modules through sum.golang.org, and a transient error there fails the
# build with no code change.
#
# The binary exists so the pre-commit hook does not need a running Docker
# daemon. `make tools` fetches it, checksum verified, into .tools/.
GOLANGCI_VERSION := v2.5.0
GOLANGCI_IMAGE   := golangci/golangci-lint:$(GOLANGCI_VERSION)-alpine
GOLANGCI_BIN     := $(CURDIR)/.tools/golangci-lint
# sqlc runs from its published image. Building it from source pulls in
# pg_query_go, whose C sources do not compile against recent macOS SDKs.
SQLC_IMAGE       := sqlc/sqlc:1.27.0
GOOSE            := github.com/pressly/goose/v3/cmd/goose@v3.22.1
# gotestsum is a tool dependency of the module rather than a `go run pkg@version`.
# Run that way its own dependency tree is resolved fresh on every machine and
# recorded in nobody's go.sum, so this repository's Go and the pipeline's Go each
# built their own — and the pipeline's, being newer, could not compile the
# x/tools the pinned version asks for. `backend unit` was red for three days
# while `make verify` was green locally, which is the exact failure a pinned
# version is supposed to prevent.

COVERAGE_FLOOR   := 80

# One stack per checkout, so worktrees run `make verify` side by side.
#
# AVIOR_STACK names the compose project, and every published port is its base
# plus an offset. The main checkout is `avior` at offset 0, the ports it has
# always had; any other name defaults to the checkout's directory and an offset
# hashed from it. Two stacks sharing a port would collide loudly, but two
# sharing a project would share a database silently, which is why the name is
# derived rather than left to be remembered.
#
# Any of these may be given explicitly. The DSNs the acceptance suite reads all
# derive from AVIOR_PG_PORT unless every one of them is set; see
# backend/test/stackenv.
AVIOR_PG_BASE_PORT       := 55432
AVIOR_BACKEND_BASE_PORT  := 58080
AVIOR_FRONTEND_BASE_PORT := 5173
AVIOR_PREVIEW_BASE_PORT  := 4174

ifeq ($(strip $(AVIOR_STACK)),)
AVIOR_STACK := $(shell basename "$(CURDIR)" | tr 'A-Z' 'a-z' | sed -E 's/[^a-z0-9_-]+/-/g; s/^[^a-z0-9]+//')
endif
ifeq ($(shell printf '%s' '$(AVIOR_STACK)' | grep -Eq '^[a-z0-9][a-z0-9_-]*$$' && echo ok),)
$(error AVIOR_STACK='$(AVIOR_STACK)' is not a compose project name: lowercase letters, digits, - and _)
endif
ifeq ($(strip $(AVIOR_PORT_OFFSET)),)
ifeq ($(AVIOR_STACK),avior)
AVIOR_PORT_OFFSET := 0
else
AVIOR_PORT_OFFSET := $(shell printf '%s' '$(AVIOR_STACK)' | cksum | awk '{ print $$1 % 99 + 1 }')
endif
endif
ifeq ($(shell printf '%s' '$(AVIOR_PORT_OFFSET)' | grep -Eq '^[0-9]+$$' && echo ok),)
$(error AVIOR_PORT_OFFSET='$(AVIOR_PORT_OFFSET)' is not a number)
endif
ifeq ($(strip $(AVIOR_PG_PORT)),)
AVIOR_PG_PORT := $(shell echo $$(( $(AVIOR_PG_BASE_PORT) + $(AVIOR_PORT_OFFSET) )))
endif
ifeq ($(strip $(AVIOR_BACKEND_PORT)),)
AVIOR_BACKEND_PORT := $(shell echo $$(( $(AVIOR_BACKEND_BASE_PORT) + $(AVIOR_PORT_OFFSET) )))
endif
ifeq ($(strip $(AVIOR_FRONTEND_PORT)),)
AVIOR_FRONTEND_PORT := $(shell echo $$(( $(AVIOR_FRONTEND_BASE_PORT) + $(AVIOR_PORT_OFFSET) )))
endif
ifeq ($(strip $(AVIOR_PREVIEW_PORT)),)
AVIOR_PREVIEW_PORT := $(shell echo $$(( $(AVIOR_PREVIEW_BASE_PORT) + $(AVIOR_PORT_OFFSET) )))
endif
ifeq ($(strip $(AVIOR_E2E_BASE_URL)),)
AVIOR_E2E_BASE_URL := http://localhost:$(AVIOR_FRONTEND_PORT)
endif
ifeq ($(strip $(AVIOR_PROXY_TARGET)),)
AVIOR_PROXY_TARGET := http://localhost:$(AVIOR_BACKEND_PORT)
endif
AVIOR_LOCAL_STORE_DSN := postgres://avior:avior@localhost:$(AVIOR_PG_PORT)/avior?sslmode=disable

export AVIOR_STACK AVIOR_PORT_OFFSET AVIOR_PG_PORT AVIOR_BACKEND_PORT \
	AVIOR_FRONTEND_PORT AVIOR_PREVIEW_PORT AVIOR_E2E_BASE_URL AVIOR_PROXY_TARGET
export COMPOSE_PROJECT_NAME := $(AVIOR_STACK)

COMPOSE          := docker compose -p $(AVIOR_STACK)
BACKEND          := backend
FRONTEND         := frontend

export DOCKER_BUILDKIT := 1

GOLANGCI_DOCKER = docker run --rm \
	-v "$(CURDIR)/$(BACKEND)":/src \
	-v golangci-cache:/root/.cache \
	-w /src $(GOLANGCI_IMAGE) golangci-lint

# Resolved once, when make parses this file: the local binary if it is the
# pinned version, the image otherwise. A binary at the wrong version is ignored
# rather than used, because a linter that disagrees with the pipeline's is worse
# than no local linter.
GOLANGCI := $(shell \
	if [ -x "$(GOLANGCI_BIN)" ] && \
	   "$(GOLANGCI_BIN)" --version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_VERSION))"; then \
		echo 'cd $(BACKEND) && $(GOLANGCI_BIN)'; \
	else \
		echo '$(GOLANGCI_DOCKER)'; \
	fi)

# ---------------------------------------------------------------------------
# help
# ---------------------------------------------------------------------------

.PHONY: help
help:
	@echo "Avior — governed SQL gateway"
	@echo
	@echo "  make dev            start the whole stack locally (hot reload)"
	@echo "  make dev-down       stop the stack, keep volumes"
	@echo "  make dev-deps       rebuild the images after changing a dependency"
	@echo "  make dev-reset      stop the stack and destroy volumes"
	@echo "  make logs           tail every service"
	@echo "  make ps             show service state and health"
	@echo "  make stack-env      the stack this checkout drives: name and ports"
	@echo
	@echo "  make hooks          install the git hooks (pre-commit, pre-push)"
	@echo "  make tools          fetch the pinned linter so the hooks need no Docker"
	@echo
	@echo "  make verify         THE gate. Run this before claiming work is done."
	@echo "  make verify-fast    same minus acceptance and e2e (no Docker needed)"
	@echo "  make verify-deep    verify plus fuzz and mutation testing (nightly)"
	@echo "  make verify-precommit  what the pre-commit hook runs: lint and types"
	@echo "  make verify-prepush    what the pre-push hook runs: builds and unit tests"
	@echo
	@echo "  make gen            regenerate all generated code from its sources"
	@echo "  make fmt            format Go and frontend sources in place"
	@echo "  make migrate        apply store migrations to the local Postgres"
	@echo
	@echo "  make test-unit      Go unit tests plus Vitest"
	@echo "  make test-accept    integration suite against the real databases"
	@echo "  make test-e2e       Playwright against the running stack"
	@echo "  make test-perf      ADR 0009 P8 to P18, one worktree at a time across the machine"
	@echo "  make verify-budget  ADR 0009 P1 to P7 on a fresh production build (no Docker)"

# ---------------------------------------------------------------------------
# local stack
# ---------------------------------------------------------------------------

.PHONY: dev
dev: .env hooks
	$(COMPOSE) up --build -d
	@$(MAKE) --no-print-directory ps
	@echo
	@echo "stack     $(AVIOR_STACK)"
	@echo "frontend  http://localhost:$(AVIOR_FRONTEND_PORT)"
	@echo "backend   http://localhost:$(AVIOR_BACKEND_PORT)/api/v1/health"
	@echo "postgres  localhost:$(AVIOR_PG_PORT)"

# Adding a dependency needs this.
#
# The module directories live in named volumes so the container does not use the
# host's platform-specific binaries. A volume mounted over /app/node_modules
# shadows whatever the image holds, so rebuilding the image alone changes
# nothing: the volume has to go first. The symptom otherwise is Vite refusing to
# resolve an import that exists perfectly well on the host.
.PHONY: dev-deps
dev-deps:
	$(COMPOSE) rm -sf frontend
	-docker volume rm -f $(AVIOR_STACK)_frontend-node-modules
	$(COMPOSE) build frontend backend
	$(COMPOSE) up -d frontend backend
	@echo "waiting for the frontend to install and serve"
	@until curl -sf -o /dev/null http://localhost:$(AVIOR_FRONTEND_PORT)/; do sleep 1; done
	@echo "rebuilt with the current lockfiles"

.PHONY: dev-down
dev-down:
	$(COMPOSE) down

.PHONY: dev-reset
dev-reset:
	$(COMPOSE) down -v --remove-orphans

.PHONY: logs
logs:
	$(COMPOSE) logs -f --tail=100

.PHONY: ps
ps:
	@$(COMPOSE) ps --format 'table {{.Service}}\t{{.Status}}\t{{.Ports}}'

.PHONY: stack-env
stack-env:
	@echo "AVIOR_STACK=$(AVIOR_STACK)"
	@echo "AVIOR_PORT_OFFSET=$(AVIOR_PORT_OFFSET)"
	@echo "AVIOR_PG_PORT=$(AVIOR_PG_PORT)"
	@echo "AVIOR_BACKEND_PORT=$(AVIOR_BACKEND_PORT)"
	@echo "AVIOR_FRONTEND_PORT=$(AVIOR_FRONTEND_PORT)"
	@echo "AVIOR_PREVIEW_PORT=$(AVIOR_PREVIEW_PORT)"
	@echo "AVIOR_E2E_BASE_URL=$(AVIOR_E2E_BASE_URL)"
	@echo "AVIOR_PROXY_TARGET=$(AVIOR_PROXY_TARGET)"
	@echo "COMPOSE_PROJECT_NAME=$(COMPOSE_PROJECT_NAME)"

.env:
	cp .env.example .env
	@echo "created .env from .env.example"

# ---------------------------------------------------------------------------
# code generation — three chains, all drift-gated
# ---------------------------------------------------------------------------

.PHONY: gen
gen: gen-api gen-store gen-client gen-sql

.PHONY: gen-api
gen-api:
	cd $(BACKEND) && go run $(OAPI_CODEGEN) -config oapi-codegen.yaml ../api/openapi.yaml

# Mounts the repository root rather than backend/, because the schema sqlc reads
# is db/migrations, which sits outside the Go tree.
.PHONY: gen-store
gen-store:
	docker run --rm -v "$(CURDIR)":/src -w /src/$(BACKEND) $(SQLC_IMAGE) generate

.PHONY: gen-client
gen-client:
	cd $(FRONTEND) && pnpm run gen

# FEAT-076. The psql form of what Avior runs to prepare a database for writes.
.PHONY: gen-sql
gen-sql:
	cd $(BACKEND) && go run ./cmd/writesupport ../deploy/governed/write-support.sql

# A hand-edit to generated code is the cheapest way to make the frontend and
# backend disagree while every test still passes. This is the gate that catches it.
# ---------------------------------------------------------------------------
# hooks
# ---------------------------------------------------------------------------

# Points git at the tracked hooks directory. Version-controlled hooks are the
# whole reason for core.hooksPath: .git/hooks is per clone, so a hook that lived
# there would exist on whichever machine happened to install it and nowhere
# else.
#
# Idempotent and instant, so `make dev` depends on it.
# Fetches the pinned linter binary so the hooks run without Docker. Idempotent:
# an already-correct binary is left alone.
.PHONY: tools
tools:
	@if [ -x "$(GOLANGCI_BIN)" ] && \
	    "$(GOLANGCI_BIN)" --version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_VERSION))"; then \
		echo "tools: golangci-lint $(GOLANGCI_VERSION) already installed"; \
	else \
		$(BACKEND)/scripts/install-golangci.sh $(GOLANGCI_VERSION) "$(CURDIR)/.tools"; \
	fi

.PHONY: hooks
hooks:
	@git rev-parse --git-dir >/dev/null 2>&1 || exit 0; \
	if [ "$$(git config --get core.hooksPath || true)" != ".githooks" ]; then \
		git config core.hooksPath .githooks; \
		echo "hooks installed: pre-commit runs lint and types, pre-push runs builds and unit tests"; \
	fi; \
	if [ ! -x "$(GOLANGCI_BIN)" ]; then \
		echo "hooks: run 'make tools' so the Go linters run without Docker"; \
	fi

.PHONY: hooks-uninstall
hooks-uninstall:
	@git config --unset core.hooksPath || true
	@echo "hooks removed. The pipeline still runs every check."

# What the pre-commit hook runs. Everything a reviewer would have caught by
# reading, and nothing that needs a database or a browser.
#
# The Go linters run from pinned images. When the daemon is down they are
# skipped loudly rather than failing: a hook that dies with "Cannot connect to
# the Docker daemon" reads as a broken repository. A linter that runs and finds
# something still fails the hook — only its absence is tolerated, and the
# pipeline runs it either way.
.PHONY: verify-precommit
verify-precommit: verify-docs verify-vet verify-types verify-lint-web verify-fmt-web
	@if [ -x "$(GOLANGCI_BIN)" ] || docker info >/dev/null 2>&1; then \
		$(MAKE) --no-print-directory verify-lint-go verify-fmt-go; \
	else \
		echo "SKIP  golangci-lint and gofumpt did not run: no local binary and no"; \
		echo "      Docker daemon. Run 'make tools' to install it, or start Docker."; \
		echo "      The pipeline checks them either way."; \
	fi

# What the pre-push hook runs. Both builds, both unit suites, and the drift
# check: generated code landing out of step with its source is the failure
# nobody can read their way out of.
# The unit suites without Go coverage instrumentation. The coverage floor is a
# pipeline gate, not a hook's business: collecting profiles here would slow the
# push for a number nothing local reads.
.PHONY: verify-prepush
verify-prepush: verify-no-drift
	cd $(BACKEND) && go build ./...
	cd $(BACKEND) && go build -tags=acceptance ./test/...
	cd $(FRONTEND) && pnpm run build
	cd $(FRONTEND) && pnpm run budget
	cd $(BACKEND) && go test -race -count=1 ./...
	cd $(FRONTEND) && pnpm run test:unit

.PHONY: verify-no-drift
verify-no-drift: gen
	@if ! git diff --quiet --exit-code -- \
		'$(BACKEND)/internal/gen' '$(BACKEND)/internal/store/gen' '$(FRONTEND)/src/gen' \
		'deploy/governed'; then \
		echo ""; \
		echo "FAIL  generated code does not match its source."; \
		echo "      Someone hand-edited a generated file, or changed a source"; \
		echo "      without running 'make gen'. Diff:"; \
		echo ""; \
		git --no-pager diff --stat -- \
			'$(BACKEND)/internal/gen' '$(BACKEND)/internal/store/gen' '$(FRONTEND)/src/gen' \
		'deploy/governed'; \
		exit 1; \
	fi
	@echo "PASS  generated code is in sync"

# ---------------------------------------------------------------------------
# formatting
# ---------------------------------------------------------------------------

.PHONY: fmt
fmt:
	$(GOLANGCI) fmt
	cd $(FRONTEND) && pnpm run fmt

.PHONY: verify-fmt
verify-fmt: verify-fmt-go verify-fmt-web

.PHONY: verify-fmt-go
verify-fmt-go:
	$(GOLANGCI) fmt --diff
	@echo "PASS  gofumpt"

.PHONY: verify-fmt-web
verify-fmt-web:
	cd $(FRONTEND) && pnpm run fmt:check

# ---------------------------------------------------------------------------
# static analysis
# ---------------------------------------------------------------------------

.PHONY: verify-lint
verify-lint: verify-vet verify-lint-go verify-lint-web

.PHONY: verify-vet
verify-vet:
	cd $(BACKEND) && go vet ./...
	cd $(BACKEND) && go vet -tags=acceptance ./test/...

.PHONY: verify-lint-go
verify-lint-go:
	$(GOLANGCI) run

.PHONY: verify-lint-web
verify-lint-web:
	cd $(FRONTEND) && pnpm run lint

.PHONY: verify-types
verify-types:
	cd $(FRONTEND) && pnpm run typecheck

# ---------------------------------------------------------------------------
# tests
# ---------------------------------------------------------------------------

.PHONY: verify-docs
verify-docs:
	cd $(BACKEND) && go test -tags=docs -count=1 ./test/docs/...

.PHONY: test-unit
test-unit: test-unit-go test-unit-web

# Separately as well as together, because the pipeline runs whichever side a
# change touched. Each carries its own coverage floor: the Go one is the script
# below, the frontend one is vitest's own threshold.
.PHONY: test-unit-go
test-unit-go:
	cd $(BACKEND) && go tool gotestsum --format testname -- \
		-race -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: test-unit-web
test-unit-web:
	cd $(FRONTEND) && pnpm run test:unit

.PHONY: verify-coverage
verify-coverage:
	@cd $(BACKEND) && ./scripts/coverage-floor.sh coverage.out $(COVERAGE_FLOOR)

.PHONY: test-accept
test-accept:
	$(COMPOSE) -f docker-compose.yml -f docker-compose.test.yml up -d --wait \
		postgres
	# --build because the seed image carries db/migrations, copied in at build
	# time. Without it a new migration never reaches the database the acceptance
	# suite runs against, and the case that covers it fails for a reason that has
	# nothing to do with the code under test.
	$(COMPOSE) -f docker-compose.yml -f docker-compose.test.yml run --build --rm seed
	cd $(BACKEND) && AVIOR_ENV=test go test -race -tags=acceptance -count=1 ./test/acceptance/...
	# ADR 0009 P24 again, without -race: the race detector slows the parser's
	# one-off WebAssembly compile about tenfold, and the budget is the
	# production binary's, which is never built with it.
	# The case has to have run and passed: a -run pattern that matches nothing
	# exits 0, and a renamed case would otherwise drop the gate without a word.
	cd $(BACKEND) && AVIOR_ENV=test go test -v -tags=acceptance -count=1 \
		-run '^TestLineageFeature$$/^a_cold_build_finishes_within_two_seconds$$' ./test/acceptance/ \
		| tee /dev/stderr \
		| grep -q -- '--- PASS: TestLineageFeature/a_cold_build_finishes_within_two_seconds'

# The replay gate. ADR-0004 calls it the trust root: it is what makes a
# declarative file and a migration directory unable to drift apart without
# somebody being told. It needs a Postgres to replay into, which is why it sits
# beside the acceptance suite rather than in verify-fast.
.PHONY: verify-dbproject
verify-dbproject:
	$(COMPOSE) -f docker-compose.yml -f docker-compose.test.yml up -d --wait postgres
	cd $(BACKEND) && go run ./cmd/replaycheck \
		"$${AVIOR_STORE_DSN:-$(AVIOR_LOCAL_STORE_DSN)}" \
		../tools/dbproject

# The stack comes up first. Playwright drives the deployables through a browser,
# so it needs them served — and `make verify` calling this with nothing served
# fails in a way that reads as "the end-to-end tests are broken" rather than as
# "nobody started the application". Compose is idempotent, so this costs a
# developer who already has it running the second it takes to say so.
.PHONY: test-e2e
test-e2e: dev wait-stack
	cd $(FRONTEND) && pnpm exec playwright test --project chromium
	@$(MAKE) --no-print-directory test-perf

# ADR 0009 P8 to P18 against a production build. One worktree at a time across
# the machine: a budget measured beside another checkout's browser is a measure
# of both, so this waits for the lock rather than sharing the CPU.
.PHONY: test-perf
test-perf:
	cd $(FRONTEND) && ../tools/harness/exclusive.sh performance pnpm run test:perf

# `make dev` starts the stack and deliberately does not wait for it: the first
# run pulls images and builds the fixture database, and blocking on that would
# make an interactive command look hung.
#
# The e2e suite cannot afford the same courtesy, and the reason is sharper than
# slowness. The seed container drops and recreates the governed fixture
# database, so a suite that starts while it is still running is querying a
# database that is about to disappear — which is what made one case fail only
# inside `make verify`, where the acceptance suite has just rebuilt everything
# and `make dev` re-runs the seed. Waiting on the seed to exit is the fix; the
# health check alone answers a different question.
.PHONY: wait-stack
wait-stack:
	@echo "waiting for the seed"
	@for i in $$(seq 1 180); do \
		state=$$($(COMPOSE) ps -a --format '{{.State}}' seed 2>/dev/null | head -1); \
		if [ -z "$$state" ] || [ "$$state" = "exited" ]; then break; fi; \
		sleep 1; \
	done
	@echo "waiting for the stack"
	@for i in $$(seq 1 120); do \
		if curl -fsS http://localhost:$(AVIOR_BACKEND_PORT)/api/v1/health >/dev/null 2>&1 \
		   && curl -fsS $(AVIOR_E2E_BASE_URL)/ >/dev/null 2>&1; then \
			echo "  stack is up"; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "FAIL  the stack did not come up in two minutes"; exit 1

.PHONY: test-fuzz
test-fuzz:
	cd $(BACKEND) && go test -run=NONE -fuzz=FuzzParserNeverPanics -fuzztime=60s ./internal/sqlguard/
	cd $(BACKEND) && go test -run=NONE -fuzz=FuzzParserFailsClosed  -fuzztime=60s ./internal/sqlguard/
	cd $(BACKEND) && go test -run=NONE -fuzz=FuzzWriteParserNeverPanics -fuzztime=60s ./internal/sqlguard/
	cd $(BACKEND) && go test -run=NONE -fuzz=FuzzWriteParserFailsClosed -fuzztime=60s ./internal/sqlguard/
	cd $(BACKEND) && go test -run=NONE -fuzz=FuzzParseRoutineNeverPanics -fuzztime=60s ./internal/adapter/pgparse/

# ---------------------------------------------------------------------------
# the gate
# ---------------------------------------------------------------------------

# Everything that reads the code without running it, plus the drift check that
# has to come first: linting a generated file that is already wrong reports the
# wrong problem.
#
# Named because the pipeline runs exactly this as one job. A gate that existed
# only in CI would let work land locally and break there, and with nobody
# reading the code there would be no way to see why.
.PHONY: verify-static
verify-static: verify-no-drift verify-fmt verify-lint verify-types verify-docs
	@echo
	@echo "PASS  verify-static"

.PHONY: verify-fast
# ADR 0009 P1 to P7: what the build ships, from the build's own manifest. It
# builds first, because a budget read off a stale dist/ reads nothing.
.PHONY: verify-budget
verify-budget:
	cd $(FRONTEND) && pnpm exec vite build
	cd $(FRONTEND) && pnpm run budget

.PHONY: verify-fast
verify-fast: verify-static test-unit verify-coverage verify-budget
	@echo
	@echo "PASS  verify-fast"

# Everything verify runs except the browser.
#
# It exists because the release pipeline runs the end-to-end suite as a job of
# its own, and a job that restated the list would drift from this one. `verify`
# is still the whole of it and is still what "done" means locally — this is a
# subset with a name, not a second definition.
.PHONY: verify-headless
verify-headless: verify-fast verify-dbproject test-accept
	@echo
	@echo "PASS  verify-headless"

.PHONY: verify
verify: verify-headless test-e2e
	@echo
	@echo "PASS  verify — this work is done."

.PHONY: verify-deep
verify-deep: verify test-fuzz
	@echo
	@echo "PASS  verify-deep"

# ---------------------------------------------------------------------------
# store migrations
# ---------------------------------------------------------------------------

.PHONY: migrate
migrate:
	cd $(BACKEND) && go run $(GOOSE) -dir ../db/migrations postgres \
		"$${AVIOR_STORE_DSN:-$(AVIOR_LOCAL_STORE_DSN)}" up

.PHONY: migrate-status
migrate-status:
	cd $(BACKEND) && go run $(GOOSE) -dir ../db/migrations postgres \
		"$${AVIOR_STORE_DSN:-$(AVIOR_LOCAL_STORE_DSN)}" status
