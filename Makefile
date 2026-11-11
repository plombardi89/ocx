OCX := go run ./cmd/ocx

# Bake-off: reviewer candidates and repetitions (override on the command line).
BAKEOFF_MODELS ?= github-copilot/gpt-6.1-sol\#xhigh,github-copilot/gpt-6-astra\#high,github-copilot/claude-opus-5.5\#high
REPS ?= 3

.PHONY: help check check-offline test eval eval-reviewer eval-e2e bakeoff verify sandbox hook-install fmt

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

check: test ## Fast, free checks: Go tests, config lint, permission probes against resolved OpenCode config
	$(OCX) check

check-offline: test ## Like check, but without running opencode
	$(OCX) check --offline

test: ## Go vet and unit tests (includes pre-push hook tests)
	@test -z "$$(gofmt -l cmd internal)" || { echo "gofmt needed:"; gofmt -l cmd internal; exit 1; }
	go vet ./...
	go test ./...

eval: ## Run every eval scenario (uses model tokens; ARGS="..." passes flags to ocx eval)
	$(OCX) eval $(ARGS)

eval-reviewer: ## Run the reviewer scenarios with the pinned reviewer model
	$(OCX) eval --tag reviewer $(ARGS)

eval-e2e: ## Run the end-to-end build -> review -> push scenario
	$(OCX) eval --tag e2e $(ARGS)

bakeoff: ## Compare reviewer models: BAKEOFF_MODELS=... REPS=...
	$(OCX) eval --tag reviewer --models '$(BAKEOFF_MODELS)' --reps $(REPS) $(ARGS)

verify: check eval ## Everything: checks, then all evals

sandbox: ## Open OpenCode in a throwaway project with this config staged (SCENARIO=name for a review branch)
	@dir="$$($(OCX) sandbox $(if $(SCENARIO),--scenario $(SCENARIO)))" && echo "sandbox: $$dir" && opencode "$$dir"

hook-install: ## Install the pre-push review gate: make hook-install REPO=/path/to/repo
	@test -n "$(REPO)" || { echo "usage: make hook-install REPO=/path/to/repo"; exit 2; }
	$(OCX) hook install $(REPO)

fmt: ## gofmt the Go sources
	gofmt -w cmd internal
