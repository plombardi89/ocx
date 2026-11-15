# 0003: `ocx apply` from a released binary

Status: in-progress
Branch: feat/apply
Decisions: [0007](../adr/0007-no-hooks-and-ocx-out-of-the-loop.md), replaced by a new ADR in task 7

## Goal

Today `make install` links `~/.config/opencode` into a checkout of this repository, so whatever branch that checkout has, including uncommitted edits, is live in every OpenCode session. Instead, a small `ocx` binary will carry the configuration it was built with and install it as plain files with `ocx apply`. CI builds the binary on every push to `main`, so setting up or upgrading a machine is a download and one command. The checks and evals that develop this repository move out of the binary and into the Makefile.

## Non-goals

These keep the tool simple, as Phil asked:

- Backups. `ocx apply` overwrites whatever is at the paths it writes.
- Compatibility with earlier installs. Links left by `make install` aren't converted.
- Locking against two runs at once, and recovery from a crash in the middle of a run.
- `ocx doctor`. `make check` still resolves every agent's permissions from the same files, staged into a project. Its checks that OpenCode loads the global install, and that no other configuration file adds policies, aren't kept.
- macOS and Windows binaries, version tags, changelogs, package managers, and automatic updates.

## Acceptance criteria

1. [ ] The `ocx` binary embeds `config/opencode.jsonc`, `config/global-AGENTS.md`, `config/prompts/`, `skills/`, and `commands/` as they were when it was built. It has three commands, `apply`, `remove`, and `version`, and all three work from any directory.
2. [ ] `ocx version` prints the commit the binary was built from, with `-modified` appended when the build had uncommitted changes, or the module version when it was built with `go install`.
3. [ ] `ocx apply` writes every embedded file as a regular file into OpenCode's global configuration directory (`$XDG_CONFIG_HOME/opencode` or `~/.config/opencode`, or `--config-dir`): `opencode.jsonc`, `AGENTS.md`, `prompts/<file>`, `skills/<id>/...`, and `commands/<name>.md`. It overwrites any file already there.
4. [ ] Before changing anything, `ocx apply` and `ocx remove` refuse, naming the path and exiting with status 1, when a link sits at a path they write or delete, or at a directory between that path and the configuration directory, or when a directory sits where `apply` writes a file. Neither writes or deletes through a link. The configuration directory itself may be a link; it's resolved first.
5. [ ] After writing every file, `ocx apply` lists them in `.ocx-files` in that directory, one relative path per line. On the next apply, it deletes the listed files that the new binary doesn't ship, and the empty directories they leave behind.
6. [ ] `apply` and `remove` refuse an `.ocx-files` entry that is absolute, contains `..`, or names the configuration directory itself, before changing anything. They delete only listed files and empty directories, never a directory's contents.
7. [ ] `ocx apply --dry-run` prints the files it would write and delete and changes nothing.
8. [ ] `ocx remove` deletes the files listed in `.ocx-files`, the empty directories they leave, and then `.ocx-files` itself. A listed file that's already gone is skipped. With no `.ocx-files`, it says there's nothing to remove.
9. [ ] When a file operation fails, `apply` and `remove` stop, exit with status 1, and name the operation and the path. They don't undo earlier changes. `.ocx-files` is only replaced or deleted once everything before it succeeded, and it's replaced by writing a temporary file and renaming it, so a failed write leaves the previous list whole. A listed file that's already gone is skipped, by `apply` as well as `remove`, so running the command again finishes the job.
10. [ ] `make test` runs the checks `ocx check --offline` runs today, as Go tests. `make check` adds the resolved-permission probes against OpenCode, as now. The `check`, `check-offline`, `eval`, `eval-*`, `bakeoff`, and `verify` Makefile targets stay; the eval targets run the eval tool as a program that isn't part of the `ocx` binary.
11. [ ] A GitHub Actions workflow runs on every push to `main`, one run at a time, without cancelling a run in progress. Each run:
    1. Runs `make check-offline`, and stops if it fails.
    2. Builds `ocx-linux-amd64` and `ocx-linux-arm64` with cgo disabled.
    3. Writes their SHA-256 checksums to `SHA256SUMS`.
    4. Stops without publishing if its commit is no longer the tip of `main`, because a newer run will publish.
    5. Publishes the three files to a release tagged `latest`, creating it the first time and replacing its files after that, and sets its notes to name the commit.

    So `latest` ends at the newest commit on `main` whose checks and build pass. A failed run leaves the release as it was until a later run succeeds, and a push followed quickly by another may never be published.
12. [ ] The `install`, `uninstall`, `doctor`, `check`, and `eval` commands are gone from the `ocx` binary, along with the code only they used. The `install`, `uninstall`, and `doctor` Makefile targets are gone too. `make apply` builds and runs `ocx apply` from the checkout, for trying a branch on purpose, and passes `ARGS` through.
13. [ ] The README explains how to install with `curl` from the `latest` release or with `go install github.com/plombardi89/ocx/cmd/ocx@main`, that upgrading means getting the new binary and running `ocx apply` again, and how to remove it. This repository's `AGENTS.md` has no instructions that use the removed commands, and `make check` passes.

## Approach

1. A Go file at the repository root, `package ocx`, embeds the files, because `embed` can't reach outside its own package's directory. `make fmt`, the formatting check, and the writing-rules check cover it.
2. The new `internal/apply` package does the work over an `fs.FS`. It replaces `internal/install` and `internal/doctor`.
3. The version comes from `runtime/debug.ReadBuildInfo`: `vcs.revision` and `vcs.modified` for builds in a checkout, or the module version for `go install`.
4. The checks become a Go test that calls `check.Run`, and the eval tool's `main` moves to `internal/tools/eval`.
5. The release job follows the steps in criterion 11. It uses a `concurrency` group with `cancel-in-progress: false`, compares its commit with `git ls-remote origin refs/heads/main` before publishing, and publishes with `gh release upload --clobber` and `gh release edit`. It needs `contents: write`. Right after the merge, both binaries are downloaded from the release and checked against `SHA256SUMS` and with `ocx version`; that's the first real run, and it isn't a condition for review.

## Open questions

None. Phil decided on 2026-11-15:

1. Only Linux amd64 and arm64 binaries.
2. `apply` makes changes by default, and `--dry-run` previews them.
3. One rolling `latest` release, updated on every push to `main`.
4. Keep it simple: no backups and no compatibility with earlier installs.
5. `check` and `eval` belong to the Makefile and the tests, not the shipped binary.
6. The `latest` release ends at the newest commit on `main` whose checks and build pass. A failed run leaves it as it was, and a push followed quickly by another may never be published.
7. `apply` refuses links and file-type conflicts instead of handling them, and a failed file operation isn't rolled back.

## Tasks

1. [ ] Embed the files and add `ocx version` (criteria 1 and 2).
   - Done when: a test reads every expected file from the embedded set, and tests cover the version text for a checkout build, a modified build, and a module build, using fake build information.
2. [ ] `ocx apply` and `ocx remove` (criteria 3 to 9).
   - Done when: unit tests cover a first apply, an apply that drops a file, a dry run, remove, an apply and a remove retried after some listed files are gone, a link and a directory in the way, a listed skill whose directory became a link (for apply's cleanup and for remove), `.ocx-files` entries with `..` and absolute paths, and a failed write of a file and of `.ocx-files` that leaves the old `.ocx-files` whole. Every link and path case has a file outside the configuration directory that must survive unchanged.
3. [ ] Smoke test the built binary (criteria 1, 3, 8, and 12).
   - Done when: a test builds `ocx` with `go build`, and from a directory outside the checkout runs `version`, `apply`, and `remove`, once with `--config-dir` and once with only `XDG_CONFIG_HOME` set to a temporary directory, checking the files after each. `make apply ARGS="--config-dir <temp>"` installs into that directory.
4. [ ] Move `check` into the tests and `eval` into its own program (criterion 10).
   - Done when: `make test` fails when a skill is missing its `name`, and `make eval ARGS=--list` lists the scenarios.
5. [ ] Remove the old commands and code (criterion 12).
   - Done when: `go run ./cmd/ocx` prints only `apply`, `remove`, and `version`, and `make check` passes.
6. [ ] The release workflow (criterion 11).
   - Done when: `GOOS=linux GOARCH=amd64` and `GOARCH=arm64` builds with `CGO_ENABLED=0` succeed locally, and `actionlint` passes on the workflow.
7. [ ] A new ADR that replaces 0007, keeping its decisions about hooks and the development loop and recording this one.
   - Done when: ADR 0007 is deleted and the new one names it in `Replaces:`.
8. [ ] README and `AGENTS.md` (criterion 13).
   - Done when: `make check` passes.
9. [ ] Review and pull request.
   - Done when: the reviewer approves the pushed commit.

## Log

1. 2026-11-15: Plan drafted. Phil asked for an `ocx apply` command that installs or upgrades, built by CI on every push to `main`, because links into a checkout make every branch live everywhere.
2. 2026-11-15: Critique round 1: REVISE (1 BLOCKER, 10 MAJOR, 2 MINOR). The BLOCKER was right: `ocx` only ran inside a checkout. Most MAJORs asked for backups, conversion of old installs, locking, and crash recovery. Phil chose to keep the tool simple instead: no backups, no compatibility with earlier installs, and `check` and `eval` move to the Makefile and tests. Kept from the critique: running from any directory, the ADR replacing 0007, the upgrade steps in the README, queued release runs, and checks before review for the version text and both builds.
3. 2026-11-15: Critique round 2: REVISE (2 BLOCKER, 5 MAJOR, 1 WRITING). A queued workflow can't promise that every push publishes in order, so Phil agreed the release only has to end at the newest commit. Fixed the contradiction about which Makefile targets go. Added simple rules for links, `.ocx-files` entries, and failed file operations, a smoke test of the built binary, and numbered release steps. Doctor's checks of the global install are dropped, as a non-goal.
4. 2026-11-15: Critique round 3: REVISE (1 BLOCKER, 3 MAJOR, 1 MINOR), the last round. Phil approved the plan with these fixes: the release promise now allows for a failed run, the link rule covers deletions and `remove`, `.ocx-files` is replaced by renaming a temporary file, and the task reference is corrected. For the release workflow, he chose a check of the real release right after the merge over testing the publish steps with a fake `gh`.
