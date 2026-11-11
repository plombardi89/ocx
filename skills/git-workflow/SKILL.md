---
name: git-workflow
description: Use when starting work in a git repository, creating a branch, committing, pushing, or opening a pull request.
---

# Git workflow

## Branches

Never commit on `main` or `master`. Before your first edit, check:

```sh
git branch --show-current
git status --porcelain
```

If you are on `main` or `master`, create a branch first:

```sh
git switch -c <type>/<slug>
```

| Type | Use for |
| --- | --- |
| `feat/` | New behavior |
| `fix/` | Bug fixes |
| `refactor/` | Behavior-preserving restructuring |
| `docs/` | Documentation only |
| `chore/` | Build, tooling, dependencies |

The slug is short kebab-case naming the work, such as `feat/product-pagination`. Use one branch per plan or task. If the working tree has someone else's uncommitted changes, stop and ask before switching branches.

## Commits

- Commit small, coherent steps that each build and pass tests.
- Message format: a Conventional Commits subject matching the branch type (`feat: paginate product list`), imperative mood, at most 72 characters, in plain English (see the `writing-style` skill). Add a body explaining why when it isn't obvious, and name the plan file if there is one.
- Stage files deliberately (`git add <paths>`); check `git status` so nothing unintended is included.
- Never use `--no-verify`, and never rewrite commits that have already been pushed.

## Pushing

Push only after the `requesting-review` skill gives you an approving review of the exact commit at HEAD.

```sh
git push -u origin <type>/<slug>
```

- Never push to `main` or `master`, force-push, delete remote branches, or use `--no-verify`. These are blocked for agents anyway.
- If the hook rejects the push, read its message and fix the cause. Don't work around it.
- If the remote branch moved, stop and tell Phil. A rebase creates new commits that need a new review.

## Pull requests

After the branch is pushed, open a pull request against `main`. If the branch adds any TODO comments, open it as a draft so it can't be merged until Phil decides on each one:

```sh
gh pr create --base main --head <type>/<slug> --title "<subject>" --body-file <file>
gh pr create --draft --base main --head <type>/<slug> --title "<subject>" --body-file <file>   # branch adds TODOs
```

Write the body to a temporary file first, following the `writing-style` skill. Include, in this order:

1. What changed and why (link the plan file if there is one).
2. How it was verified (commands and results).
3. The review verdict (`git notes --ref=ocreview show HEAD`, first line) and any MINOR findings left open.
4. **Decisions for Phil:** every TODO the branch adds, with its file and line, and the question "fix now or remove?". Write "None" if there are none.

Never merge pull requests, approve them, mark them ready for review, or call `gh api`. Those decisions are Phil's.
