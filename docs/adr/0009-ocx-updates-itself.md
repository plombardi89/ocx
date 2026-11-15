# 0009: ocx installs and updates itself from released binaries, with no hooks

Status: accepted
Date: 2026-11-15
Replaces: 0008-ocx-apply-from-a-released-binary

## Context

ADR 0008 made `ocx` a binary that carries the configuration it was built from, with `apply`, `remove`, and `version`, built and published by CI on every push to main. Upgrading still meant downloading the new binary by hand, checking it against `SHA256SUMS`, installing it, and running `ocx apply`.

## Decision

1. No git hooks. The git provider's branch protection guards main: it blocks force pushes and deletion and requires a pull request. GitHub, GitLab, Gitea, and Bitbucket all have it.
2. Getting an approving review before each push is a working agreement in the skills, not something a tool enforces. The pull request quotes the review's verdict.
3. Agent policies deny pushing to main or master, force pushes, `--no-verify`, deleting remote refs, `git config` writes, and editing files under `.git/`.
4. The reviewer records its verdict as a git note, because later review rounds read it.
5. The `ocx` binary carries the configuration it was built from and has four commands: `apply` installs or upgrades it as plain files in OpenCode's global configuration directory, `remove` deletes what `apply` wrote, `update` replaces the binary with the latest release, and `version` names the commit. `apply` keeps no backups and doesn't convert earlier link-based installs.
6. `ocx update` checks the release binary against the release's `SHA256SUMS` and that its `version` runs before it renames it over the running binary, and leaves the running binary alone on any failure. It installs the release's binary whenever its bytes differ, with no version comparison, and doesn't guard against two updates at once. `--check` only reports, and `--apply` runs the new binary's `apply`. `OCX_RELEASE_URL` points it at another release location, such as a mirror.
7. GitHub Actions builds `ocx` for Linux amd64 and arm64 on every push to main, after the checks pass, and publishes it to a rolling release tagged `latest`. `go install github.com/plombardi89/ocx/cmd/ocx@main` builds the same binary without GitHub's releases.
8. The checks and evals of this repository are developer tooling: the checks run as Go tests under `make test` and `make check`, and the evals run through `make eval`. Neither is part of the binary, and `ocx` plays no part in the development loop.

## Alternatives considered

- Keeping links, but from a dedicated clone that stays on main: a second checkout to keep track of, and nothing records which version is installed.
- Copying files with backups, conflict detection, and conversion of old installs: much more code for one user who can reinstall in a minute.
- macOS and Windows binaries: OpenCode runs in WSL on Windows, and nobody needs macOS yet.
- Signing releases so `ocx update` can prove who built them: more machinery than one user needs; HTTPS to GitHub vouches for the source.
- Comparing versions so `ocx update` never installs older code: versions are commit hashes with no order, and the release publishes nothing that would give one.

## Consequences

A branch of this repository never reaches the installed configuration by accident; trying one means `make apply` on purpose. Upgrading is `ocx update --apply`. A file at a path `ocx apply` writes is overwritten without a copy, and a `go install` build is replaced by the release build the next time `ocx update` runs.
