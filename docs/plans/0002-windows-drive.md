# 0002: Keep agents off the Windows C: drive

Status: done
Branch: feat/block-windows-drive
Decisions: none

## Goal

WSL mounts the Windows C: drive at `/mnt/c` and runs the programs there on the Windows host. A test that ran a fake Windows program there caused popups on Phil's machine, and agents have no business with his Windows files. Policies in `config/opencode.jsonc` now keep every agent from reaching into `/mnt/c` from a project elsewhere.

## Non-goals

These are Phil's decisions, recorded in the Log:

- A project opened under `/mnt/c` isn't covered. OpenCode checks paths inside a project relative to it, so no policy can see them, and opening a project on the drive is Phil's own choice. The README and the config comment say so.
- Windows programs found by name through PATH, such as `powershell.exe`, aren't blocked.
- Other drive letters aren't covered; this machine only mounts C:.
- Paths through a link into `/mnt/c` aren't covered. OpenCode doesn't resolve links before its directory check, although its docs say it does; reported as anomalyco/opencode#64018.
- Paths hidden inside another program's code or a shell variable, such as `python3 -c "open('/mnt/c/x')"`, aren't covered.

## Acceptance criteria

1. [x] For every agent, OpenCode denies `external_directory` for `/mnt/c` and anything under it. In a project outside `/mnt/c`, that blocks the read, edit, glob, and grep tools there, `cd /mnt/c`, and a shell working directory under it, each with "Blocked by configuration policy".
2. [x] For every agent, OpenCode denies any shell command containing `/mnt/c`, whatever character follows it. OpenCode checks each part of a compound command separately. Examples: `ls /mnt/c`, `ls "/mnt/c"`, `ls '/mnt/c'`, `ls /mnt/c>files.txt`, `cat /mnt/c/Windows/win.ini`, and `/mnt/c/Windows/System32/cmd.exe /c ver`.
3. [x] The pattern also denies lookalikes such as `ls /mnt/cache`, and commit messages that mention `/mnt/c`, inline or in a heredoc. Phil accepted both costs; an agent can write such a message to a file and use `git commit -F`.
4. [x] The drive policies come after every policy exception, so `git config --get --file /mnt/c/Users/phil/.gitconfig user.name` and `git config --list --file` on the same file are denied.
5. [x] Probes in `tests/permissions.yaml` cover criteria 1 to 4 for `build`, and criteria 1 and 2 for `plan`, `explore`, and `general`. Each deny probe fails with the drive policies removed, and `make check` passes.
6. [x] The README lists the block and what it doesn't cover.

## Approach

1. Two `tool.use` statements deny `external_directory:/mnt/c` and `external_directory:/mnt/c/*`. OpenCode checks file-tool paths outside the project, `cd` targets, and shell working directories against `external_directory`, but not other paths in a shell command.
2. One statement denies `shell:*/mnt/c*`, for those other paths. A precise pattern that allowed lookalikes kept missing ordinary spellings (a quote, a tab, a redirect right after `/mnt/c`), so Phil chose the broad one.
3. The statements come last among the policies, because the last matching statement wins.

## Open questions

None

## Tasks

1. [x] Add the policies with a comment saying why.
   - Done when: `make check` passes.
2. [x] Add probes for every criterion.
   - Done when: each deny probe fails with the drive policies removed.
3. [x] Check the behavior in a real OpenCode session.
   - Done when: a session with this config shows the results in the Log.
4. [x] Update the README.
   - Done when: it lists the block and the gaps.

## Log

1. 2026-11-14: Phil asked for a rule that keeps agents off `/mnt/c`. He chose `/mnt/c` only, and not blocking Windows programs by name. The work was small enough that it started without a plan; this plan was added during review to record his decisions.
2. 2026-11-14: A real OpenCode 2.2.5 session showed that `external_directory` covers the file tools, `cd`, and working directories, but not path arguments such as `ls /mnt/c/Windows`. Phil chose to add shell text patterns. Reading through a link into `/mnt/c` wasn't blocked either; Phil had it reported as anomalyco/opencode#64018. His home folder has no links into `/mnt/c`, three levels deep.
3. 2026-11-14: Review round 1 found that `ls "/mnt/c"` and a tab got through; patterns for quotes and a tab were added. Round 2 found that the later `git config --get` and `--list` exceptions lifted the deny; the drive policies moved to the end.
4. 2026-11-14: Round 3 found that a project opened under `/mnt/c` isn't covered. Phil chose to document the limit, not enforce it, and allowed a fourth round.
5. 2026-11-14: Round 4 found that a redirect right after `/mnt/c`, as in `ls /mnt/c>files.txt`, got through. Phil chose the single pattern `shell:*/mnt/c*` over chasing more characters, accepting that lookalikes such as `/mnt/cache` are blocked too, wrote down his decisions in this plan, and allowed a fifth round.
6. 2026-11-14: A real OpenCode 2.2.5 session with the single pattern blocked `ls -d /mnt/c>/dev/null` and `ls -d /mnt/cache-ocx-probe`. A commit message naming `/mnt/c`, written with the write tool and committed with `git commit -F`, went through.
7. 2026-11-14: Round 5 accepted the redirect fix and the plan's scope, and found that no probe covered criterion 3's commit messages, so an exception placed after the drive policies could lift that block unnoticed. Probes for an inline and a heredoc message now fail without the shell pattern, and `git commit -F commit-msg.txt` is probed as allowed.
