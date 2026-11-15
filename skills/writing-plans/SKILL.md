---
name: writing-plans
description: Use when work will take more than one sitting or several steps, touches multiple components, or the user asks for a plan, spec, or design decision record.
---

# Writing plans

Match the paperwork to the size of the work.

| Work | Artifact |
| --- | --- |
| Fits in one sitting | None. The commit message is the record. |
| Several steps or sessions | A plan: `docs/plans/NNNN-<slug>.md`, deleted once the work has merged |
| A decision of the user's that should outlive the work | An architecture decision record (ADR): `docs/adr/NNNN-<slug>.md` |

`NNNN` is the next four-digit number never used in that directory, starting at `0001`; check `git log --name-only` for deleted files too. The slug is short kebab-case.

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
4. Decisions only the user can make: behavior on empty, invalid, or excessive input; error responses; limits; compatibility with current callers; concurrency expectations.

Learn what you can from the code first; don't ask the user what the repository already answers. Then ask the user the rest in one batch with the question tool, each question with a suggested default the user can accept. If you can't ask (for example in a non-interactive run), write the questions under "Open questions" in the plan instead.

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

1. **Findings:** fix every BLOCKER, MAJOR, and WRITING finding by changing the plan, or dispute it with evidence by bringing it to the user. Never drop one silently.
2. **Questions for the user:** ask the user each one, with the critic's suggested default, and write their answers into the plan as criteria.
3. **Log:** append `Critique round <n>: <verdict> (<counts>)` and what you changed.
4. **Next round:** request round n+1 after revising. If round 3 still returns REVISE, stop and bring the remaining findings to the user. Don't start round 4.

### 4. Get the user's approval

Show the user the Goal, Non-goals, Acceptance criteria, and the critic's final verdict, and ask for their approval. Record it in the Log. If they change anything substantive, run the critique again.

### 5. Commit and implement

Create the feature branch (see the `git-workflow` skill), commit the plan as the first commit, then implement task by task, checking off tasks and appending to the Log as you go.

When every task is implemented and committed, get independent tests for the acceptance criteria with the `requesting-tests` skill, then request review with the `requesting-review` skill.

When the work is done, set `Status: done`.

## Clean up finished plans

Old plans fill agents' context without helping, so they don't stay. When you start new work, check `docs/plans/` first. In the new branch's first commit, delete every plan whose work has merged, after writing its lasting decisions as ADRs. Git history keeps the plan.

## ADRs

Copy `templates/adr.md` and keep it short: the context, the decision, the alternatives considered, and the consequences. Write one for each decision of the user's that should outlive the work: an answer to a plan's open question that constrains later work, a scope ruling during review, or a settled dispute. Write it on the branch where the user decides, with `Status: accepted`, and link it from the plan if there is one. You may draft one with `Status: proposed` for the user to accept; it isn't a decision until the user accepts it.

An accepted ADR is never edited. When a decision changes, write a new ADR that names the old one in `Replaces:`, and delete the old one in the same commit, so `docs/adr/` holds only the decisions in force.

## Red flags

| Thought | Reality |
| --- | --- |
| "The request is clear enough; I'll skip the questions." | Edge-case behavior is the user's call. Ask, with suggested defaults. |
| "I'll tell the critic the plan is small so it goes faster." | That's framing. Use the template, nothing else. |
| "The critic is nitpicking; I'll start coding." | Fix the findings, dispute them to the user, or stop at the round cap. No code before approval. |
| "The user approved the idea, so the plan is approved." | Approval is for the written criteria. Show them to the user. |
