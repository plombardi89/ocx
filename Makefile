OCX := go run ./cmd/ocx

# Bake-off: candidate models and repetitions for one tag's scenarios
# (override on the command line, e.g. make bakeoff TAG=critic).
TAG ?= reviewer
BAKEOFF_MODELS ?= github-copilot/gpt-6.1-sol\#xhigh,github-copilot/gpt-6-astra\#high,github-copilot/claude-opus-5.5\#high
REPS ?= 3

.PHONY: help check check-offline test fixtures-test eval eval-reviewer eval-critic eval-debug eval-tests eval-e2e bakeoff verify install uninstall doctor fmt

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

check: export OCX_TEST_OPENCODE = 1
check: test fixtures-test ## Fast, free checks: Go tests, fixture tests, config lint, permission probes against resolved OpenCode config
	$(OCX) check

# Pointing ocx at false makes anything that still runs opencode fail.
check-offline: export OCX_OPENCODE = false
check-offline: test fixtures-test ## Like check, but without running opencode (what CI runs)
	$(OCX) check --offline

test: ## Go vet and unit tests
	@test -z "$$(gofmt -l cmd internal)" || { echo "gofmt needed:"; gofmt -l cmd internal; exit 1; }
	go vet ./...
	go test ./...

fixtures-test: ## Run the eval fixtures' own tests; scenarios assume the unchanged fixtures pass
	cd evals/fixtures/go-service && go test ./...
	PYTHONDONTWRITEBYTECODE=1 $(MAKE) -s -C evals/fixtures/py-service test
	$(MAKE) -s -C evals/fixtures/sh-tools test

eval: ## Run every eval scenario (uses model tokens; ARGS="..." passes flags to ocx eval)
	$(OCX) eval $(ARGS)

eval-reviewer: ## Run the reviewer scenarios with the pinned reviewer model
	$(OCX) eval --tag reviewer $(ARGS)

eval-critic: ## Run the plan-critic scenarios, including the end-to-end planning run
	$(OCX) eval --tag critic $(ARGS)

eval-debug: ## Run the debugger and consultant scenarios, including the end-to-end debugging run
	$(OCX) eval --tag debug $(ARGS)

eval-tests: ## Run the test-writer scenarios, including build's side of the flow end to end
	$(OCX) eval --tag tests $(ARGS)

eval-e2e: ## Run the end-to-end scenarios (planning, debugging, the test-writer flow, and review before pushing)
	$(OCX) eval --tag e2e $(ARGS)

bakeoff: ## Compare models on one tag's scenarios: TAG=reviewer|critic|debug|tests BAKEOFF_MODELS=... REPS=...
	$(OCX) eval --tag $(TAG) --models '$(BAKEOFF_MODELS)' --reps $(REPS) $(ARGS)

verify: check eval ## Everything: checks, then all evals

install: ## Link agents, skills, and commands into ~/.config/opencode (dry run; APPLY=1 to change, FORCE=1 to replace others' files)
	$(OCX) install $(if $(APPLY),--apply) $(if $(FORCE),--force)

uninstall: ## Remove the global install and restore its backups (dry run; APPLY=1 to change)
	$(OCX) uninstall $(if $(APPLY),--apply)

doctor: ## Check the install, OpenCode, tools, and each of REPOS="path ..." (AGENTS.md, branch protection)
	$(OCX) doctor $(REPOS)

fmt: ## gofmt the Go sources
	gofmt -w cmd internal
