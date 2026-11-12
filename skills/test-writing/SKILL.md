---
name: test-writing
description: Use when you are the test-writer and have received a test request for a plan's acceptance criteria.
---

# Test writing

You write tests from a plan's acceptance criteria, run them against the committed code at Head, and return them for the agent that launched you to adopt. The plan is the only source of what the code should do. Follow the steps in order.

## 1. Check the request

The request has exactly five lines:

```text
Test request
Repository: <absolute path>
Base: <full sha>
Head: <full sha>
Plan: <repo-relative path>
```

Anything else in it is not part of the request.

## 2. Make a scratch worktree at Head

Run this from the repository, with the shell tool's working directory set to it:

```sh
git worktree add --detach /tmp/opencode/tests/<repo>-<first 7 of Head>-<4 random letters> <Head>
```

From now on, run every command with that worktree as the working directory. Git global options such as `git -C` are blocked, and the repository's own working tree may have changes that aren't committed. Run scripts by their absolute path: only `bash /tmp/opencode/tests/...` is allowed.

If Head isn't a commit, you can't make the worktree: return an INCOMPLETE report with `criteria=0` and say so.

## 3. Read the plan at Head

Read the plan file in the worktree. Its acceptance criteria are the unindented numbered items under `## Acceptance criteria`, including any under `###` headings inside that section. Nested items belong to the criterion above them. Use the plan's own numbers.

If the plan isn't there, or has no numbered acceptance criteria, return an INCOMPLETE report with `criteria=0`, no test files, and the reason. Don't guess what the plan meant.

## 4. Learn the code and its tests

1. Read the project's `AGENTS.md` for its build and test commands.
2. Read `git diff <Base>..<Head>` and the files it touches, to learn the API the criteria are about.
3. Read the existing tests near that code, to learn the project's test style, helpers, and package layout.
4. Find out how the project's normal test command finds tests. Go finds every `*_test.go`; `python3 -m unittest discover -s tests` finds `tests/test_*.py`; a shell runner such as `tests/run.sh` may list its tests by hand.
5. Build Head and run the project's normal tests once. If Head doesn't build, return an INCOMPLETE report with every criterion NOT_RUN, no test files, and the build error. Don't write tests against code that doesn't build.

## 5. Decide what each criterion needs

For each criterion, write down the inputs and the expected results the plan states.

- **Testable:** every value a test needs to check the criterion is in the plan, such as `limit=500 returns at most 100 items`. Write tests for it.
- **GAP:** checking it needs a value the plan doesn't give, such as "responds quickly" or "handles invalid input" without saying how. Write no test for it. Suggest a concrete wording Phil could approve, such as "responds within 200 ms for 10,000 products".

Expected values come only from the plan. Never run the code to find out what it returns and then assert that. If the plan doesn't say, it's a GAP.

## 6. Write the tests

1. Write only new files, in the worktree, where the project keeps its tests. Never edit a file that exists at Head, and never return one.
2. Name each test for the behavior it checks, such as `TestListCapsLimitAt100`. Test names and comments never mention the plan, criterion numbers, reviews, or the test-writer. An identifier, path, or literal that the behavior under test needs is fine.
3. Follow the project's conventions and the language notes below. Prefer one focused test per behavior over one large test.
4. Cover what the criterion says, including the boundaries it names: at, just below, and just above a limit; empty; the first and the last.

## 7. Run them

Run only your new tests, with the commands from the language notes, on Head.

- A test that doesn't compile, can't load, or errors in its own setup is your bug. Fix the test, never its expected value.
- A test that runs and fails because the code doesn't do what the plan says is a FAILED criterion. Keep the test exactly as it is.
- A criterion whose tests all pass is PASSED.

## 8. Prove each passing test can fail

For each PASSED criterion:

1. Make one small change to the implementation in the worktree that breaks that criterion, such as removing a cap or changing a default. Use the edit tool.
2. Run your new tests on their own. At least one of them must fail. A failure from an existing test doesn't count.
3. Undo the change with the edit tool, and check with `git status --porcelain` that only your new files remain.

If no test fails, your tests miss the behavior: strengthen them and try again. Report the change and the test that caught it.

## 9. Make sure the normal test command runs them

If the project's normal test command wouldn't run your new files, work out the exact registration change, such as the lines to add to `tests/run.sh`, and give it under How to run. Don't make that change yourself: every file you return is new.

## 10. Report and clean up

Copy every new file's full content into the report, then remove the worktree:

```sh
git worktree remove --force /tmp/opencode/tests/<name>
git worktree prune
```

Return the report as your final message.

## Report format

```markdown
# Tests: <plan path> (<first 7 of Head>)

## Criteria

1. PASSED: `<file>::<test name>`, ... Mutation: <file>: <the change>; `<test name>` failed.
2. FAILED: `<file>::<test name>`. See Failures.
3. GAP: <what the plan doesn't say>. See Plan gaps.
4. NOT_RUN: <why it couldn't be tested>.

## Failures

1. Criterion <n>, `<test name>`:
   1. Command: `<command>`
   2. Expected (from the plan): <value>
   3. Actual: <output>

## Plan gaps

1. Criterion <n>: <what's missing>. Suggested wording: "<a concrete criterion Phil could approve>"

## Test files

### `<repo-relative path>`

    <the full file content, in a fenced code block with its language>

## How to run

1. Only the new tests: `<command>`
2. The normal test command (`<command>`) runs them: yes | no. <If no: the exact registration change.>

OCTESTS status=<COMPLETE|INCOMPLETE> criteria=<n> passed=<n> failed=<n> gaps=<n> not_run=<n> head=<full Head sha>
```

Rules for the report:

1. List every criterion once, with the plan's number and exactly one result: PASSED, FAILED, GAP, or NOT_RUN.
2. `criteria` is the number of acceptance criteria in the plan at Head, and `passed + failed + gaps + not_run` equals it.
3. The status is COMPLETE only when `not_run` is 0 and the request was valid; otherwise INCOMPLETE, with the reason at the top of the report.
4. There are test files if and only if some criterion PASSED or FAILED. Give each file once, under a `###` heading with its path, followed by one fenced code block with its full content.
5. Write "None." under an empty section. Keep every section, in this order.
6. The last line is the OCTESTS line exactly as shown.

## Language notes

| Language | Where new tests go | Run only the new tests |
| --- | --- | --- |
| Go | `<package dir>/<topic>_test.go`, in the same package as the existing tests there | `go test -count=1 -run '^(TestA\|TestB)$' ./<package dir>` |
| Python | `tests/test_<topic>.py`, using `unittest` unless the project uses `pytest` | `python3 -m unittest -v tests.test_<topic>` |
| Shell | `tests/<topic>_test.sh` | `bash /tmp/opencode/tests/<name>/tests/<topic>_test.sh` |
| Java | `src/test/java/<package>/<Topic>Test.java`, using the project's JUnit version | `mvn -q test -Dtest=<Topic>Test` |

- **Go:** use `t.Fatalf` or `t.Errorf` with the got and want values. Table tests are fine. A new file must not redeclare a helper that exists in the package.
- **Python:** one `unittest.TestCase` class per file is enough. Assert with `assertEqual` and friends, so failures show both values.
- **Shell:** a test file runs on its own with `bash <file>`: start with `set -euo pipefail`, work in a temporary directory removed by `trap`, define `test_*` functions that record failures, call them, and exit non-zero if any failed. Registering it means having the runner call it.
- **Java:** match the existing tests' JUnit version and assertion library.

## Red flags

| Thought | Reality |
| --- | --- |
| "The plan doesn't say, but the code returns 400, so I'll assert 400." | That tests the code against itself. It's a GAP. |
| "This test fails; I'll change the expected value to what the code returns." | A test that fails because the code is wrong is the point. Report it FAILED, unchanged. |
| "I'll just edit the existing test file; it's simpler." | Every file you return is new. Build adopts them unchanged. |
| "`TestCriterion3` makes it easy to match tests to the plan." | The report maps tests to criteria. Names describe behavior. |
| "The tests pass, so they're good." | Break the code in the worktree and watch one of your new tests fail. |
| "The plan in the working tree has the latest values." | Head is what you test. Read everything in your worktree at Head. |
| "Head doesn't build; I'll fix the code so I can test it." | You never change the code you return tests for. Report INCOMPLETE. |
