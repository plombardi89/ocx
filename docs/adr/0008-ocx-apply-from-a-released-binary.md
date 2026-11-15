# 0008: ocx installs the configuration from a released binary, with no hooks

Status: accepted
Date: 2026-11-15
Replaces: 0007-no-hooks-and-ocx-out-of-the-loop

## Context

ADR 0007 removed the pre-push hook and kept `ocx` to developing and installing this configuration. Its install linked `~/.config/opencode` into a checkout of this repository, so whatever branch that checkout had, including uncommitted edits, was live in every OpenCode session. The `ocx` binary also carried the checks and evals that only matter while developing this repository.

## Decision

1. No git hooks. The git provider's branch protection guards main: it blocks force pushes and deletion and requires a pull request. GitHub, GitLab, Gitea, and Bitbucket all have it.
2. Getting an approving review before each push is a working agreement in the skills, not something a tool enforces. The pull request quotes the review's verdict.
3. Agent policies deny pushing to main or master, force pushes, `--no-verify`, deleting remote refs, `git config` writes, and editing files under `.git/`.
4. The reviewer records its verdict as a git note, because later review rounds read it.
5. The `ocx` binary carries the configuration it was built from and has three commands: `apply` installs or upgrades it as plain files in OpenCode's global configuration directory, `remove` deletes what `apply` wrote, and `version` names the commit. It keeps no backups and doesn't convert earlier link-based installs.
6. GitHub Actions builds `ocx` for Linux amd64 and arm64 on every push to main, after the checks pass, and publishes it to a rolling release tagged `latest`. `go install github.com/plombardi89/ocx/cmd/ocx@main` builds the same binary without GitHub's releases.
7. The checks and evals of this repository are developer tooling: the checks run as Go tests under `make test` and `make check`, and the evals run through `make eval`. Neither is part of the binary, and `ocx` plays no part in the development loop.

## Alternatives considered

- Keeping links, but from a dedicated clone that stays on main: a second checkout to keep track of, and nothing records which version is installed.
- Copying files with backups, conflict detection, and conversion of old installs: much more code for one user who can reinstall in a minute.
- macOS and Windows binaries: OpenCode runs in WSL on Windows, and nobody needs macOS yet.

## Consequences

A branch of this repository never reaches the installed configuration by accident; trying one means `make apply` on purpose. Upgrading means getting a newer binary and running `ocx apply` again. A file at a path `ocx apply` writes is overwritten without a copy.
