# 0004: Keep agents off the Windows C: drive

Status: accepted
Date: 2026-11-14
Replaces: none

## Context

WSL mounts the Windows C: drive at `/mnt/c` and runs the programs there on the Windows host. A test that ran a fake Windows program there caused popups on Phil's machine, and agents have no business with his Windows files.

## Decision

1. Policies in `config/opencode.jsonc` deny every agent `external_directory` for `/mnt/c`, and any shell command containing `/mnt/c`. They come last among the policies, so no exception can lift them.
2. The shell pattern is broad on purpose: it also blocks lookalikes such as `/mnt/cache`, and commit messages that mention the drive. Agents write such a message to a file and use `git commit -F`. Precise patterns kept missing ordinary spellings, such as a quote or a redirect right after the path.
3. Only `/mnt/c` is covered, the only drive mounted here.
4. Not covered: a project opened under `/mnt/c` itself, Windows programs found by name through PATH, paths hidden in another program's code, and paths through a link into the drive. OpenCode doesn't resolve links before its directory check, although its docs say it does: anomalyco/opencode#64018.

## Alternatives considered

- Blocking every Windows drive letter: only C: is mounted.
- Blocking `.exe` in commands: it would also block text that mentions one.
- Enforcing the rule for projects on the drive with a plugin: new tooling, for a case Phil chooses himself.

## Consequences

Some harmless commands that mention `/mnt/c` are blocked. If OpenCode fixes its link handling, the link gap closes without a change here.
