---
name: requesting-tests
description: Use when every task of a plan with acceptance criteria is implemented, committed, and passing, before requesting review, or when asked to get independent tests for a plan.
---

# Requesting tests

The `test-writer` subagent writes tests from a plan's acceptance criteria and runs them on your committed work. It starts with no context, runs on a different model than you, and takes its expected results only from the plan, so its tests don't share your blind spots. You adopt its tests unchanged and fix your code until they pass. Every plan with acceptance criteria goes through this before review.

## Before you request

1. Every task in the plan is implemented and committed on the feature branch, the plan included, and `git status --porcelain` is empty. The test-writer reads only what's committed.
2. The project's normal test command passes.
3. Collect the fields, in template order:
   1. Repository: `git rev-parse --show-toplevel`
   2. Base: `git merge-base HEAD origin/main` (fall back to `main`, then `master`)
   3. Head: `git rev-parse HEAD`
   4. Plan: the plan file's path, relative to the repository

## Dispatch

Launch the `test-writer` subagent in the foreground. The prompt is this template with the fields filled in, and nothing else:

```text
Test request
Repository: <absolute path>
Base: <full base sha>
Head: <full head sha>
Plan: <repo-relative path to the plan file>
```

- Don't summarize your change, say what to test, or claim anything about the code. The test-writer reads the plan and the code itself.
- Don't set a `model` when launching it. Its model is pinned on purpose.

## Check the report

Check the whole report before you adopt anything:

1. The last line is the `OCTESTS` line, and its `head` is the Head you sent.
2. It has these sections, in order: Criteria, Failures, Plan gaps, Test files, How to run.
3. Criteria lists every acceptance criterion of the plan at Head exactly once, by the plan's number, with one result: PASSED, FAILED, GAP, or NOT_RUN. Count the plan's criteria yourself.
4. The `OCTESTS` counts match that list, and `passed + failed + gaps + not_run` equals the number of criteria. COMPLETE needs `not_run=0`.
5. There are test files if and only if some criterion PASSED or FAILED. Every path is relative, stays inside the repository, doesn't exist at Head, and appears only once, also when spelled differently.

If any check fails, request one new run with the same template. If that report fails a check too, stop and bring both reports to the user.

## Act on the report

Handle the report in this order.

1. **INCOMPLETE:** adopt nothing. If the cause is something you can fix, such as a missing tool or a flaky download, fix it, record the cause in the plan's Log, and request a new run, at most once per cause. If the cause isn't yours to fix, or the same cause comes back, stop and bring the report to the user.
2. **GAP:** adopt nothing yet. Bring every gap to the user with the test-writer's suggested wording. When the user changes the plan, write their answers into it as criteria, commit, and request a new run. When the user accepts a gap as it is, record that in the plan's Log and continue with this report.
3. **Adopt:** add every test file exactly as given, byte for byte, and commit only those files, for example `test: add tests for product pagination from its acceptance criteria`. Run them.
4. **Register:** if How to run says the project's normal test command doesn't run the new tests, register them in a separate commit that changes only the registration, such as the lines in `tests/run.sh`. Then run the normal test command and show that it runs every adopted test.
5. **FAILED:** fix the code until every adopted test and the normal test command pass. Follow the `systematic-debugging` skill when the cause isn't obvious. Never change a test's expected result, remove an assertion, or skip a test. Renaming and restyling to fit the project go in a separate later commit that leaves every assertion as it was.
6. **Disputes:** if you believe one of the tests contradicts the plan, stop and bring the user the test, the plan's text, and what the code does. Change the test only with the user's approval, in a commit of its own whose message states the ruling, and record the ruling in the plan's Log.
7. **Log:** append the outcome to the plan's Log, such as `Independent tests: 7 criteria, 5 passed, 2 failed; fixed the limit cap and past-the-end offsets`.

Never request a new run of a COMPLETE report just to get a different answer. Don't request review while a GAP, a disputed test, or an INCOMPLETE report is unresolved. When everything passes, continue with the `requesting-review` skill.

## Red flags

| Thought | Reality |
| --- | --- |
| "This test expects the wrong value; I'll fix it." | That's a dispute. Bring it to the user; change it only with the user's approval. |
| "I'll loosen this assertion so the suite goes green." | Fix the code. A weakened test hides the bug it found. |
| "I'll merge its tests into our existing test file." | Add the files exactly as given. Restyle later, in a separate commit, with every assertion unchanged. |
| "The gap is obvious; I'll pick a value myself." | Gaps are the user's decisions. Bring each one to the user. |
| "I'll tell the test-writer which edge cases matter." | That's framing. Use the template, nothing else. |
| "The report looks fine; no need to count the criteria." | A skipped criterion hides untested behavior. Check every criterion is listed. |
| "It passed when I ran it, so the normal suite runs it too." | Check How to run, register the tests if needed, and show the normal command running them. |
