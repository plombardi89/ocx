#!/usr/bin/env bash
# Tests for hooks/pre-push. Each test pushes from a fresh repository to a
# fresh bare remote.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

fail() {
	echo "FAIL: $1" >&2
	failures=$((failures + 1))
}

# new_repo DIR: a repository on branch feat/x with one commit, the hook
# installed, and an empty bare remote named origin.
new_repo() {
	git init -q --bare "$1/origin.git"
	git init -q -b main "$1/repo"
	git -C "$1/repo" config user.name test
	git -C "$1/repo" config user.email test@example.com
	git -C "$1/repo" remote add origin "$1/origin.git"
	cp "$root/hooks/pre-push" "$1/repo/.git/hooks/pre-push"
	chmod +x "$1/repo/.git/hooks/pre-push"
	git -C "$1/repo" switch -q -c feat/x
	echo change >"$1/repo/file"
	git -C "$1/repo" add file
	git -C "$1/repo" commit -q -m change
}

# note DIR VERDICT HEAD: record a review note on DIR's HEAD naming HEAD.
note() {
	local sha
	sha=$(git -C "$1" rev-parse HEAD)
	git -C "$1" notes --ref=ocreview add -f -m "ocreview: verdict=$2 spec=NA quality=PASS base=x head=$3" "$sha"
}

approve() {
	note "$1" APPROVE "$(git -C "$1" rev-parse HEAD)"
}

test_rejects_an_unapproved_commit() {
	new_repo "$work/unapproved"
	if git -C "$work/unapproved/repo" push -q origin feat/x 2>/dev/null; then
		fail "an unapproved commit was pushed"
	fi
}

test_allows_an_approved_commit() {
	new_repo "$work/approved"
	approve "$work/approved/repo"
	git -C "$work/approved/repo" push -q origin feat/x 2>/dev/null || fail "an approved commit was rejected"
}

test_rejects_main_even_when_approved() {
	new_repo "$work/main"
	approve "$work/main/repo"
	if git -C "$work/main/repo" push -q origin feat/x:main 2>/dev/null; then
		fail "a push to main was allowed"
	fi
}

test_rejects_a_commit_added_after_the_approval() {
	new_repo "$work/other"
	approve "$work/other/repo"
	echo more >>"$work/other/repo/file"
	git -C "$work/other/repo" commit -q -am more
	if git -C "$work/other/repo" push -q origin feat/x 2>/dev/null; then
		fail "a commit added after the approval was pushed"
	fi
}

test_rejects_requested_changes() {
	new_repo "$work/changes"
	note "$work/changes/repo" REQUEST_CHANGES "$(git -C "$work/changes/repo" rev-parse HEAD)"
	if git -C "$work/changes/repo" push -q origin feat/x 2>/dev/null; then
		fail "a commit with requested changes was pushed"
	fi
}

test_rejects_an_approval_naming_another_commit() {
	new_repo "$work/named"
	note "$work/named/repo" APPROVE 0000000000000000000000000000000000000000
	if git -C "$work/named/repo" push -q origin feat/x 2>/dev/null; then
		fail "an approval naming another commit was accepted"
	fi
}

test_rejects_an_unapproved_commit
test_rejects_requested_changes
test_rejects_an_approval_naming_another_commit
test_allows_an_approved_commit
test_rejects_main_even_when_approved
test_rejects_a_commit_added_after_the_approval

if [ "$failures" -gt 0 ]; then
	echo "$failures hook test(s) failed" >&2
	exit 1
fi
echo "all hook tests passed"
