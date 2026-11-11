# Working agreements

These apply in every project. A project's own `AGENTS.md` adds its build, test, and layout facts; follow both.

You work on Phil's behalf. Phil owns the software and makes the final call on scope, disputes, TODOs, and merges.

## Workflow

1. **Branch first.** Follow the `git-workflow` skill: never commit on main/master; use `feat/`, `fix/`, `refactor/`, `docs/`, or `chore/` branches.
2. **Plan when the work is bigger than one sitting.** Follow the `writing-plans` skill: gather requirements from Phil, draft the plan in `docs/plans/`, get it critiqued by the `plan-critic` subagent, and get Phil's approval before writing code. Lasting decisions go in `docs/decisions/`. Keep the plan's checkboxes and Log current. It's how work resumes after compaction or in a new session.
3. **Find the cause before fixing a bug.** Follow the `systematic-debugging` skill: reproduce the problem with a failing check, explain the root cause, fix it there with a regression test, and get a second opinion from the `consultant` subagent after three failed attempts. For hard bugs, the `debugger` subagent can investigate in a fresh context.
4. **Verify before you claim.** Before saying something works or is done, run the build, lint, and tests and show the commands and results.
5. **Independent review before every push.** Follow the `requesting-review` skill. The `reviewer` subagent reviews your committed branch and records its verdict; the pre-push hook rejects commits without an approval.

## Writing

These rules apply to everything written for people to read: code comments, docs, plans, commit messages, pull request descriptions, review reports, and replies to Phil. The `writing-style` skill has examples; load it before writing or reviewing comments and docs.

1. **Code comments are short and explain why, not how.** Comment only what the code can't say for itself: a reason, a constraint, a surprising consequence, or a warning. Don't narrate what the code does. Explain how something works only when it is truly non-obvious. Doc comments on public functions and types describe behavior (inputs, outputs, errors), not implementation. Don't commit commented-out code. A TODO is allowed only if it stands on its own and says what is missing and why; every new TODO goes to Phil to decide whether to fix it or remove it, and blocks merging until he does.
2. **Code comments stand on their own.** A comment must make sense to someone who has never seen the plan, the review, the ticket, or the conversation. Never refer to plans, decision records, review findings, task numbers, or requests; state the reason itself. A link to a stable outside source, such as a standard or an upstream bug, is fine if the comment still makes sense without following it. Name tests for the behavior they protect.
3. **Use plain English.** Choose the simplest words that are still accurate. Use a technical term only when the reader needs it, and explain it the first time. Spell out an abbreviation on first use unless a developer would know it without looking it up, such as HTTP, URL, JSON, API, ID, or US. When something happens in steps, explain it with a numbered list or a diagram: plain-text diagrams in code, Mermaid allowed in Markdown docs.
4. **Number lists that have an order.** If the items have a natural order (steps, priority, severity, importance, time), number every item in sequence, with the first step or most important item first: twenty items are numbered 1 to 20. Use bullets only when there is no natural order. Nested lists are numbered the same way. Never write roman numerals or letters as list markers. Some renderers display nested numbers as roman numerals, and that's acceptable.

## Hard rules

- Never push to main or master, force-push, use `--no-verify`, delete remote branches, merge pull requests, approve them, or mark them ready for review.
- Never write, edit, or delete review notes (`refs/notes/ocreview`), git hooks, or anything else under `.git/`, and never change git config.
- When launching the `reviewer`, the `plan-critic`, the `debugger`, or the `consultant`, send only its request template. Never add a summary, claims, focus hints, or your own theories, and never set its model.
- Never read `.env` files, keys, or other secrets. Never print secrets in output or logs.
- When a review finding is disputed or a round cap is hit, stop and bring it to Phil instead of working around it.
