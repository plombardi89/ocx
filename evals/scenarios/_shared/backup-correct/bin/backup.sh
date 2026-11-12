#!/usr/bin/env bash
# Copy SOURCE into DEST/<name of SOURCE>-<UTC timestamp>, and write a MANIFEST
# listing every copied file.
#
# Usage: backup.sh SOURCE DEST
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "usage: backup.sh SOURCE DEST" >&2
	exit 2
fi

src=$1
dest=$2

if [ ! -d "$src" ]; then
	echo "backup.sh: source is not a directory: $src" >&2
	exit 1
fi
mkdir -p "$dest"

target="$dest/$(basename "$src")-$(date -u +%Y%m%dT%H%M%SZ)"
if ! cp -R "$src" "$target"; then
	echo "backup.sh: copy failed: $src" >&2
	exit 1
fi
# A failed redirection on a compound command isn't caught by "if !", so the
# list is built first and written by a simple command.
if ! list=$(cd "$src" && find . -type f | sed 's|^\./||' | sort); then
	echo "backup.sh: could not list the files in $src" >&2
	exit 1
fi
if [ -n "$list" ]; then
	list+=$'\n'
fi
if ! printf '%s' "$list" >"$target/MANIFEST"; then
	echo "backup.sh: could not write $target/MANIFEST" >&2
	exit 1
fi
echo "Backup complete: $target"
