OCX := go run ./cmd/ocx

# Bake-off: candidate models and repetitions for one tag's scenarios
# (override on the command line, e.g. make bakeoff TAG=critic).
TAG ?= reviewer
BAKEOFF_MODELS ?= github-copilot/gpt-6.1-sol\#xhigh,github-copilot/gpt-6-astra\#high,github-copilot/claude-opus-5.5\#high
REPS ?= 3

.PHONY: help check check-offline test fixtures-test eval eval-reviewer eval-critic eval-debug eval-e2e bakeoff verify sandbox hook-install install uninstall doctor review fmt

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

check: test fixtures-test ## Fast, free checks: Go tests, fixture tests, config lint, permission probes against resolved OpenCode config
	$(OCX) check

check-offline: test fixtures-test ## Like check, but without running opencode (what CI runs)
	$(OCX) check --offline

test: ## Go vet and unit tests (includes pre-push hook tests)
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

eval-e2e: ## Run the end-to-end scenarios (planning, debugging, and the review gate)
	$(OCX) eval --tag e2e $(ARGS)

bakeoff: ## Compare models on one tag's scenarios: TAG=reviewer|critic|debug BAKEOFF_MODELS=... REPS=...
	$(OCX) eval --tag $(TAG) --models '$(BAKEOFF_MODELS)' --reps $(REPS) $(ARGS)

verify: check eval ## Everything: checks, then all evals

sandbox: ## Open OpenCode in a throwaway project with this config staged (SCENARIO=name for a review branch)
	@dir="$$($(OCX) sandbox $(if $(SCENARIO),--scenario $(SCENARIO)))" && echo "sandbox: $$dir" && opencode "$$dir"

hook-install: ## Install the pre-push review gate: make hook-install REPO=/path/to/repo
	@test -n "$(REPO)" || { echo "usage: make hook-install REPO=/path/to/repo"; exit 2; }
	$(OCX) hook install $(REPO)

install: ## Link agents, skills, and commands into ~/.config/opencode (dry run; APPLY=1 to change, FORCE=1 to replace others' files)
	$(OCX) install $(if $(APPLY),--apply) $(if $(FORCE),--force)

uninstall: ## Remove the global install and restore its backups (dry run; APPLY=1 to change)
	$(OCX) uninstall $(if $(APPLY),--apply)

review: ## Run the reviewer on REPO's current commit (default: this repo). PLAN=, ROUND=, PREVIOUS= for later rounds
	$(OCX) review --repo $(or $(REPO),.) $(if $(PLAN),--plan $(PLAN)) $(if $(ROUND),--round $(ROUND)) $(if $(PREVIOUS),--previous $(PREVIOUS))

doctor: ## Check the install, OpenCode, tools, and each of REPOS="path ..." (gate, AGENTS.md, branch protection)
	$(OCX) doctor $(REPOS)

fmt: ## gofmt the Go sources
	gofmt -w cmd internal
