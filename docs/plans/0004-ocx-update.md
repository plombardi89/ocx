# 0004: `ocx update` replaces itself with the latest release

Status: in-progress
Branch: feat/ocx-update
Decisions: [0009](../adr/0009-ocx-updates-itself.md)

## Goal

Upgrading `ocx` today means downloading the new binary by hand, checking it against `SHA256SUMS`, installing it, and running `ocx apply`. A new `ocx update` command does all of that: it downloads the latest release for this machine, checks it, replaces the running binary, and with `--apply` installs the new configuration.

## Non-goals

- Proving who built a release. `SHA256SUMS` comes from the same place as the binary, so the check catches a corrupted or truncated download, not a tampered release; HTTPS to GitHub is what vouches for the source.
- Choosing a version. `ocx update` always installs the binary at the release location when its bytes differ from the running one, even when the running one is newer, such as a `go install` build from `main` or a binary from a mirror that's behind. There's no version comparison.
- Two `ocx update` commands running at the same time on the same binary.
- Automatic or background updates.
- Builds for systems other than Linux amd64 and arm64.
- Updating a binary in a directory the user can't write to, such as `/usr/local/bin`. `ocx update` never uses `sudo`.

## Acceptance criteria

1. [ ] `ocx update` downloads `SHA256SUMS` from the release location and finds the line for this machine's binary, `ocx-linux-amd64` or `ocx-linux-arm64`. The file must have exactly one line for that name, in `sha256sum`'s format: 64 hexadecimal digits, two spaces, and the name. On any other operating system or processor, it exits with status 1 and names the system, without downloading anything.
2. [ ] The release location is `https://github.com/plombardi89/ocx/releases/latest/download`, or the value of `OCX_RELEASE_URL` when it's set, with a trailing slash ignored. The files are `<location>/SHA256SUMS` and `<location>/ocx-linux-<arch>`.
3. [ ] When the SHA-256 checksum of the running binary's file, after following links to the real file, matches the line in `SHA256SUMS`, `ocx update` prints `Already up to date: <version>`, downloads nothing else, and changes nothing.
4. [ ] Otherwise it:
    1. Downloads the binary and checks its SHA-256 checksum against `SHA256SUMS`.
    2. Writes it to a new temporary file in the same directory as the running binary's real file, and makes it executable (0755).
    3. Runs it with `version`, which must exit with status 0 within 10 seconds and print at least one line. Only the first line is used, so `version` can print more later.
    4. Renames the temporary file over the running binary's real file. A link that pointed to it still does.
    5. Prints `Updated ocx from <old version> to <new version>.`, with the first line of each binary's `version`.
5. [ ] If anything in criteria 1 to 4 fails, `ocx update` exits with status 1, prints what failed, and leaves the old binary byte for byte as it was, with no temporary file left behind. That covers a network error or a download cut off part way, an HTTP status other than 200 (naming the address and the status), a download over 100 MiB, `SHA256SUMS` without exactly one valid line for this machine's binary, a checksum mismatch, a new binary whose `version` fails, takes longer than 10 seconds, or prints nothing, and a directory it can't write to. For a directory it can't write to, the message names the directory and suggests moving `ocx` to one the user can write to, such as `~/.local/bin`.
6. [ ] `ocx update --check` does steps 1 to 3 only. It prints `Already up to date: <version>` or `An update is available.`, exits with status 0 either way, and changes nothing. It exits with status 1 only when the check itself fails.
7. [ ] `ocx update --apply` runs the new binary's `apply` after replacing it, or the current binary's `apply` when it was already up to date, passing `--config-dir DIR` along when it's given. Its output and exit status become those of `ocx update`. If `apply` fails, the binary stays replaced, and running `ocx apply` again finishes the job.
8. [ ] `--check` together with `--apply`, an unknown flag, or an extra argument is a usage error: exit status 2, and nothing is downloaded or changed. `--config-dir` without `--apply` is a usage error too.
9. [ ] The usage text lists `apply`, `remove`, `update`, and `version`, and so do the README's layout and this repository's `AGENTS.md`. The adopted test `TestUsageListsOnlyApplyRemoveAndVersion` expects exactly those four commands, renamed `TestUsageListsOnlyTheFourCommands`, as the user approved.
10. [ ] A new ADR replaces ADR 0008: it restates 0008's decisions, adds `ocx update` and `OCX_RELEASE_URL`, and ADR 0008 is deleted.
11. [ ] The README's upgrade step is `ocx update --apply`, still saying that upgrading gets the new binary and runs `ocx apply` again. It notes that `ocx update` installs the release's binary whenever it differs from the running one, so a `go install` build is replaced by the release build. `make check` passes.

## Approach

1. A new `internal/update` package does steps 1 to 5 over an `http.Client` with a two-minute timeout, the release location, the running binary's path, and the machine's operating system and processor, so tests can supply each. `cmd/ocx` adds the `update` command, and `--apply` runs the new binary with `os/exec`.
2. The running binary's path comes from `os.Executable` and `filepath.EvalSymlinks`.
3. The temporary file is created with `os.CreateTemp` in the binary's directory, so the rename stays on one file system and can't leave a half-written `ocx`.

## Open questions

None. The user decided on 2026-11-15:

1. `ocx update --check` reports whether an update is available without changing anything.
2. `OCX_RELEASE_URL` can point the download at another location, for tests and for mirrors or forks.
3. The adopted test that expects exactly three commands may expect four, including `update`.
4. Two `ocx update` commands at once aren't supported; there's no lock.
5. `ocx update` always installs the release's binary when it differs, with no version comparison.
6. The new binary counts as working when `version` exits with status 0 within 10 seconds and prints at least one line, so its output can grow later.

## Tasks

1. [x] `internal/update` (criteria 1 to 6).
   - Done when: unit tests against an `httptest` server cover:
     1. Up to date, `--check` both ways, and a newer build.
     2. An HTTP 404, a download cut off part way, a download over the limit, and a timeout, using a short timeout in the test.
     3. `SHA256SUMS` with no line, two lines, and an invalid checksum for this machine's binary, and a checksum mismatch.
     4. A new binary whose `version` fails, hangs past a short limit in the test, or prints nothing, and one that prints two lines.
     5. A directory that can't be written to, an unsupported system, a write of the temporary file that fails part way, and a rename that fails after the new binary's `version` succeeded. The last two errors name the failed operation.
     6. Replacing a binary reached through a link: the link stays and points to the new binary, and other files in both directories are unchanged.
     7. The default release location, and a trailing slash in `OCX_RELEASE_URL`, without the public network.

     Each failure case checks that the old binary is unchanged and no temporary file is left.
2. [x] The `update` command in `cmd/ocx` (criteria 7 to 9).
   - Done when: tests cover the usage errors in criterion 8, and a smoke test builds two binaries stamped with different versions and serves the newer one from an `httptest` server through `OCX_RELEASE_URL`. With the older one, it runs:
     1. `ocx update --apply --config-dir <temp>`, and checks that the binary file now reports the newer version, that the output includes `Applied <newer version>`, which the old binary can't print, and that the configuration was applied.
     2. `ocx update --apply` again, which is already up to date and still applies.
     3. `ocx update --apply`, from a fresh copy of the older binary, with a `--config-dir` that `apply` refuses: it replaces the binary, exits with `apply`'s status 1 and its message, and the installed file still reports the newer version afterwards.

     A unit test also serves a stand-in binary whose `version` succeeds and whose `apply` prints one line to standard output, another to standard error, and exits with status 7. `ocx update --apply` must pass both lines through unchanged and exit with status 7.
3. [x] Update the adopted usage test (criterion 9), and rename `TestOnlyThreeCommands` in `cmd/ocx/main_test.go` to `TestRemovedCommandsAreUsageErrors`, which is what it checks, without changing its assertions.
   - Done when: both pass.
4. [x] A new ADR that replaces 0008 (criterion 10).
   - Done when: ADR 0008 is deleted and the new one names it in `Replaces:`.
5. [x] README and this repository's `AGENTS.md` (criteria 9 and 11).
   - Done when: `make check` passes.
6. [ ] Independent tests, review, and pull request.
   - Done when: the reviewer approves the pushed commit.

## Log

1. 2026-11-15: Plan drafted. The user asked for `ocx update`, which downloads and checks the latest binary, replaces the current one, and with `--apply` runs `ocx apply`. They chose `--check`, `OCX_RELEASE_URL`, and the usual plan, test, and review loop, and approved updating the adopted usage test.
2. 2026-11-15: Critique round 1: REVISE (5 MAJOR, 1 WRITING, 1 MINOR). The user decided: two updates at once aren't supported; `ocx update` always installs the release's binary when it differs; and the new binary counts as working when `version` exits 0 within 10 seconds with at least one line, so its output can grow. Added a strict `SHA256SUMS` format, numbered the replacement steps, made failed `apply` behavior explicit, expanded the tests (a link to the binary, forwarded exit status, an already-current `--apply`, the old binary's `apply` not being used), and added the other command listings to the docs task.
3. 2026-11-15: Critique round 2: REVISE (2 MAJOR), both about tests. Added tests for a failed write of the temporary file and a failed rename, and for `apply` failing right after a replacement, with a stand-in binary whose `apply` exits 7 to show its output and status pass through unchanged.
4. 2026-11-15: The user approved the plan with round 2's test additions, without a third critique round.
5. 2026-11-15: Tasks 1 to 5 done. The `update` command can't replace the test binary in its own tests, so they swap the path it replaces for a temporary file, and the smoke test runs real builds as separate processes. `run` now maps a usage error to exit status 2 and passes a child `apply`'s exit status through. `make check` and `make check-offline` pass.
