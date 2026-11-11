---
name: writing-plans
description: Use when work will take more than one sitting or several steps, touches multiple components, or Phil asks for a plan, spec, or design decision record.
---

# Writing plans

Match the paperwork to the size of the work.

| Work | Artifact |
| --- | --- |
| Fits in one sitting | None. The commit message is the record. |
| Several steps or sessions | A plan: `docs/plans/NNNN-<slug>.md` |
| A decision that should outlive the feature | A decision record: `docs/decisions/NNNN-<slug>.md` |

`NNNN` is the next free four-digit number in that directory, starting at `0001`. The slug is short kebab-case.

## What a plan is

A plan is three things at once:

1. **The reviewer's spec.** The code reviewer checks every acceptance criterion and fails the review if one is unmet. Write criteria that can be checked: observable behavior with concrete values, not "works well" or "is robust".
2. **The task list.** Each task is a small, verifiable step with a "Done when" line.
3. **The resume point.** Conversation memory doesn't survive compaction or a new session; the plan does. Update the task checkboxes and append to the Log as you go. When resuming, trust the plan and `git log` over your memory.

Plans are documents: they may name code files, but code comments never refer back to plans.

## Making a plan

Follow these steps in order. Don't write any code until step 5.

### 1. Gather requirements

Before drafting, make sure you know:

1. The problem, and who it's for.
2. What "done" looks like, in observable behavior.
3. What is out of scope.
4. Decisions only Phil can make: behavior on empty, invalid, or excessive input; error responses; limits; compatibility with current callers; concurrency expectations.

Learn what you can from the code first; don't ask Phil what the repository already answers. Then ask Phil the rest in one batch with the question tool, each question with a suggested default he can accept. If you can't ask (for example in a non-interactive run), write the questions under "Open questions" in the plan instead.

### 2. Draft

Copy `templates/plan.md` and fill it in, following the `writing-style` skill: plain English, numbered criteria, tasks, and log entries. Write every edge-case decision from step 1 as an acceptance criterion with concrete values, and give every criterion a planned test in the tasks.

### 3. Get an independent critique

Launch the `plan-critic` subagent in the foreground. The prompt is this template with the fields filled in, and nothing else:

```text
Plan review request
Repository: <absolute path>
Plan: <repo-relative path to the plan file>
Round: <1, 2, or 3>
```

- Don't summarize the plan, defend it, or say what to focus on. The critic reads the plan and the code itself.
- Don't set a `model` when launching it. Its model is pinned to a different family on purpose.

Read the `OCPLAN` line at the end of the report, then:

1. **Findings:** fix every BLOCKER, MAJOR, and WRITING finding by changing the plan, or dispute it with evidence by bringing it to Phil. Never drop one silently.
2. **Questions for Phil:** ask Phil each one, with the critic's suggested default, and write his answers into the plan as criteria.
3. **Log:** append `Critique round <n>: <verdict> (<counts>)` and what you changed.
4. **Next round:** request round n+1 after revising. If round 3 still returns REVISE, stop and bring the remaining findings to Phil. Don't start round 4.

### 4. Get Phil's approval

Show Phil the Goal, Non-goals, Acceptance criteria, and the critic's final verdict, and ask for his approval. Record it in the Log. If he changes anything substantive, run the critique again.

### 5. Commit and implement

Create the feature branch (see the `git-workflow` skill), commit the plan as the first commit, then implement task by task, checking off tasks and appending to the Log as you go.

When the work is done, set `Status: done` and leave the file in place.

## Decision records

Copy `templates/decision.md`. Keep it short: context, the decision, the alternatives considered, and the consequences. Write one when a choice constrains future work, such as a library, data format, protocol, or architectural boundary, and link it from the plan.

Never rewrite an accepted decision. Supersede it with a new record and mark the old one `Superseded by NNNN`.

## Red flags

| Thought | Reality |
| --- | --- |
| "The request is clear enough; I'll skip the questions." | Edge-case behavior is Phil's call. Ask, with suggested defaults. |
| "I'll tell the critic the plan is small so it goes faster." | That's framing. Use the template, nothing else. |
| "The critic is nitpicking; I'll start coding." | Fix the findings, dispute them to Phil, or stop at the round cap. No code before approval. |
| "Phil approved the idea, so the plan is approved." | Approval is for the written criteria. Show them to him. |
