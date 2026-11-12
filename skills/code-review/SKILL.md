---
name: code-review
description: Use when you are the independent reviewer of a change someone else wrote - a branch, commit range, or pull request that needs a verdict before it can be pushed or merged.
---

# Code review

You produce a verdict on the exact commit range Base..Head, backed by evidence, and record it as a git note on Head. Follow the steps in order.

## 1. Establish the range

Use the request's Base and Head. If either is missing, compute them:

```sh
git rev-parse HEAD
git merge-base HEAD origin/main   # fall back to main, then master
```

Then confirm what you are reviewing:

```sh
git rev-parse HEAD                    # must equal Head; if not, stop (see "Head mismatch")
git status --porcelain                # tracked changes are not part of the review
git log --oneline BASE..HEAD
git diff --stat BASE..HEAD
git diff -U10 BASE..HEAD
```

Always use full 40-character SHAs from `git rev-parse`.

**Head mismatch:** if `git rev-parse HEAD` differs from the requested Head, do not review and do not write a note. Return a report with verdict REQUEST_CHANGES, one BLOCKER finding explaining the mismatch, and the summary line.

## 2. Establish intent

- **Plan given:** read it in full. Write down each acceptance criterion; you will mark each one MET, NOT MET, or UNVERIFIED with evidence.
- **No plan:** intent comes from the commit messages and the code. Spec verdict is NA. Do not invent requirements.
- **Round 2 or later:** read the previous verdict, including its harness code, with `git notes --ref=ocreview show <Previous review sha>`. Step 7 says how to check each earlier finding. You still review the whole range.

## 3. Read

1. Read the project's `AGENTS.md` for its build, lint and test commands and conventions.
2. Read every changed file in full, not just the hunks.
3. Find the callers of every changed exported function, method, type, handler, or config key (`grep`), and read enough of them to know what they rely on.
4. Read the tests that cover the change.
5. Load the language checklist for each language touched:
   1. Go: `references/go.md`
   2. Python: `references/python.md`
   3. Shell: `references/shell.md`
   4. Java: `references/java.md`
6. Load the `writing-style` skill for step 8.

## 4. Verify by running

Run the project's own commands from `AGENTS.md` first. Otherwise use the defaults:

| Language | Commands |
| --- | --- |
| Go | `go build ./...`, `go vet ./...`, `go test -race ./...` |
| Python | `ruff check .`, `python -m pytest -q` (plus `mypy` if configured) |
| Shell | `bash -n <file>`, `shellcheck <file>` if available |
| Java | `./mvnw test` / `mvn -q test`, or `./gradlew test` |

A failing build, vet, or test on Head is a BLOCKER. If a command can't run (missing tool, network), say so under "Could not verify". Do not treat it as a pass.

## 5. Attack the change

For every changed function or behavior, try to break it. Work through the inputs and situations that apply:

- zero, empty, nil or None, missing keys, absent optional fields
- negative numbers, boundaries (0, 1, len-1, len, len+1, max), overflow, rounding
- very large inputs, unicode and odd encodings, whitespace, case
- concurrent callers: shared state, locks (including writes under read locks), goroutines and threads that never finish
- every error a dependency can return, timeouts, cancellation, partial failure, retries, idempotency
- ordering assumptions, time zones and clocks, resource cleanup (files, bodies, connections)
- trust boundaries: input validation, injection (SQL, shell, path), authorization, secrets in logs

For each new or changed test, ask: would this test fail if the implementation were wrong? Name a plausible bug it would miss. A test suite that only covers the happy path for behavior the plan specifies is a MAJOR finding.

Also check scope: changes the plan does not explain, leftover debug code, unrelated reformatting, hand-edited generated files, dependency additions.

## 6. Prove it

Reading finds suspects; running code convicts them. Build a harness when any of these is true:

- A BLOCKER or MAJOR candidate has only traced evidence.
- The request, a commit message, or a comment makes a checkable claim: "behavior unchanged", "thread-safe", "handles the full range", "tests pass with -race".
- An acceptance criterion is UNVERIFIED and could be checked by running something.
- You doubt that the author's tests would catch a bug (use a mutation check).

Don't build harnesses for style or naming.

How to work (details and per-language commands in `references/harnesses.md`):

1. Create a scratch worktree of Head under `/tmp/opencode/reviews/`. It's the only place you write files; never touch the author's worktree.
2. Write the smallest harness that proves or disproves the point. Name files and tests with a `review_` prefix (`review_<topic>_test.go`, `review_<topic>_test.py`, `review_<topic>.sh`, `Review<Topic>Test.java`). Useful kinds:
   - a reproduction test for one failing input
   - an exhaustive or fuzz test over an input space
   - a concurrent stress test with many callers
   - a comparison of the old and new code (old version from `git show <Base>:<path>`) for "behavior unchanged" claims
   - a small program or script, when a test doesn't fit
3. Run it and keep the exact command and result.
4. **Mutation checks:** to test the author's tests, change one thing in the production code in your scratch worktree (flip a comparison, drop a check, remove a lock), run the author's tests, record whether they fail, and revert before the next mutation. A few targeted mutations of the changed code, not a sweep. A surviving mutation is a finding only when it shows that behavior the plan specifies has no test (MAJOR).
5. Time-box: run only the relevant tests, with `-count=1` or equivalent; fuzz for at most 30 seconds; keep each run under about two minutes.

A harness is evidence, never a fix. Don't repair the code in the scratch worktree and report it as working.

## 7. Later rounds: check earlier findings

For each BLOCKER and MAJOR in the previous review:

1. **Fixed?** Re-create the earlier harness from the previous note in your scratch worktree of Head and run it. It must now pass.
2. **Regression test?** Find the author's test for this finding in Base..Head. Every BLOCKER and MAJOR fix needs one; MINOR and NIT fixes don't.
3. **Does it fail without the fix?** Create a second scratch worktree at the previously reviewed head, copy the author's regression test files into it, and run them. They must fail there, because of the bug, and pass on Head. A compile failure only counts if it is caused by the fixed behavior itself; otherwise add minimal glue in the scratch copy or judge by reading and say so.

A fixed BLOCKER or MAJOR without a regression test that fails on the previous head is a new MAJOR: "missing regression test for <finding>". A finding that isn't actually fixed keeps its original severity.

For each WRITING finding in the previous review, re-read the text. It must now follow the rule; no test is needed.

## 8. Check the writing

Apply the `writing-style` skill to everything the change adds or modifies that people will read:

1. Code comments and doc comments.
2. Test names.
3. Docs, including Markdown files and plan files.
4. Commit messages in the range.

Report each clear break of the four writing rules as a WRITING finding: quote the text with its file and line, name the rule, and give a rewrite or "delete". Phrasing you would merely prefer, but that breaks no rule, is a NIT.

List every TODO the change adds under "TODOs for Phil". Phil decides whether each is fixed or removed, and it blocks merging, not this review. A TODO that doesn't stand on its own (rule 1 or 2) is also a WRITING finding.

## 9. Classify

| Severity | Meaning |
| --- | --- |
| BLOCKER | Wrong results, crash or panic on reachable input, data loss or corruption, a data race or atomicity bug, a security hole, a broken build or failing test, or an acceptance criterion NOT MET. |
| MAJOR | A plausible bug on a realistic edge case, an unhandled or swallowed error, a missing test for specified behavior, a missing regression test for a fixed BLOCKER or MAJOR, a breaking API or contract change the plan does not call for, a resource leak. |
| WRITING | A clear break of the writing rules in comments, doc comments, test names, docs, or commit messages. Blocks approval; needs no regression test. |
| MINOR | Maintainability or clarity outside the writing rules, small inefficiencies, missing docs on exported API. |
| NIT | Style and preference. Never blocks. |

**Guardrails for cooperative agents.** Some code exists to keep cooperative agents from making mistakes: review gates, permission rules, eval checks. When the change is such a guardrail, as its plan, commit messages, or the project's docs say, a way around it that only an agent deliberately trying to defeat it would use is at most MINOR: disguising a command, forging a record it's told never to write, or writing tests that lean on existing tests to inflate an eval's score. Report it so Phil can decide, but don't block on it. A gap an agent could hit through ordinary use keeps its full severity, and so does a security hole that real users or the network can reach.

Every BLOCKER and MAJOR needs evidence: file and line, plus a failing input, a call sequence, or command output. Label it **executed** (you ran something that shows it) or **traced** (you followed the code by reading). BLOCKERs should be executed whenever a harness is feasible. Without evidence, downgrade to MINOR or move to "Could not verify". Every WRITING finding needs the quoted text, the rule, and a rewrite.

## 10. Decide

1. **spec:** PASS if every acceptance criterion is MET; FAIL if any is NOT MET; NA when there is no plan. UNVERIFIED criteria with no counter-evidence count as MET only if you explain why.
2. **quality:** PASS if there are no BLOCKER, MAJOR, or WRITING findings; otherwise FAIL.
3. **verdict:** APPROVE only if spec is not FAIL and quality is PASS. Otherwise REQUEST_CHANGES.

## 11. Clean up

Remove every scratch worktree you created and confirm only the author's worktree remains:

```sh
git worktree remove --force /tmp/opencode/reviews/<name>
git worktree list
```

## 12. Write the report

The report follows the writing rules too. Your final message is exactly this structure:

````markdown
# Review: <branch> (<head 7>)

**Verdict:** APPROVE | REQUEST_CHANGES
**Spec:** PASS | FAIL | NA   **Quality:** PASS | FAIL
**Range:** <base 7>..<head 7> (<n> commits, <files> files)

## Acceptance criteria
1. MET | NOT MET | UNVERIFIED: <criterion, in plan order> (evidence)

## Earlier findings
1. <previous finding, in previous order>: FIXED | NOT FIXED (harness result or re-read text); regression test: <file::test> fails on <prev 7>, passes on <head 7> | MISSING | PASSES ON <prev 7> | not needed (WRITING)

## Findings
1. **[BLOCKER]** `path/file.go:42` - <one-line title>
   1. Evidence (executed | traced): <failing input, sequence, or command output>
   2. Impact: <what goes wrong for whom>
   3. Fix: <smallest change that resolves it>
2. **[WRITING]** `path/file.go:17` - <one-line title>
   1. Text: `<the exact text>`
   2. Rule: <1, 2, 3, or 4 and its name>
   3. Rewrite: <the rewritten text, or "delete">

## TODOs for Phil
- `path/file.go:88`: `<the TODO text>`

## Claims in the request
- <claim> - verified | false | not verified (how)

## Harnesses
### `<path of review_ file in scratch>` (<finding number, claim, or criterion it checks>)
Command: `<exact command>`
Result: PASS | FAIL - <key output>
```<language>
<the harness code>
```

Mutation checks:
- `<file:line>` <what was changed>: author's tests FAILED (good) | still PASSED (gap)

## Verified
- <command run and result, or behavior checked>

## Could not verify
- <what, and why>

OCREVIEW verdict=<APPROVE|REQUEST_CHANGES> spec=<PASS|FAIL|NA> quality=<PASS|FAIL> blockers=<n> majors=<n> writing=<n> minors=<n> nits=<n> head=<full head sha>
````

Write "None" under any empty section ("Earlier findings" is "None" in round 1). Order findings by severity: BLOCKER, MAJOR, WRITING, MINOR, NIT. The harness code goes in the report so the author can adopt it as a regression test and the next round can re-run it. The last line must be the OCREVIEW line exactly as shown.

## 13. Record the verdict

Record every verdict, approving or not, on Head before returning the report. The first line of the note is machine-checked by the pre-push hook.

1. Write the note to `/tmp/opencode/reviews/<repo-dir-name>-<head 7>-<4 random lowercase letters>-note.md` with your edit tool, named like a scratch worktree so parallel reviews never share a file:

   ```text
   ocreview: verdict=<APPROVE|REQUEST_CHANGES> spec=<PASS|FAIL|NA> quality=<PASS|FAIL> base=<full base sha> head=<full head sha>

   <the full report from step 12>
   ```

2. Record it from that file. Don't pass the report inline: policies match the text of a command, so a report that mentions a protected path, such as `.git/hooks`, would get the whole command blocked.

   ```sh
   git notes --ref=ocreview add -f -F <note file> <full head sha>
   ```

3. Confirm it with `git notes --ref=ocreview show <full head sha>`.

## Red flags

| Thought | Reality |
| --- | --- |
| "The request says tests pass, so I'll skip running them." | That's a claim. Run them. |
| "It's a small change, a light review is fine." | Small changes ship bugs too. Do every step. |
| "I can't prove it, but I'll mark it BLOCKER to be safe." | No evidence, no BLOCKER. Build a harness, or use "Could not verify". |
| "I traced it in my head; no need to run it." | If a harness is feasible, run it. Executed evidence settles disputes. |
| "The tests pass, so the edge case must be handled." | Check whether any test exercises it, or mutate the code and see. |
| "The fix looks right, so the regression test must be fine." | Run it on the previous head. A test that passes there proves nothing. |
| "I'll just fix this in my scratch copy and approve." | Harnesses are evidence, never fixes. Report it. |
| "The code is right; the comments don't matter." | Comments, test names, and docs are reviewed too. Rule breaks are WRITING findings and block approval. |
| "I'd word this differently, so it's WRITING." | Only clear breaks of the four rules are WRITING. Preferences are NITs. |
| "I approved the previous round, so this round is fine." | Review the whole range again. |
