# Shell review checklist

Check each item that applies to the change. Run `bash -n` and, if available, `shellcheck` on every changed script.

## Robustness
- `set -euo pipefail` (bash) or deliberate error handling; with `set -e`, failures inside `if`, `&&`, `||`, and command substitutions are not caught.
- Unquoted variable expansions (`$var`, `$(cmd)`) that can contain spaces, globs, or be empty. `rm -rf "$dir/"*` with an empty `$dir` is a disaster.
- `cd` without `|| exit` (or under `set -e` in a subshell where that doesn't apply).
- `[ $a = $b ]` with unquoted operands; `==` in POSIX `sh`.
- Bashisms (`[[ ]]`, arrays, `local` semantics, `$'...'`) in scripts with a `#!/bin/sh` shebang.
- Parsing `ls` output; `for f in $(find ...)` breaking on spaces; prefer `find -print0 | xargs -0` or globs.
- `read` without `-r`; missing `IFS=` when whitespace matters.

## Safety
- Temporary files created with fixed names instead of `mktemp`, and not cleaned up with `trap`.
- Destructive commands (`rm -rf`, `git reset --hard`, `git clean -fdx`, overwriting redirects) on paths that come from variables or arguments.
- `eval` or `sh -c` on input from arguments, environment, or files.
- Secrets passed on the command line (visible in `ps`) or echoed in logs (`set -x`).
- `curl | sh` patterns; downloads without checksum verification.

## Portability
- GNU-only flags (`sed -i` without a suffix argument on macOS, `readlink -f`, `date -d`) when the script must run on macOS.
- Hard-coded paths to tools instead of `command -v` checks.

## Behavior
- Exit codes: does the script exit non-zero on failure so callers notice?
- Output: errors to stderr, data to stdout.
