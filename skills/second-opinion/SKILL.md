---
name: second-opinion
description: Use when you are the consultant asked for an independent second opinion on a problem that another agent is stuck on.
---

# Second opinion

Another agent has tried and failed to explain or fix a problem. Your value is a view it doesn't share: look at the problem fresh, question what it concluded, and find the hypotheses it never considered. Follow the steps in order.

## 1. Restate the problem

Write down the expected and the actual behavior from the Problem field, with exact inputs and outputs. Ignore the attempts for now.

## 2. Read the code yourself

Read the code on the failing path end to end, including callers and the data it receives, and the project's `AGENTS.md`. Form your own picture before you read the attempts.

## 3. Audit the attempts

For each attempt:

1. What did the experiment actually rule out? Often less than the author concluded.
2. Was the result real? Re-run the cheap ones (tests, small commands) if a conclusion depends on them.
3. Which assumption did it rest on? An assumption every attempt shares is the first place to look.

## 4. Form your own hypotheses

List candidate causes, most likely first, including ones no attempt considered. Look especially at:

1. Nondeterminism: map iteration order, ties in sorting, goroutine scheduling, clocks, randomness.
2. State: data shared between calls or requests, caches, globals, initialization order.
3. Boundaries: empty, zero, max, off-by-one, overflow, encoding.
4. The environment: configuration, versions, input data that differs from the tests.

## 5. Make each hypothesis testable

For each hypothesis, give the evidence for it, the evidence against it, and one cheap experiment whose result confirms or rules it out. Run the experiments you can with the commands you're allowed.

## 6. Write the report

Your final message is exactly this structure:

````markdown
# Second opinion: <one-line summary of the problem>

**Status:** HYPOTHESES | NEED_INFO

## What the attempts ruled out
1. <attempt>: <what its evidence actually shows>

## Unsupported conclusions
- <a conclusion the evidence doesn't support, and why>

## Hypotheses
1. **<hypothesis, most likely first>**
   1. Evidence for: <...>
   2. Evidence against: <...>
   3. Experiment: <command or change>. If <result>, confirmed; if <result>, ruled out.

## Recommended next step
<the one experiment to run first, and why>

OCCONSULT status=<HYPOTHESES|NEED_INFO>
````

Use NEED_INFO only when no useful hypothesis is possible without more information, and say exactly what information. Write "None" under any empty section.

## Red flags

| Thought | Reality |
| --- | --- |
| "Attempt 2 ruled out the store." | Check what its experiment could actually detect. |
| "All the attempts looked at the handler, so it's in the handler." | A shared assumption is the first thing to question. |
| "I'll suggest the same fix with a tweak." | Your job is a different view. Start from the code, not the attempts. |
