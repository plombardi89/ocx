# 0005: Phil's decisions go in ADRs, and finished plans are deleted

Status: accepted
Date: 2026-11-15
Replaces: none

## Context

Plans piled up in `docs/plans/` after their work was done, and old plans fill agents' context without helping. Phil's decisions were buried in plans' Open questions and Logs. Small work, which needs no plan, had no place to record his rulings, so a plan was once written after the fact just to carry three decisions to the reviewer.

## Decision

1. Each of Phil's decisions that should outlive the work becomes a short ADR in `docs/adr/`: an answer to a plan's open question, a scope ruling during review, or a settled dispute. An agent may draft one as proposed; it's a decision once Phil accepts it.
2. An accepted ADR is never edited. A new ADR that changes a decision names the old one in `Replaces:`, and the old one is deleted, so `docs/adr/` holds only the decisions in force.
3. Plans stay the spec for multi-step work. When new work starts, its branch's first commit deletes every plan whose work has merged, after writing its lasting decisions as ADRs.
4. The reviewer reads the ADRs a branch adds or changes, or that its plan links, and treats accepted ones as Phil's decisions. The plan critic flags a plan that contradicts an accepted ADR.

## Alternatives considered

- Delete each plan in its own branch's last commit: the final review would have no plan at Head, so the reviewer, the test-writer, and `ocx review` would all need to read plans from an earlier commit.
- Keep plans outside the repository: they would no longer be versioned with the code the test-writer and the reviewer check.
- Replace plans with ADRs: the plan critic, the test-writer, and the reviewer all check work against a plan's acceptance criteria.
- Keep replaced ADRs, marked superseded: they would pile up like plans.

## Consequences

A finished plan stays in `main` until the next piece of work starts. Agents read only the ADRs that matter for the change at hand, not the whole directory.
