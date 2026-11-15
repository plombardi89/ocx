EVAL := go run ./internal/tools/eval

# Bake-off: candidate models and repetitions for one tag's scenarios
# (override on the command line, e.g. make bakeoff TAG=critic).
TAG ?= reviewer
BAKEOFF_MODELS ?= github-copilot/gpt-6.1-sol\#xhigh,github-copilot/gpt-6-astra\#high,github-copilot/claude-opus-5.5\#high
REPS ?= 3

.PHONY: help check check-offline test lint fixtures-test eval eval-reviewer eval-critic eval-debug eval-tests eval-e2e bakeoff verify ocx apply fmt

# golangci-lint lints and formats the Go code, with the settings in
# .golangci.yml. It's installed into bin/ once per version, so everyone,
# and CI, runs the same one.
GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

# The checkout's commit, marked when it has uncommitted changes, for builds
# of ocx from it. Go records this itself, except in a git worktree.
COMMIT_FLAG = -X main.commit=$$(git rev-parse HEAD)$$(test -z "$$(git status --porcelain)" || echo -modified)
BUILD_OCX = go build -ldflags "$(COMMIT_FLAG)" -o

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

check: export OCX_TEST_OPENCODE = 1
check: test fixtures-test lint ## Fast, free checks: Go tests, fixture tests, config lint, permission probes against resolved OpenCode config, Go lint and formatting

# Pointing ocx at false makes anything that still runs opencode fail.
check-offline: export OCX_OPENCODE = false
check-offline: test fixtures-test lint ## Like check, but without running opencode (what CI runs)

test: ## Go vet and the Go tests, which include the checks of this repository's configuration, skills, and scenarios
	go vet ./...
	go test ./...

lint: $(GOLANGCI_LINT) ## Lint the Go code, including its formatting, with golangci-lint
	$(GOLANGCI_LINT) run

fixtures-test: ## Run the eval fixtures' own tests; scenarios assume the unchanged fixtures pass
	cd evals/fixtures/go-service && go test ./...
	PYTHONDONTWRITEBYTECODE=1 $(MAKE) -s -C evals/fixtures/py-service test
	$(MAKE) -s -C evals/fixtures/sh-tools test

eval: ## Run every eval scenario (uses model tokens; ARGS="..." passes flags, such as --scenario NAME or --list)
	$(EVAL) $(ARGS)

eval-reviewer: ## Run the reviewer scenarios with the pinned reviewer model
	$(EVAL) --tag reviewer $(ARGS)

eval-critic: ## Run the plan-critic scenarios, including the end-to-end planning run
	$(EVAL) --tag critic $(ARGS)

eval-debug: ## Run the debugger and consultant scenarios, including the end-to-end debugging run
	$(EVAL) --tag debug $(ARGS)

eval-tests: ## Run the test-writer scenarios, including build's side of the flow end to end
	$(EVAL) --tag tests $(ARGS)

eval-e2e: ## Run the end-to-end scenarios (planning, debugging, the test-writer flow, and review before pushing)
	$(EVAL) --tag e2e $(ARGS)

bakeoff: ## Compare models on one tag's scenarios: TAG=reviewer|critic|debug|tests BAKEOFF_MODELS=... REPS=...
	$(EVAL) --tag $(TAG) --models '$(BAKEOFF_MODELS)' --reps $(REPS) $(ARGS)

verify: check eval ## Everything: checks, then all evals

ocx: ## Build ocx from this checkout into OUT (default: a new temporary directory) and print its path
	@out="$(OUT)"; [ -n "$$out" ] || out="$$(mktemp -d)/ocx"; $(BUILD_OCX) "$$out" ./cmd/ocx && echo "$$out"

apply: ## Build ocx from this checkout and run ocx apply, to try a branch on purpose (ARGS="--dry-run" or "--config-dir DIR")
	@bin="$$(mktemp -d)/ocx" && $(BUILD_OCX) "$$bin" ./cmd/ocx && "$$bin" apply $(ARGS)

fmt: $(GOLANGCI_LINT) ## Format the Go code (gofumpt, goimports)
	$(GOLANGCI_LINT) fmt

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@
