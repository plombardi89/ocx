# ocx

OpenCode agents, skills, and commands, plus the Go tool `ocx` that checks, evaluates, installs, and diagnoses them. `README.md` explains how the pieces fit together.

## Commands

- Check everything that's free (seconds): `make check`
- The same without running OpenCode, which is what CI runs: `make check-offline`
- Go vet and unit tests only: `make test`
- The eval fixtures' own tests only: `make fixtures-test`
- Review the current commit before pushing, which spends model tokens: `make review`, then `make review ROUND=2 PREVIOUS=<sha>` after fixing findings
- Evals, which spend model tokens: `make eval-reviewer`, `make eval-critic`, `make eval-debug`, `make eval-e2e`, or one scenario with `go run ./cmd/ocx eval --scenario <name>`
- Compare models on one area: `make bakeoff TAG=reviewer` (or `critic`, `debug`)
- Format Go code: `make fmt`
- Check a real install and repositories: `make doctor REPOS="<path> ..."`

## Layout

- `config/opencode.jsonc`: agents, models, permissions, and policies
- `config/prompts/`: agent system prompts, referenced with `{file:./prompts/<id>.md}`
- `config/global-AGENTS.md`: working agreements, installed as the global `AGENTS.md`
- `skills/`, `commands/`: installed as they are
- `hooks/pre-push`: the review gate
- `tests/permissions.yaml`: permission probes
- `evals/`: fixtures and scenarios; `evals/results/` is ignored by git
- `cmd/ocx/`, `internal/`: the Go tool
- `.github/workflows/check.yml`: CI

## Conventions

1. A change to how an agent, skill, or command behaves comes with an eval scenario that fails before the change and passes after it, when that's practical.
2. A change to permissions or policies comes with probes in `tests/permissions.yaml`.
3. A new custom agent has a deny-all first rule, a pinned model, and a `{file:}` prompt. An agent that writes files also needs its own scratch root in `config.ScratchRoots`.
4. This repository follows its own writing rules in `config/global-AGENTS.md`.

## Gotchas

- Eval runs use `opencode run --standalone` and spend model tokens. A full `make verify` takes 20 to 30 minutes.
- Each eval run copies the configuration when it starts. Don't edit the config, prompts, or skills while `make bakeoff` or another long eval run is going.
- The global install links into this checkout. Running `make install APPLY=1` from a different checkout points the install there instead.
- A failing eval keeps its workspace under `/tmp/opencode/ocx-eval/` for inspection; passing ones are removed.
- The pre-push hook in this repository rejects any commit the reviewer hasn't approved. `make review` reviews only committed changes.
- Changes reach `main` through pull requests. CI runs `make check-offline` on them, but nothing waits on it.
- A Go test that runs OpenCode must skip unless `OCX_TEST_OPENCODE=1`, which `make check` sets. `make check-offline` points `OCX_OPENCODE` at `false`, so any test that runs OpenCode anyway fails there and in CI.
