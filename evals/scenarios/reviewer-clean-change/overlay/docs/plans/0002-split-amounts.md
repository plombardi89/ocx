# 0002: Split amounts into near-equal parts

Status: in-progress
Branch: feat/split-amounts
Decisions: none

## Goal

Internal callers (invoicing, refunds) need to divide an amount into equal installments without losing or inventing cents.

## Non-goals

- No HTTP endpoint; callers are internal packages.
- Weighted or percentage-based splits.

## Acceptance criteria

1. [ ] `money.Split(total Cents, n int) ([]Cents, error)` returns `n` parts.
2. [ ] The parts always sum exactly to `total`.
3. [ ] Parts differ by at most one cent, and larger parts come first: `Split(100, 3)` returns `[34 33 33]`.
4. [ ] `n` greater than `total` is allowed; the trailing parts are zero: `Split(2, 5)` returns `[1 1 0 0 0]`.
5. [ ] `n < 1`, `n > money.MaxParts` (1000), or a negative `total` return an error wrapping `money.ErrInvalidSplit` and a nil slice.
6. [ ] Works for the full non-negative `Cents` range, including `math.MaxInt64`.

## Approach

Integer division for the base part; the remainder goes one cent at a time to the leading parts.

## Tasks

1. [x] `Split`, `MaxParts`, `ErrInvalidSplit`
   - Done when: table tests, property tests over small ranges, and rejection tests pass.

## Log

1. 2026-11-10: Plan drafted.
2. 2026-11-11: Implemented with tests.
