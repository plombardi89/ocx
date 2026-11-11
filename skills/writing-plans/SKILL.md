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

## Plans

Copy `templates/plan.md` and fill it in, following the `writing-style` skill: plain English, numbered criteria, tasks, and log entries. The plan is three things at once:

1. **The reviewer's spec.** The reviewer checks every acceptance criterion and fails the review if one is unmet. Write criteria that can be checked: observable behavior with concrete values, not "works well" or "is robust".
2. **The task list.** Each task is a small, verifiable step with a "Done when" line.
3. **The resume point.** Conversation memory doesn't survive compaction or a new session; the plan does. Update the task checkboxes and append to the Log as you go. When resuming, trust the plan and `git log` over your memory.

Plans are documents: they may name code files, but code comments never refer back to plans.

Before implementing a non-trivial plan, show Phil the Goal, Non-goals, and Acceptance criteria and get his agreement. Commit the plan on the feature branch with the code it describes.

When the work is done, set `Status: done` and leave the file in place.

## Decision records

Copy `templates/decision.md`. Keep it short: context, the decision, the alternatives considered, and the consequences. Write one when a choice constrains future work, such as a library, data format, protocol, or architectural boundary, and link it from the plan.

Never rewrite an accepted decision. Supersede it with a new record and mark the old one `Superseded by NNNN`.
