# ocx

OpenCode agents, skills, and commands for building complex software, with **independent, adversarial checks at both ends**: a plan critic before any code is written, and a review gate before anything is pushed. Both run on a different model family from the agent writing the code.

## How it works

```
you ──▶ /plan <the work>
          │
          ▼
        build (Claude Opus, built-in prompt)
          │  asks you the requirements questions, with suggested defaults
          │  drafts docs/plans/NNNN-<slug>.md
          ▼
        plan-critic subagent (GPT-6 Astra, fresh context, read-only)
          │  gets only: repository, plan, round
          │  checks every criterion can be verified, attacks it for missing
          │  edge cases, checks the plan against the actual code
          │  returns APPROVE or REVISE, plus questions for you
          ▼
        you approve the plan ──▶ build implements on feat/… or fix/… branches
          │
          ▼
        reviewer subagent (GPT-6.1 Sol, fresh context, cannot touch your tree)
          │  gets only: repository, branch, base, head, plan, round
          │  reads the diff and plan itself, runs the tests, attacks the change
          │  proves findings and checks claims with harnesses in a scratch
          │  worktree under /tmp/opencode/reviews (removed afterwards)
          │  records its verdict, with harness code, as a git note on the commit
          ▼
        git push ──▶ pre-push hook: rejects main/master and any commit
                     without an APPROVE note naming that exact commit
```

Independence comes from the setup, not from asking nicely:

| Weak point | How it's closed |
| --- | --- |
| Plans that can't be checked | The plan critic rejects vague criteria ("fast", "gracefully") and undefined edge cases before any code exists; you approve the written criteria |
| Author frames the review ("tests pass, just check style") | `requesting-review` and `writing-plans` give fixed request templates; the reviewer and critic treat anything extra as an unverified claim |
| Shared blind spots | Reviewer is pinned to a different model family than the driver |
| Findings argued away | Findings carry executed evidence where feasible: a `review_` harness that fails, with its code in the report |
| Author drops inconvenient findings | Findings are fixed or disputed to Phil, never dropped; 3-round cap, then escalate |
| Fixes without proof | Every BLOCKER and MAJOR fix needs a regression test; next round, the reviewer runs it on the previous commit and requires it to fail there |
| Author skips review | Pre-push hook requires an approval note; only the reviewer may write notes; policies block `--no-verify`, `git -c`, `git config`, hook edits, force pushes, main/master |
| Unreadable comments and docs | The writing rules apply to every agent; the reviewer reports clear breaks as WRITING findings, which block approval |
| Bugs patched at the symptom | `systematic-debugging` requires a failing reproduction and a step-by-step root cause before any fix; after three failed attempts, a consultant from the other model family gives a second opinion |
| TODOs slipping into main | The reviewer lists every new TODO for Phil; build opens the pull request as a draft, and only Phil can mark it ready |

## Writing rules

Every agent follows four rules for anything people read: code comments, docs, plans, commit messages, pull requests, review reports, and replies. The full text is in `config/global-AGENTS.md`; the `writing-style` skill has examples.

1. Code comments are short and explain why, not how. TODOs must stand on their own and go to Phil, who decides whether to fix or remove them; they block merging until he does.
2. Code comments stand on their own: no references to plans, reviews, tickets, or requests. Tests are named for the behavior they protect.
3. Plain English: simple accurate words, technical terms and uncommon abbreviations explained on first use, numbered lists or diagrams for steps (plain text in code, Mermaid allowed in docs).
4. Lists with a natural order are numbered 1, 2, 3, through the last item, most important first; nested lists too. Bullets only when there is no order.

The reviewer enforces these with a separate finding category:

| Category | Blocks approval | Needs a regression test |
| --- | --- | --- |
| BLOCKER | Yes | Yes |
| MAJOR | Yes | Yes |
| WRITING | Yes | No (the reviewer re-reads the fixed text) |
| MINOR | No | No |
| NIT | No | No |

`make check` applies the mechanical parts of the rules to this repository: no letter or roman-numeral list markers in our Markdown, and no Go or shell comments that point at plans, reviews, or requests.

## Review harnesses

The reviewer may write and run code to prove a point, but only in a detached scratch worktree of the reviewed commit under `/tmp/opencode/reviews/`. It never writes to the author's worktree, and harnesses are evidence, never fixes.

- **When:** a serious finding has only traced evidence; the request or code makes a checkable claim ("behavior unchanged", "thread-safe"); an acceptance criterion is unverified; or it doubts the author's tests (mutation check).
- **What:** `review_`-prefixed reproduction tests, exhaustive and fuzz tests, concurrent stress tests, old-vs-new comparisons, small programs and scripts, and time-boxed mutations of the production code to see whether the author's tests notice.
- **Report:** evidence is labelled `executed` or `traced`; a "Harnesses" section carries each harness's command, result, and code. The code lives in the review note, so the author can adopt it as a regression test and the next round can re-run it.
- **Later rounds:** each earlier BLOCKER/MAJOR must be fixed (earlier harness now passes) and covered by a regression test that fails on the previously reviewed commit. Missing or toothless regression tests are a MAJOR. If build thinks a test is impractical, that's a dispute for Phil.

## Layout

```
config/opencode.jsonc     agents, models, baseline permissions, policies (single source of truth)
config/global-AGENTS.md   global working agreements and writing rules (becomes ~/.config/opencode/AGENTS.md)
config/prompts/           agent system prompts, referenced with {file:./prompts/…}
skills/                   code-review, plan-critique, requesting-review, git-workflow, writing-plans,
                          writing-style, systematic-debugging, second-opinion
commands/                 /plan: plan work with a critique and your approval
                          /critique-plan <file>: run the plan critic yourself
                          /debug <problem>: reproduce, find the root cause, fix, review
                          /review: run the reviewer on the current branch yourself
hooks/pre-push            the review gate
templates/AGENTS.md       starting point for a project's AGENTS.md
tests/permissions.yaml    permission probes: "agent X doing Y must be allow/ask/deny"
evals/                    fixture project, scenarios, and results (results are gitignored)
cmd/ocx, internal/        Go tooling behind the Makefile
```

## Make targets

| Target | What it does | Cost |
| --- | --- | --- |
| `make check` | gofmt, vet, Go tests (incl. hook tests), config/skill/command lint, writing-rule lint, 103 permission probes against OpenCode's resolved config | free, ~seconds |
| `make check-offline` | Same without running OpenCode | free |
| `make eval-reviewer` | Reviewer scenarios with the pinned reviewer model | tokens |
| `make eval-critic` | Plan-critic scenarios, plus the end-to-end planning run | tokens |
| `make eval-debug` | Debugger and consultant scenarios, plus the end-to-end debugging run | tokens |
| `make eval-e2e` | End-to-end runs: planning flow, debugging flow, and implement → review → push through the gate | tokens |
| `make bakeoff` | One tag's comparison scenarios × `BAKEOFF_MODELS` × `REPS` (`TAG=reviewer`, `critic`, or `debug`) | tokens |
| `make verify` | `check` + every eval: the single "is everything good?" entrypoint | tokens |
| `make sandbox` | Throwaway copy of the fixture with this config staged; opens OpenCode in it (`SCENARIO=reviewer-planted-bug` to start on a review branch) | — |
| `make hook-install REPO=…` | Install the review gate into a repository | — |

Eval results go to `evals/results/<timestamp>/` (`summary.md`, plus per-run event streams, session exports, and final reports). Failing runs keep their workspace under `/tmp/opencode/ocx-eval/` for inspection.

| Scenario | What it tests |
| --- | --- |
| `reviewer-planted-bug` | Past-end panic and missing limit cap behind passing happy-path tests; needs executed evidence |
| `reviewer-clean-change` | A correct, well-tested change: approve with no invented problems |
| `reviewer-framing-resistance` | Check-then-act overselling race (invisible to `-race`) behind "tests green, keep it light" |
| `reviewer-equivalence-claim` | "Pure refactor" that parses 3 of 110 fractions a cent low; all existing tests pass |
| `reviewer-round2-fixed` | Round 2: fixes plus regression tests that fail on the round-1 commit: approve |
| `reviewer-round2-missing-test` | Round 2: correct fixes, no regression tests: request them |
| `reviewer-round2-weak-test` | Round 2: "regression tests" that also pass on the round-1 commit: catch it by running them there |
| `reviewer-python-planted-bug` | Python: a mutable default list shared by every product and mutated in place; the tests only ever add one product |
| `reviewer-shell-planted-bug` | Shell: dropped `set -euo pipefail` and quoting; with spaces in a path the copy fails but the script reports success and exits 0 |
| `reviewer-writing-rules` | Correct, tested code whose comments, test name, and doc break the writing rules: WRITING findings, and the one valid TODO listed for Phil |
| `critic-vague-plan` | Plan with uncheckable criteria ("fast", "gracefully") and an undefined matching rule: REVISE |
| `critic-missing-edge-cases` | Concrete plan whose own goal needs insufficient-stock, concurrency, and quantity rules it never states: REVISE |
| `critic-good-plan` | Complete, checkable plan: APPROVE with no invented gaps or questions |
| `e2e-review-gate` | build implements, requests review with the bare template, pushes only the approved commit; its added comments must stand on their own |
| `e2e-plan-flow` | build plans a feature, asks the critic with the bare template, revises, and writes no code |
| `debugger-unstable-sort` | Paging shows products twice or never: sorting by name leaves ties in random map order. Dedupe and `SliceStable` are wrong fixes; the debugger must find the tie and break it by ID |
| `consultant-second-opinion` | The same bug after three attempts that all looked elsewhere: the consultant must question their shared assumption |
| `e2e-debug-flow` | build reproduces the bug, fixes it in the store with a regression test, gets it reviewed, and pushes a `fix/` branch |

Fixtures: `go-service` (Go), `py-service` (Python, standard-library `unittest`), and `sh-tools` (bash). Each has a `make test` target, so no extra tools are needed.

Scenarios can stack several commits on a branch or on main, seed earlier review notes, and layer base overlays (`base_overlays:`, `commits:` with `overlays:` and `note:`); shared material lives in `evals/scenarios/_shared/`.

## Models

All models are set in `config/opencode.jsonc`. MAI models are never used; `make check` fails on any reference.

| Role | Model | Why |
| --- | --- | --- |
| Driver (`build`, session default) | `claude-opus-5.5` | Strongest on repository-scale coding (SWE-bench Pro 89.9%, FrontierCode, AA Intelligence Index 58) |
| `reviewer` | `gpt-6.1-sol#xhigh` | Different family from the driver; won our model comparison (below) |
| `plan-critic` | `gpt-6-astra#high` | Different family from the driver, and a different model from the reviewer; tied for best in our model comparison (below) |
| `debugger` | `claude-opus-5.5#xhigh` | Deep, long-context investigation; build applies its fix and the reviewer still checks it |
| `consultant` | `gpt-6.1-sol#xhigh` | Different family from the driver, so its second opinion doesn't share build's assumptions |
| `explore` | `claude-haiku-5.5` | Matches Opus on code-search recall, much faster |

Planned for later slices: `test-writer` (Sonnet 5.5 xhigh), `security-reviewer` (GPT-6 Astra high), `lead`/`implementer`.

### Reviewer model comparison

`make bakeoff` ran the 7 reviewer scenarios (before `reviewer-writing-rules` existed) 3 times each on 3 models (63 runs, 2026-11-11):

1. **GPT-6.1 Sol (xhigh) and GPT-6 Astra (high) tied**, with zero false findings on the clean change. In 2 of 3 runs each, they also found a real bug in the "correct" round-2 fix: `URL.Query` silently drops malformed pairs such as `limit=abc;`, so they became the default instead of a 400. Both proved it with an executed test. The fixture has since been fixed.
2. **Claude Opus 5.5 (high)** was about twice as fast, but approved that buggy fix in all 3 runs and added style findings to the clean change.

Sol stays the reviewer: it tied Astra on quality, and it is a different family from the Opus driver.

### Plan-critic model comparison

`make bakeoff TAG=critic` ran the 3 critic scenarios 3 times each on 3 models (27 runs, 2026-11-11). All 27 passed, so the finer signals decided:

1. **GPT-6 Astra (high)** found the most dangerous gap in the stock-reservation plan, that a negative quantity would *increase* stock, in 3 of 3 runs. It added one MINOR finding to the good plan in 2 of 3 runs.
2. **GPT-6.1 Sol (xhigh)** found that gap in 2 of 3 runs, and made no findings at all on the good plan.
3. **Claude Opus 5.5 (high)** was about 3 times faster but found that gap in only 1 of 3 runs, added 2 MINOR findings to the good plan every time, and is the same family as the driver.

Astra stays the critic: it ties Sol on quality, and using a different model at each gate makes a blind spot shared by the plan critic and the reviewer less likely.

### Debugger and consultant model comparison

`make bakeoff TAG=debug` ran the debugger and consultant scenarios 3 times each on 3 models (18 runs, 2026-11-11). Every run found the real cause: products with the same name tie, and map iteration order breaks the tie differently on every call. One Sol run worded it as "unspecified" order, which the check didn't accept at first; the check has since been widened. Most runs also ruled out `sort.SliceStable` as a fix, several by trying it.

| Role | Claude Opus 5.5 | GPT-6.1 Sol | GPT-6 Astra |
| --- | --- | --- | --- |
| Debugger (mean time) | 1m29s | 3m58s | 4m19s |
| Consultant (mean time) | 33s | 2m22s | 1m35s |

1. **Debugger stays on Opus:** equally correct and about 3 times faster. Build applies its fix and the reviewer, on GPT, still checks it.
2. **Consultant stays on Sol:** Opus was faster, but a second opinion is only useful if it doesn't share the stuck agent's assumptions, and build runs on Opus.

## Permissions model

- **Baseline (all agents):** shell asks by default; read-only git and build/test/lint commands are pre-approved; committing and pushing are denied (build re-allows them); review notes are read-only; `.env`/keys denied; `.git/` uneditable; outside directories ask; only `explore`, `plan-critic`, `reviewer`, `debugger`, and `consultant` can be launched as subagents.
- **Custom agents** start with a deny-all rule and allowlist what they need. `make check` enforces this; that only the reviewer (`/tmp/opencode/reviews/`) and the debugger (`/tmp/opencode/debug/`) write files, each only in its own scratch root; and that their shell allowlists stay within the read-only baseline. The reviewer's exceptions are its verdict note, scratch worktrees (`git worktree add --detach|remove --force` under the scratch root, path traversal denied), `go run`, and scripts inside its scratch worktree.
- **Policies** hard-deny what must never happen regardless of approvals: pushes to main/master, force pushes, `--no-verify`, ref deletion, git global options and environment variables that retarget commands (`git -c`, `git -C`, `--git-dir`, `GIT_DIR`, `GIT_CONFIG_*`), `git config` writes, `git update-ref`, touching hooks, `gh pr merge|review|ready`, `gh api`, `sudo`, publishing.

## Using it

**Today (not installed globally):**

1. Try it: `make sandbox`, then ask build for a change and to push it.
2. Try it on a real project: copy the project, run `go run ./cmd/ocx stage /path/to/copy` and `make hook-install REPO=/path/to/copy`, then run `opencode` there.

**Install globally (later):** link `config/opencode.jsonc`, `config/prompts/`, `skills/`, and `commands/` into `~/.config/opencode/`, link `config/global-AGENTS.md` as `~/.config/opencode/AGENTS.md`, and run `make hook-install REPO=…` in each project. For GitHub repos, also protect `main` (require a pull request, block force pushes; private repos need GitHub Pro). A required status check that fails on new TODOs would make the TODO merge block independent of draft status.

**Bypassing the gate yourself:** `git push --no-verify` from your own terminal. Agents can't.

## Conventions for adding to this repo

- **Agents:** define in `config/opencode.jsonc` with `description`, `mode`, a pinned `model`, `system: "{file:./prompts/<id>.md}"`, and permissions starting with `{"action": "*", "resource": "*", "effect": "deny"}`. Never replace a built-in agent's prompt; override permissions or model only.
- **Skills:** `skills/<id>/SKILL.md`, `name` equal to the directory, description starting "Use when…" that says only when to use it (not what it does: agents follow a summary instead of loading the skill). Supporting files in `references/`, `templates/`, `scripts/`.
- **References:** mention skills as `` `id` skill `` and agents as `` `id` subagent ``; `make check` verifies they exist.
- **Behavior changes:** add or update an eval scenario. Watch it fail before the change when you can, then pass after.
- **Permission changes:** add probes to `tests/permissions.yaml`.
- **Writing:** this repository follows its own writing rules. `make check` catches lettered lists and comments that point at plans or reviews; the rest is on whoever writes the text.
