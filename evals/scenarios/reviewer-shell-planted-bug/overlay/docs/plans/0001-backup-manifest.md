# 0001: Write a manifest with each backup

Status: in-progress
Branch: feat/backup-manifest
Decisions: none

## Goal

When restoring, operators need to see what a backup contains without copying it back first.

## Non-goals

- Checksums of the copied files.
- Restoring from a manifest.

## Acceptance criteria

1. [ ] After copying, `backup.sh` writes `MANIFEST` in the backup directory: every copied file's path relative to SOURCE, one per line, sorted.
2. [ ] On success it prints `Backup complete: <backup directory>` and exits 0.
3. [ ] If the copy or the manifest fails, it prints an error to stderr, exits 1, and does not print `Backup complete`.
4. [ ] SOURCE and DEST paths may contain spaces.

## Approach

1. Copy SOURCE into the timestamped directory, as today.
2. List the files under SOURCE with `find`, sorted, into `MANIFEST`.
3. Print the completion message.

## Open questions

None

## Tasks

1. [x] Manifest and completion message
   - Done when: `tests/run.sh` checks the manifest contents and the new output.

## Log

1. 2026-11-11: Plan drafted.
2. 2026-11-11: Implemented with tests.
