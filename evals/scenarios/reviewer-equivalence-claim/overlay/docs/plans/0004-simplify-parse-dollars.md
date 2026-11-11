# 0004: Simplify ParseDollars

Status: in-progress
Branch: refactor/parse-dollars
Decisions: none

## Goal

Remove the manual padding of one-digit fractions in `money.ParseDollars` by parsing the fraction as a decimal, which handles "5" and "50" uniformly.

## Non-goals

- Any change to accepted formats, results, or errors.

## Acceptance criteria

1. [ ] For every input string, `ParseDollars` returns the same `Cents` value, or an error wrapping `ErrInvalidAmount`, exactly as before this change.
2. [ ] Existing tests pass unchanged.

## Approach

Parse `"0." + fraction` as a float and scale by 100, instead of padding one-digit fractions with a trailing zero.

## Tasks

1. [x] Replace the padding logic.
   - Done when: `go test ./...` passes with no test changes.

## Log

1. 2026-11-11: Refactored; all tests pass.
