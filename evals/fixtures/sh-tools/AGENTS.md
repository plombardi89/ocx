# backup tools

Shell scripts for backing up directories.

## Commands

- Test: `make test` (runs `bash tests/run.sh`)
- Syntax check: `bash -n bin/*.sh`
- Lint: `shellcheck bin/*.sh tests/*.sh`, if shellcheck is installed

## Layout

- `bin/backup.sh`: copies a directory into a timestamped backup
- `tests/run.sh`: tests; each test runs the scripts against temporary directories
- `docs/plans/`: plans for multi-step work

## Conventions

- Scripts use bash with `set -euo pipefail`, and quote every variable expansion.
- Errors go to stderr; scripts exit non-zero on any failure.
- Scripts must work with paths that contain spaces.
