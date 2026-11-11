---
name: systematic-debugging
description: Use when something behaves wrongly - a failing test, a bug report, a crash, flaky behavior, or wrong output - before changing any code to fix it.
---

# Systematic debugging

Find the cause before changing code. A fix without a root cause is a guess, and guesses ship new bugs. Follow the steps in order.

## 1. Reproduce

1. Write down the expected and the actual behavior from the report, with exact inputs, outputs, and error text.
2. Reproduce it with the smallest automated check you can: a failing test is best, then a short program or script. Run it and see it fail for the reason in the report.
3. For a bug that comes and goes, repeat the check many times (`go test -count=200`, a loop), vary timing and order, and look for sources of nondeterminism: map iteration order, goroutine scheduling, clocks, randomness, and unsorted or tied input.
4. If you can't reproduce it, don't change any code. Report what you tried and what information would help.

Build reproduces on its `fix/` branch. The debugger reproduces in a scratch worktree (see "Scratch worktrees").

## 2. Gather evidence

1. Read the code on the failing path end to end, including its callers and the data it receives.
2. Check recent history: `git log -p` for the files involved. If the bug is a regression, find when it started by running the reproduction against older commits.
3. Add temporary logging or assertions where the evidence runs out. Remove them before committing.

## 3. Form hypotheses

List the candidate causes, most likely first. For each one, write the prediction that tells it apart from the others: "if the cause is X, then Y happens when Z".

## 4. Test one hypothesis at a time

Run the experiment that best separates the hypotheses, changing one thing at a time. Record each attempt: the hypothesis, the experiment, and the result.

## 5. Explain the root cause

State the chain from the cause to the symptom as numbered steps, with file and line. If you can't explain every step, you haven't found the root cause yet.

## 6. Fix at the root

1. Make the smallest change that removes the cause. Don't patch the symptom: removing duplicates from output, retrying, catching and ignoring an error, or special-casing the reported input all hide the bug.
2. Keep the reproduction as a permanent regression test named for the behavior it protects. It must fail without the fix and pass with it.
3. Look for siblings: search for the same mistake elsewhere and fix those too.
4. Run the full test suite.

## 7. When you're stuck

Stop guessing after three experiments in a row disprove your leading hypothesis, or after three fixes fail to remove the failure:

1. **Build:** get a second opinion from the `consultant` subagent, using the template below.
2. **Debugger:** report STUCK, with every attempt.
3. If the consultant's hypotheses don't hold up either, bring everything to Phil.

The consultation request is this template with the fields filled in, and nothing else:

```text
Consultation request
Repository: <absolute path>
Commit: <full sha where the problem occurs>
Problem: <the bug report or failure, verbatim>
Attempts:
1. Hypothesis: <what you thought> Experiment: <command or change> Result: <what happened, with the key output>
2. <...>
```

Report facts only in Attempts: what you ran and what you saw. Don't add your opinion of the cause; the consultant forms its own. Don't set its model.

## Using the debugger subagent (build only)

For a hard bug, or when your context is already crowded, launch the `debugger` subagent in the foreground. The prompt is this template with the fields filled in, and nothing else:

```text
Debug request
Repository: <absolute path>
Commit: <full sha where the bug occurs>
Bug: <the bug report, verbatim>
```

Pass the report exactly as you received it, without your own hypotheses, and don't set its model. Then apply its fix on a `fix/` branch, keep its reproduction as a regression test, run the full suite yourself, and continue with the `requesting-review` skill.

## Scratch worktrees (debugger only)

1. Work only in scratch worktrees under `/tmp/opencode/debug/`, named `<repo-dir-name>-<sha 7>-<4 random lowercase letters>`:

   ```sh
   git worktree add --detach /tmp/opencode/debug/shop-306ddb2-kqvx <full sha>
   ```

2. To test an older commit, add another worktree with an `-old` suffix.
3. Give file tools absolute paths under `/tmp/opencode/debug/`, and use literal paths in commands. Relative paths resolve against the author's repository and are denied.
4. Run commands with the shell tool's working directory set to the scratch worktree.
5. Remove every scratch worktree before you finish, then confirm with `git worktree list`:

   ```sh
   git worktree remove --force /tmp/opencode/debug/shop-306ddb2-kqvx
   ```

## Debugger report

The debugger's final message is exactly this structure:

````markdown
# Debug report: <one-line summary of the bug>

**Status:** ROOT_CAUSE | STUCK

## Reproduction
Command: `<exact command>`
Result: <the failing output>
```<language>
<the reproduction code>
```

## Root cause
1. <step from the cause to the symptom, with file:line>

## Fix
```diff
<the smallest fix at the root>
```
Why this is the root: <why the fix belongs here and not where the symptom appeared>

## Verification
- <the fix applied in scratch: the reproduction passes; the full suite passes>

## Siblings
- <the same mistake elsewhere, or None>

## Attempts
1. Hypothesis: <...> Experiment: <...> Result: <...>

OCDEBUG status=<ROOT_CAUSE|STUCK>
````

## Red flags

| Thought | Reality |
| --- | --- |
| "I can see the problem; no need to reproduce it." | Without a failing check, you can't show the fix works. |
| "I'll remove the duplicates in the handler." | That hides the symptom. Find out why there are duplicates. |
| "One more quick fix might do it." | After three, stop and get a second opinion. |
| "The test is flaky; I'll rerun it." | Flaky means nondeterministic. That's a bug to find. |
| "The report says it's the cache, so it's the cache." | The report describes symptoms. Its theories are unverified. |
