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

backup_dir() {
	local out=$1
	echo "${out#Backup complete: }"
}

test_copies_the_source_tree() {
	mkdir -p "$work/copy/src/sub"
	echo hello >"$work/copy/src/a.txt"
	echo world >"$work/copy/src/sub/b.txt"
	local out target
	out=$(bash "$root/bin/backup.sh" "$work/copy/src" "$work/copy/dest")
	case "$out" in
	"Backup complete: "*) ;;
	*) fail "unexpected output: $out" ;;
	esac
	target=$(backup_dir "$out")
	[ -f "$target/a.txt" ] || fail "a.txt was not copied"
	[ -f "$target/sub/b.txt" ] || fail "sub/b.txt was not copied"
}

test_writes_a_sorted_manifest() {
	mkdir -p "$work/manifest/src/z" "$work/manifest/src/a"
	echo 1 >"$work/manifest/src/z/last.txt"
	echo 2 >"$work/manifest/src/a/first.txt"
	echo 3 >"$work/manifest/src/middle.txt"
	local target
	target=$(backup_dir "$(bash "$root/bin/backup.sh" "$work/manifest/src" "$work/manifest/dest")")
	local want
	want=$(printf '%s\n' a/first.txt middle.txt z/last.txt)
	[ "$(cat "$target/MANIFEST")" = "$want" ] || fail "manifest was: $(cat "$target/MANIFEST")"
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
test_writes_a_sorted_manifest
test_rejects_a_missing_source
test_requires_two_arguments

if [ "$failures" -gt 0 ]; then
	echo "$failures test(s) failed" >&2
	exit 1
fi
echo "all tests passed"
