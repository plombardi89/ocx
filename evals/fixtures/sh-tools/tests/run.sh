#!/usr/bin/env bash
# Tests for bin/backup.sh. Each test works in its own temporary directory.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

fail() {
	echo "FAIL: $1" >&2
	failures=$((failures + 1))
}

test_copies_the_source_tree() {
	mkdir -p "$work/copy/src/sub"
	echo hello >"$work/copy/src/a.txt"
	echo world >"$work/copy/src/sub/b.txt"
	local out
	out=$(bash "$root/bin/backup.sh" "$work/copy/src" "$work/copy/dest")
	[ -f "$out/a.txt" ] || fail "a.txt was not copied"
	[ -f "$out/sub/b.txt" ] || fail "sub/b.txt was not copied"
}

test_rejects_a_missing_source() {
	if bash "$root/bin/backup.sh" "$work/missing" "$work/dest" 2>/dev/null; then
		fail "a missing source was accepted"
	fi
}

test_requires_two_arguments() {
	if bash "$root/bin/backup.sh" 2>/dev/null; then
		fail "no arguments were accepted"
	fi
}

test_copies_the_source_tree
test_rejects_a_missing_source
test_requires_two_arguments

if [ "$failures" -gt 0 ]; then
	echo "$failures test(s) failed" >&2
	exit 1
fi
echo "all tests passed"
