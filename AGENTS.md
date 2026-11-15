# ocx

OpenCode agents, skills, and commands, plus the `ocx` binary that installs them with `ocx apply`. The binary plays no part in the development loop itself; the checks and evals that develop this repository run through the Makefile. `README.md` explains how the pieces fit together.

## Commands

- Check everything that's free (seconds): `make check`
- The same without running OpenCode, which is what CI runs: `make check-offline`
- Go vet and unit tests only: `make test`
- The eval fixtures' own tests only: `make fixtures-test`
- Evals, which spend model tokens: `make eval-reviewer`, `make eval-critic`, `make eval-debug`, `make eval-tests`, `make eval-e2e`, or one scenario with `make eval ARGS="--scenario <name>"`
- Compare models on one area: `make bakeoff TAG=reviewer` (or `critic`, `debug`, `tests`)
- Format Go code: `make fmt`
- Try this checkout's configuration in OpenCode on purpose: `make apply`; run `ocx apply` from a released binary to go back

## Layout

- `config/opencode.jsonc`: agents, models, permissions, and policies
- `config/prompts/`: agent system prompts, referenced with `{file:./prompts/<id>.md}`
- `config/global-AGENTS.md`: working agreements, installed as the global `AGENTS.md`
- `skills/`, `commands/`: installed as they are
- `docs/adr/`: the user's decisions that are in force; plans in `docs/plans/` are deleted once their work has merged
- `tests/permissions.yaml`: permission probes
- `evals/`: fixtures and scenarios; `evals/results/` is ignored by git
- `ocx.go`: embeds the configuration into the binary
- `cmd/ocx/`: the `ocx` binary (`apply`, `remove`, `update`, `version`); `internal/tools/eval/`: the eval tool behind `make eval`
- `internal/`: the apply and update packages, the checks (run as Go tests), and eval support
- `.github/workflows/release.yml`: builds `ocx` on every push to main and publishes it to the `latest` release
- `.github/workflows/check.yml`: CI

## Conventions

1. A change to how an agent, skill, or command behaves comes with an eval scenario that fails before the change and passes after it, when that's practical.
2. A change to permissions or policies comes with probes in `tests/permissions.yaml`.
3. A new custom agent has a deny-all first rule, a pinned model, and a `{file:}` prompt. An agent that writes files also needs its own scratch root in `config.ScratchRoots`.
4. This repository follows its own writing rules in `config/global-AGENTS.md`.

## Gotchas

- Eval runs use `opencode run --standalone` and spend model tokens. A full `make verify` takes 20 to 30 minutes.
- Each eval run copies the configuration when it starts. Don't edit the config, prompts, or skills while `make bakeoff` or another long eval run is going.
- A failing eval keeps its workspace under `/tmp/opencode/ocx-eval/` for inspection; passing ones are removed.
- Changes here go through the same loop as any project, so the reviewer and the other subagents need the configuration installed with `ocx apply`.
- Changes reach `main` through pull requests. CI runs `make check-offline` on them, but nothing waits on it.
- A Go test that runs OpenCode must skip unless `OCX_TEST_OPENCODE=1`, which `make check` sets. `make check-offline` points `OCX_OPENCODE` at `false`, so any test that runs OpenCode anyway fails there and in CI.
