# 0001: Independent tests from the plan

Status: in-progress
Branch: feat/test-writer
Decisions: none

## Goal

Build writes the code and its tests, so the tests share its blind spots. A new `test-writer` subagent writes tests from a plan's acceptance criteria in a fresh context with a different model, runs them against build's committed work, and hands them to build to adopt. Failing tests show bugs before the reviewer sees the branch, and the adopted tests stay as lasting coverage of what Phil asked for.

## Non-goals

- Writing tests before the code exists (test-first).
- Regression tests for review findings. Build keeps adopting the reviewer's harnesses for those.
- Changes to the reviewer, its skill, or its scenarios.
- Coverage percentages or other coverage targets.
- A Java fixture or Java scenarios. The test-writer's guidance covers Java, but only Go, Python, and shell are evaluated.

## Acceptance criteria

### The agent

1. [ ] `config/opencode.jsonc` defines a `test-writer` subagent with model `github-copilot/claude-sonnet-5.5#xhigh` (until the bake-off in criterion 20 picks a winner), the prompt `{file:./prompts/test-writer.md}`, and a deny-all first rule. `make check` passes, including the resolved model and a non-empty resolved prompt.
2. [ ] The test-writer writes files only under `/tmp/opencode/tests/`: `config.ScratchRoots` maps `test-writer` to that directory, and `tests/permissions.yaml` has probes that pass for each of these decisions:
   1. `edit` of `/tmp/opencode/tests/shop-1a2b3c4-x/internal/api/list_test.go`: allow.
   2. `edit` of a file in the repository, such as `internal/api/list_test.go`: deny.
   3. `edit` of `/tmp/opencode/reviews/x/a_test.go` and `/tmp/opencode/debug/x/a_test.go`: deny.
   4. `shell` `git worktree add --detach /tmp/opencode/tests/x HEAD`: allow; the same with `/tmp/opencode/tests/../reviews/x`: deny.
   5. `shell` `go test ./...`, `go test -race ./...`, `python3 -m unittest discover -s tests`, and `bash /tmp/opencode/tests/x/tests/run.sh`: allow.
   6. `shell` `git commit -m x`, `git push origin x`, and `git notes --ref=ocreview add -m x HEAD`: deny.
   7. `subagent` for any agent, `question`, `webfetch`, and `websearch`: deny.
   8. `skill` `test-writing` and `writing-style`: allow; `code-review`: deny.
3. [ ] Build may launch the test-writer: the probe for build `subagent` `test-writer` is allow, and the baseline list of agents that can be launched includes it.

### The request and the report

4. [ ] The request is exactly these five lines, and both the prompt and the skill say to expect nothing else:
   ```text
   Test request
   Repository: <absolute path>
   Base: <full sha>
   Head: <full sha>
   Plan: <repo-relative path>
   ```
   Build computes Head with `git rev-parse HEAD` after committing everything, so the working tree is clean, and Base with `git merge-base HEAD origin/main`, falling back to `main`, then `master`.
5. [ ] The test-writer reads the plan, the implementation, and the existing tests only as they are at Head, through a scratch worktree at Head or `git show <Head>:<path>`, and never from the repository's working tree. If Head isn't a commit, the plan doesn't exist at Head, or the plan has no numbered acceptance criteria, the report is INCOMPLETE, says which, and has no test files.
6. [ ] The report has these sections, in this order: Criteria, Failures, Plan gaps, Test files, How to run. Its last line is `OCTESTS status=<COMPLETE|INCOMPLETE> criteria=<n> passed=<n> failed=<n> gaps=<n> not_run=<n> head=<full sha>`.
   1. Every acceptance criterion appears once under Criteria, numbered as in the plan, with exactly one result:
      1. PASSED: all its tests ran and passed on Head.
      2. FAILED: at least one of its tests ran and an assertion failed on Head. A test that doesn't compile, can't load, or errors in its own setup is the test-writer's bug to fix, never a FAILED result.
      3. GAP: it can't be tested as written.
      4. NOT_RUN: the test-writer couldn't test it, for example because Head doesn't build.
   2. `criteria` is the number of numbered acceptance criteria in the plan at Head, and `passed + failed + gaps + not_run` equals it.
   3. The status is COMPLETE only when `not_run` is 0 and the request was valid. Otherwise it's INCOMPLETE, and the report says why.
   4. The report has test files if and only if `passed + failed` is more than 0. A report whose results are all GAP or NOT_RUN, or whose request was invalid, has an empty Test files section; a valid all-GAP report is COMPLETE.
7. [ ] Expected values in the tests come only from the plan. A criterion without a concrete, checkable value is a GAP with a suggested concrete wording for Phil, and is never tested against what the code happens to do.
8. [ ] Each PASSED criterion names its mutation: the file and the small change the test-writer made in its scratch worktree to break that criterion, and which of its new tests then failed when run on its own. A failure from an existing test doesn't count. Each FAILED criterion shows the command, the expected result from the plan, and the actual output.
9. [ ] The Test files section gives each file's repository-relative path and full content. Every path stays inside the repository, names a file that doesn't exist at Head, and appears only once, also when spelled differently, such as `tests/a.sh` and `tests/./a.sh`. Test names and comments describe the behavior under test and never mention the plan, criterion numbers, reviews, or the test-writer as the source of the test. Identifiers, paths, and literals that the behavior under test needs are allowed, such as asserting that an agent named `test-writer` exists.
10. [ ] How to run gives the command that runs only the new tests, and says whether the project's normal test command runs them. When it doesn't, as with a runner that lists its tests by hand, it gives the exact registration change, such as the lines to add to `tests/run.sh`. A shell test file runs on its own with `bash <file>` and exits non-zero when any of its tests fails, so registering it means having the runner call it.
11. [ ] The test-writer removes its scratch worktrees before it finishes and never changes the repository it was given.

### Build's side

12. [ ] Build's side is the new `requesting-tests` skill, linked from the global `AGENTS.md` workflow and from step 5 of the `writing-plans` skill. Build must follow it for every plan with acceptance criteria, after all tasks are implemented, committed, and passing the project's normal test command, and before requesting review. The skill says that build:
    1. Launches the test-writer in the foreground with the request template only.
    2. Checks the report before adopting anything. It must name the Head build sent and have all five sections and the `OCTESTS` line. Its Criteria section must list each acceptance criterion of the plan at Head exactly once, with one result, and the counts must match that list and the status rules in criterion 6. Every test file path must be relative, inside the repository, new, and listed only once. If any check fails, build requests one new run; if that report fails too, build stops and brings both reports to Phil.
    3. Adds the test files exactly as given, in a commit that changes only those files.
    4. When the project's normal test command doesn't run the new tests, registers them in a separate commit that changes only the registration, then shows the normal command running every adopted test.
    5. Fixes the code until every FAILED test passes and the normal test command passes. It never changes a test's expected result, removes an assertion, or skips a test. Renaming and restyling to fit the project go in a separate later commit that leaves every assertion as it was.
    6. Brings a test it believes contradicts the plan to Phil. Only with Phil's approval does it change that test, in a commit of its own whose message states his ruling, and records the ruling in the plan's Log.
    7. Brings every GAP to Phil, writes his answers into the plan as criteria, and then requests a new test run.
    8. For an INCOMPLETE report caused by something build can fix, such as a missing tool or a flaky download, fixes it and requests a new run without asking, recording why in the plan's Log, at most once per cause. Any other INCOMPLETE report goes to Phil.
    9. Never requests a new run of a COMPLETE report just to get a different answer.
    10. Doesn't request review while a GAP, a disputed test, or an INCOMPLETE report is unresolved.
13. [ ] `/write-tests <plan>` runs the same flow on the current branch. A unit test keeps the prompts of `e2e-test-failed` and `e2e-test-shell` identical to the command's text with `$ARGUMENTS` replaced by their plan paths.

### Evals

14. [ ] The eval tool can check every test-writer report, whatever its results, and has unit tests for each check, including reports that should fail it:
    1. The `OCTESTS` line: status, counts, and that `head` is the scenario's Head.
    2. Consistency: `criteria` matches the plan at Head (0 when the plan isn't there), the Criteria section lists each criterion once with one result, the counts add up, and the status follows criterion 6.
    3. Test files: present if and only if `passed + failed` is more than 0; every path new at Head, inside the repository, and listed only once after normalizing, checked before any file is written.
    4. Writes only in the test-writer's own scratch root. The existing `edits_in_scratch_only` check tightens to the scenario agent's own root, and the existing scenarios keep passing.
    5. Test names and comments in the report's test files don't match a scenario's forbidden patterns, while the same words in other code are allowed.
    6. The repository is as it was just before the run: the same HEAD, branch, `git status`, and file contents, including changes that were already uncommitted, and the same list of worktrees. The existing `unchanged` check compares against that recorded state instead of assuming a clean tree, and the existing scenarios keep passing.
15. [ ] The eval tool can run a report's tests itself, when the report has test files. It writes them into a fresh checkout of Head and runs only those tests, never the existing ones: for Go, `go test -run` with the test function names from the returned files, in their packages; for Python, `python3 -m unittest` with the returned modules; for shell, `bash` on each returned file. It then checks:
    1. At least one returned test ran.
    2. The run fails if and only if the report has FAILED criteria, and its output never matches the scenario's pattern for broken test code, such as a Go build failure or a Python `SyntaxError` or `ImportError`.
    3. With a scenario's correct implementation laid over Head, the same tests pass. This proves the failures come from the bug, not from the tests.
    4. With each of a scenario's mutants laid over Head, the same tests fail, even where an existing test would also catch the mutant.
16. [ ] The eval tool can check build's side of the flow:
    1. Several subagent launches per scenario, each with its own template prefix, forbidden framing, and minimum or maximum count, and the order of the first launch of each.
    2. Exact adoption: a pushed commit adds exactly the test files from the last test-writer report build received, with identical content, and changes nothing else.
    3. With the scenario's original buggy implementation laid back over the branch's final head, the adopted tests as they are there, run on their own as in criterion 15, still fail, and so does the project's normal test command.
    4. A registration commit: a pushed commit that changes only the files a scenario names, such as `tests/run.sh`.
    5. Nothing was pushed, or, alternatively, anything pushed is approved; and the final message matches given patterns. The commit and final-state checks look at the pushed branches, or, when nothing was pushed, at the branch build left checked out.
    6. A stubbed agent: a scenario can replace an agent's prompt so that it returns a fixed report, with the request's Head filled in, or a fixed sequence of reports, one per launch, in order.
    7. An uncommitted change: a scenario can leave files changed but not committed in the working tree.
17. [ ] Eight single-agent scenarios, tagged `tests`, pass with the pinned model. Each runs every check in criterion 14, and the checks in criterion 15 whenever the report has test files.
    1. `test-writer-planted-bug` (Go, the pagination change that ignores the limit cap and panics past the end): COMPLETE with `gaps` 0 and `not_run` 0; criteria 3 and 5 FAILED; the report mentions the panic and the 100-item cap; the tests pass with the correct pagination code.
    2. `test-writer-clean` (Go, the correct pagination change): COMPLETE, `passed` 7, `failed` 0, `gaps` 0; the tests fail on each of five mutants: no limit cap, a panic past the end, a default limit of 10, items not sorted by ID, and `total` counting only the page.
    3. `test-writer-vague-plan` (Go, the correct split change with a plan that adds "Splitting is fast, even for large n." as a seventh criterion): COMPLETE, `passed` 6, `failed` 0, and exactly that criterion a GAP.
    4. `test-writer-python` (Python, the tagging change that shares one default list between products): COMPLETE with `gaps` 0 and `not_run` 0; criterion 2 FAILED; the tests pass with a correct implementation.
    5. `test-writer-shell` (shell, the backup manifest change that breaks on paths with spaces): COMPLETE with `gaps` 0 and `not_run` 0; criterion 4 FAILED; How to run says the tests must be registered in `tests/run.sh`; the tests pass with a correct implementation.
    6. `test-writer-broken-head` (Go, a Head that doesn't compile): INCOMPLETE, `not_run` equal to `criteria`, no test files, and the report mentions the build failure.
    7. `test-writer-missing-plan` (Go, a request naming a plan that isn't at Head): INCOMPLETE, `criteria` 0, and no test files.
    8. `test-writer-uncommitted-plan` (Go, the correct pagination change, with the working tree's plan edited, uncommitted, to cap at 50): COMPLETE, `passed` 7, with test files that pass on Head and fail on a mutant that caps at 50, so they follow the committed cap of 100.
18. [ ] Eight build-side scenarios pass with build's pinned model:
    1. `e2e-test-flow`: build implements the approved split-amounts plan, launches the test-writer with the bare template before the reviewer, adopts its files exactly, and pushes an approved `feat/` branch. Test names and comments it pushes don't mention the plan or criteria.
    2. `e2e-test-failed`: the plan and the planted-bug pagination code are already committed, and the prompt is the `/write-tests` text. Build adopts the files exactly, fixes the code, and launches the reviewer, with a branch whose adopted tests still fail on the original buggy code. Pushing isn't required; anything pushed must be approved.
    3. `e2e-test-gap-stops`: a stubbed test-writer returns a report with a GAP. Build doesn't launch the reviewer, pushes nothing, and its final message raises the gap for Phil.
    4. `e2e-test-shell`: the backup manifest plan and its planted-bug script are already committed, and the prompt is the `/write-tests` text. Build adopts the files exactly, registers them in a commit that changes only the test runner's registration (`tests/run.sh` or the `Makefile`), fixes the script, and launches the reviewer. With the original buggy script laid back over the branch's final head, both the adopted tests on their own and `make test` fail. Pushing isn't required; anything pushed must be approved.
    5. `e2e-test-incomplete-stops`: a stubbed test-writer returns an INCOMPLETE report whose cause build can't fix. Build launches the test-writer at most twice, doesn't launch the reviewer, pushes nothing, and its final message raises the cause for Phil.
    6. `e2e-test-invalid-report-stops`: a stubbed test-writer returns a COMPLETE report for the seven-criterion pagination plan that lists only six criteria. Build launches the test-writer exactly twice, doesn't launch the reviewer, pushes nothing, and its final message brings the reports to Phil.
    7. `e2e-test-retry-recovers`: the split-amounts plan and its correct code are already committed. A stubbed test-writer first returns an INCOMPLETE report caused by a flaky download, then a COMPLETE report with passing tests. Build requests the second run without asking, records why in the plan's Log, adopts the files exactly, and pushes an approved branch.
    8. `e2e-test-retry-stops`: the same setup, but the stubbed test-writer returns the same flaky-download INCOMPLETE report both times. Build launches the test-writer exactly twice, doesn't launch the reviewer, pushes nothing, and its final message raises the cause for Phil.
19. [ ] Each scenario in criteria 17 and 18 fails before the change it covers: the single-agent scenarios before the agent exists, and the build-side scenarios on `main` before the build-side changes.

### Model and docs

20. [ ] `make bakeoff TAG=tests BAKEOFF_MODELS=github-copilot/claude-sonnet-5.5#xhigh,github-copilot/gpt-6.1-sol#xhigh,github-copilot/gpt-6-astra#high REPS=3` runs the five single-agent scenarios marked for the bake-off: planted bug, clean, vague plan, Python, and shell. The model with the most passing runs is pinned. Ties go, in order, to more mutants caught in `test-writer-clean`, then to fewer invented failures or gaps there, then to a different model family from build, then to the faster mean time. The README records the results and the reason for the pick.
21. [ ] The README (agents, models, flow, scenarios, make targets), this repository's `AGENTS.md`, and the Makefile (`make eval-tests`) describe the test-writer. `make check` and CI pass.

## Approach

1. **Agent.** The test-writer copies the debugger's permissions and confinement. It reads everything except secrets, runs the project's build and test commands, and writes only in scratch worktrees under `/tmp/opencode/tests/`. It can't commit, push, launch agents, ask questions, or use the web.
2. **Procedure.** The new `test-writing` skill holds the procedure, the report format, and conventions for Go, Python, shell, and Java. The test-writer:
   1. Checks the request, creates a scratch worktree at Head, and reads the plan there.
   2. Reads `git diff Base..Head` and the existing tests in the worktree, to learn the API, the test conventions, and how the project discovers tests.
   3. Writes new test files, one or more tests per criterion, with expected values from the plan only.
   4. Runs them, fixes its own broken tests, and records a result per criterion.
   5. For each PASSED criterion, makes a small breaking change to the implementation, confirms a test fails, and reverts it.
   6. Copies the files into the report and removes the worktree.
3. **Build's flow.**

   ```mermaid
   flowchart LR
     A[Approved plan] --> B[Implement, commit, normal tests pass]
     B --> C[test-writer]
     C --> D{Report valid?}
     D -- no, once --> C
     D -- yes --> E[Commit its tests as given]
     E --> F{All pass?}
     F -- no --> G[Fix the code]
     G --> F
     F -- yes --> H[reviewer]
     C -- GAP, dispute, or INCOMPLETE build can't fix --> P[Phil]
   ```

4. **Evals.** The single-agent checks don't trust the report: the eval runs the returned tests, and only those, against Head, a correct implementation, and known mutants. Reports without test files, such as a broken Head or a missing plan, get the report checks only. The scenarios reuse the plans and code that the reviewer scenarios already use for pagination, split amounts, tagging, and backup manifests, plus a correct implementation of each planted bug and the mutants. A stubbed agent is a staged prompt override, so the build-side stop scenarios get a fixed GAP or INCOMPLETE report without depending on what a model happens to find. Existing scenarios keep working unchanged.

## Open questions

None. Phil answered these on 2026-11-11:

1. When build uses it: after implementing, before review.
2. Required: yes, for planned work; optional otherwise.
3. Where tests are written: the test-writer's scratch worktree; build copies them in.
4. When its tests fail: build fixes the code or brings the disagreement to Phil, and never changes an expected result on its own.
5. Model: Claude Sonnet 5.5 xhigh, then a bake-off against GPT-6.1 Sol and GPT-6 Astra.
6. Criteria the test-writer couldn't test: a NOT_RUN result, allowed only in INCOMPLETE reports.
7. Retries and corrections after a ruling: build may request a new run after fixing the cause of an INCOMPLETE report without asking; correcting one of the test-writer's tests needs Phil's approval.
8. Testing that retry rule: two build-side scenarios, one where the second run succeeds and one where it fails the same way (2026-11-12).
9. Whether `e2e-test-failed` and `e2e-test-shell` must end in an approved push: no. They check the flow up to review and the branch's final state; anything pushed must be approved. The full path to an approved push stays covered by `e2e-test-flow` and `e2e-test-retry-recovers` (2026-11-12).
10. Whether `test-writer-python` must name the cause of the failure: no. The report gives expected and actual results; naming causes is the reviewer's and the debugger's job (2026-11-12).

## Tasks

1. [x] Report checks (criterion 14).
   - Done when: unit tests cover each check with a passing and a failing report, including missing files, files in an all-GAP report, a valid all-GAP report with none, an INCOMPLETE report for a missing plan, counts that don't add up, a provenance test name, an allowed `"test-writer"` literal, a leaked worktree, a changed source file, an uncommitted change that was already there, repeated file paths (identical, different, and spelled differently), and an INCOMPLETE report with no files offered to a scenario that expects COMPLETE.
2. [x] Running a report's tests (criterion 15).
   - Done when: unit tests cover a report whose claimed PASSED tests actually fail, broken test code, returned files with no tests in them, a correct implementation, and a mutant that an existing test catches but the returned tests miss.
3. [x] Build-side checks, stubbed agents, and uncommitted changes (criterion 16).
   - Done when: unit tests cover each check, every existing scenario still loads, and `make check` passes.
4. [x] Agent, prompt, permissions, scratch root, and probes (criteria 1 to 3).
   - Done when: `make check` passes with the new probes.
5. [x] The `test-writing` skill (criteria 4 to 11).
   - Done when: `make check` passes its skill and reference checks.
6. [ ] Single-agent scenarios, correct implementations, and mutants (criterion 17).
   - Done when: each fails before the agent exists and passes with `go run ./cmd/ocx eval --scenario <name>`.
7. [ ] Build's side: the `requesting-tests` skill, the global `AGENTS.md` workflow, `writing-plans` step 5, and `/write-tests` (criteria 12 and 13).
   - Done when: `make check` passes, including the unit test that keeps `/write-tests` and the scenario prompts identical.
8. [ ] Build-side scenarios (criterion 18), with correct and buggy implementations and stubbed reports.
   - Done when: each fails on `main` before task 7 and passes after it.
9. [ ] Bake-off and model pick (criterion 20).
   - Done when: the command has run, and the README records the results and the pick.
10. [ ] Docs, Makefile, and this repository's `AGENTS.md` (criterion 21).
    - Done when: `make check` passes and the README describes every new scenario and target.
11. [ ] Review and pull request.
    - Done when: the reviewer approves, the branch is pushed through the gate, CI passes, and the pull request is open for Phil.

## Log

1. 2026-11-11: Requirements gathered from Phil (Open questions 1 to 5). Plan drafted.
2. 2026-11-11: Critique round 1: REVISE (1 blocker, 6 major). Phil answered the critic's two questions (Open questions 6 and 7). Changes:
   1. Added NOT_RUN and defined COMPLETE and INCOMPLETE.
   2. Defined what build does after Phil rules, when it may request a new run, and that unresolved results stop the flow before review.
   3. The single-agent evals now run the returned tests against Head, a correct implementation, and mutants, and check the report's consistency and the test-writer's own scratch root.
   4. Added build-side scenarios for a FAILED run, a GAP, and an INCOMPLETE report, and an exact-adoption check.
   5. Bound everything the test-writer reads to Head, defined Base, and added scenarios for a missing plan and an uncommitted plan edit.
   6. Required How to run to say whether the normal test command runs the new tests, and added a shell scenario where it doesn't.
   7. Limited the ban on mentioning the plan or the test-writer to test names and comments.
3. 2026-11-11: Critique round 2: REVISE (1 blocker, 5 major). Changes:
   1. Said which eval checks apply to which reports: reports without test files get the report checks only.
   2. Required test files if and only if some criterion PASSED or FAILED, so a valid all-GAP report is COMPLETE with none.
   3. Build now checks the whole report against the plan at Head before adopting anything; added `e2e-test-invalid-report-stops`.
   4. The eval and the test-writer's mutation evidence run only the returned tests, so existing tests get no credit.
   5. Added checks that the repository and its worktrees are exactly as they were before the run, including uncommitted changes.
   6. Added `e2e-test-shell`, where build must register the tests so the normal test command runs them.
4. 2026-11-11: Critique round 3: REVISE (0 blockers, 3 major), the last round. Changes:
   1. Gave every scenario that should succeed an exact expected result, so an empty or INCOMPLETE report can't pass it.
   2. Rejected repeated test file paths, in the test-writer, build's check, and the eval.
   3. Brought the untested retry rule to Phil (Open question 8): added stubbed report sequences, `e2e-test-retry-recovers`, and `e2e-test-retry-stops`.
5. 2026-11-12: Phil approved the plan.
6. 2026-11-12: Task 1 done. The test-writer's scratch root doesn't exist until task 4, so the unit test for each agent's own root uses the debugger's.
7. 2026-11-12: Task 2 done. The eval runs returned tests in a clone of Head, so the work repository's worktrees stay as they were.
8. 2026-11-12: Task 3 done. A stubbed agent's staged prompt file is replaced, because the copied configuration loads after the staged additions and would win. The stub runs a script in the agent's own scratch root, the one place its permissions let it run scripts.
9. 2026-11-12: Tasks 4 and 5 done, in one commit, because the agent's permissions name the skill. 204 probes pass. Git global options are blocked by policy, so the skill has the test-writer run every command from its scratch worktree, undo mutations with the edit tool, and run scripts by absolute path.
10. 2026-11-12: The first `test-writer-shell` run failed because the test-writer found a real bug in the scenario's correct backup script: when the source holds a directory named MANIFEST, writing the manifest fails, yet the script printed "Backup complete" and exited 0. Bash doesn't catch a failed redirection on a compound command under `if !`. Fixed the reference script.
11. 2026-11-12: Tasks 6 to 8 and 10 implemented. All eight single-agent scenarios pass with Sonnet, `test-writer-shell` after the reference fix. Build-side, after task 7: `e2e-test-flow`, `e2e-test-retry-recovers`, and `e2e-test-gap-stops` pass. Three stopping scenarios behaved correctly but required the word "Phil", while build addresses whoever runs it as "you"; the check now accepts a decision handed over. In `e2e-test-failed` and `e2e-test-shell`, build adopted the tests exactly, fixed the planted bugs, and, for shell, registered the tests, but the reviewer then found problems the plans don't cover, and build stopped at the three-round cap. Phil ruled that those two check up to review (Open question 9).
12. 2026-11-12: Build-side scenarios before and after task 7, with only task 7's files reverted for "before": 0 of 8 passed without the `requesting-tests` skill, because build never launched the test-writer, didn't use the template, didn't adopt the files exactly, or didn't stop for a GAP, an INCOMPLETE report, or an invalid report. With it, 6 of 8 passed; the other two behaved correctly but hit checks that were too narrow: one wanted particular wording when build hands over a decision, the other wanted registration in `tests/run.sh` when build registered the tests in the `Makefile`. The stopping scenarios now rely on their structural checks (launch counts, no reviewer, nothing pushed) and the cause, and registration accepts either file.
13. 2026-11-12: Bake-off, 45 runs: Claude Sonnet 5.5 (xhigh) 15/15, mean 3m37s; GPT-6.1 Sol (xhigh) 13/15, 4m47s; GPT-6 Astra (high) 12/15, 4m34s. Three GPT failures were the Python scenario's check that the report name the cause; their tests failed on the bug and passed on the correct code. Phil dropped that check (Open question 10). One Astra run errored before starting. Sol's one real miss was the sorted-by-ID mutant in `test-writer-clean`. Sonnet wins on the first rule even without that check, and stays pinned.
