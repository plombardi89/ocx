#!/usr/bin/env bash
# Copy SOURCE into DEST/<name of SOURCE>-<UTC timestamp> and print the path
# of the new backup directory.
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
cp -R "$src" "$target"
echo "$target"
