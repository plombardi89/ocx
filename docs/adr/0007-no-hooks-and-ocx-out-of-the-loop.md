# 0007: The git provider protects main, and ocx stays out of the loop

Status: accepted
Date: 2026-11-15
Replaces: none

## Context

A pre-push hook rejected pushes to main and any commit without an approving review note. It had to be installed in every repository, which is easy to forget, and it also gated the user's own pushes. A set of policies existed only to keep agents from getting around it. Over time `ocx` had also gained commands used during the work itself: running the reviewer, installing the hook, and staging the configuration into a project.

## Decision

1. No git hooks. The git provider's branch protection guards main: it blocks force pushes and deletion and requires a pull request. GitHub, GitLab, Gitea, and Bitbucket all have it.
2. Getting an approving review before each push is a working agreement in the skills, not something a tool enforces. The pull request quotes the review's verdict.
3. Agent policies still deny pushing to main or master, force pushes, `--no-verify`, deleting remote refs, `git config` writes, and editing files under `.git/`. The policies that only guarded the hook are removed: `git -c` and `git -C`, git's other global options, the `GIT_*` environment variables, `git update-ref`, and any command mentioning `.git/hooks`.
4. `ocx` only develops and installs this configuration: `check`, `eval`, `install`, `uninstall`, and `doctor`. The `review`, `hook`, `sandbox`, and `stage` commands are removed.
5. The reviewer still records its verdict as a git note, because later review rounds read it.

## Alternatives considered

- A global hook through git's `core.hooksPath`, enforced only in agent sessions: no per-repository setup, but it changes how every repository finds its hooks, and repositories that set their own path skip it.
- An `ocx push` command, with plain `git push` denied to agents: it would put `ocx` in the middle of the loop.
- Keeping per-repository hooks, with a reminder before the first push: still per-repository, and still gating the user's own pushes.

## Consequences

Nothing mechanically stops an agent from pushing a branch that wasn't reviewed; the missing verdict shows in the pull request. A repository without branch protection relies on the agent policies alone. This repository is reviewed through the `reviewer` subagent like any other, which needs the global install.
