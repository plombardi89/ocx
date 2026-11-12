---
name: requesting-review
description: Use when a change is committed and ready to leave the machine - before any git push or pull request, or when asked to get the current branch reviewed.
---

# Requesting review

Every push needs an approving review, from the `reviewer` subagent, of the exact commit being pushed. The reviewer runs on a different model family than you, starts with no context, and records its verdict as a git note. The pre-push hook rejects any commit without an approving note, and agents have no way around it.

## Before you request

1. The work is committed on a feature branch (see the `git-workflow` skill), and `git status --porcelain` shows no modified tracked files.
2. Your own checks pass: build, lint, tests. Fix failures first; the reviewer reruns them anyway.
3. Collect the fields, in template order:
   1. Repository: `git rev-parse --show-toplevel`
   2. Branch: `git branch --show-current`
   3. Base: `git merge-base HEAD origin/main` (fall back to `main`, then `master`)
   4. Head: `git rev-parse HEAD`
   5. Plan: the plan file for this work, or `none`

## Dispatch

Launch the `reviewer` subagent in the foreground. The prompt is this template with the fields filled in, and nothing else:

```text
Review request
Repository: <absolute path>
Branch: <branch>
Base: <full base sha>
Head: <full head sha>
Plan: <repo-relative path to the plan file, or none>
Round: <1, 2, or 3>
Previous review: <full sha of the head reviewed in the previous round, or none>
```

- Do not summarize your change, claim that tests pass, call it small or safe, or say what to focus on or skip. The reviewer reads the diff and the plan itself. Anything extra is framing; the reviewer is told to treat it as a claim to check.
- Do not set a `model` when launching the reviewer. Its model is pinned to a different family on purpose.
- Do not review your own change in its place, and do not launch any other agent to review it.

## Act on the verdict

Read the `OCREVIEW` line at the end of the report.

**APPROVE:** push (see the `git-workflow` skill). The approval covers only that commit. Any new commit, amend, or rebase needs a new review.

**REQUEST_CHANGES:** handle every BLOCKER, MAJOR, and WRITING finding in one of two ways. Never drop one silently.

- **Fix it.** BLOCKER and MAJOR fixes ship with a regression test that fails on the reviewed commit and passes with the fix. Adopt the reviewer's harness from the report's "Harnesses" section, or write your own. Either way, name and describe the test by the behavior it protects, never by the review finding, and fit it to the project's test conventions. Run it against the old code before committing the fix, to see it fail. WRITING fixes need no test: fix the text using the reviewer's rewrite or your own. Then request the next round with `Previous review` set to the head that was just reviewed. The reviewer checks that each BLOCKER and MAJOR fix has a regression test that fails on the old head; a fix without one comes back as a new MAJOR.
- **Dispute it,** only with concrete evidence that the finding is wrong (a test, a trace, a spec quote). Do not dispute by re-requesting and hoping for a different answer. Stop and bring the finding, your evidence, and your recommendation to Phil; he decides.

If a regression test for a BLOCKER or MAJOR fix is genuinely impractical (for example, a resource leak with no observable hook, or a revert of an unplanned API change), that is a dispute too: don't skip the test quietly. Bring the fix and your reasoning to Phil.

MINOR and NIT findings: fix them if cheap, without regression tests; otherwise list them for Phil in your summary.

**TODOs for Phil:** the report lists every TODO the change adds. They don't block the push, but they block merging: list each one for Phil in your summary under "Decisions for Phil". On GitHub, put them in the pull request too and open it as a draft (see the `git-workflow` skill).

**Round cap:** if round 3 still returns REQUEST_CHANGES, stop and escalate to Phil with the outstanding findings. Do not start round 4.

## When the push is rejected

Read the hook's message. It means the commit has no approval, the approval is for a different commit, or the target is main/master. Fix the cause by requesting a review or pushing a feature branch. Never try to work around the hook.

## Red flags

| Thought | Reality |
| --- | --- |
| "The reviewer is being pedantic; I'll push anyway." | The hook rejects it. Fix the finding or escalate it. |
| "I'll mention the tests already pass to save time." | That is framing. Use the template, nothing else. |
| "Tiny fix after approval; no need for another review." | New commit, no approval. Request round N+1. |
| "I'll ask for a fresh review and see if it approves." | That's shopping for a verdict. Disagreements go to Phil. |
| "I could write the approval note myself." | You can't, and trying is a violation of the working agreement. |
| "The fix is obviously right; a regression test is overkill." | BLOCKER and MAJOR fixes always get one. The reviewer checks it fails on the old head. |
| "I'll write a test that passes, that's a regression test." | It must fail on the reviewed commit first. Run it there before committing the fix. |
| "`// Regression test for review finding 2` explains why the test exists." | Comments and test names stand on their own. Describe the behavior the test protects. |
| "The TODO is minor; no need to bother Phil." | Every new TODO goes to Phil, and on GitHub the pull request stays a draft until he decides. |
