# 0008: Split amounts into installments

Status: draft
Branch: feat/split-amounts
Decisions: none

## Goal

Invoicing and refunds code needs to divide an amount into equal installments without losing or inventing cents.

## Non-goals

- A web endpoint; only Go code in this module calls it.
- Weighted or percentage-based splits.
- Currencies other than the United States dollar.

## Acceptance criteria

1. [ ] `money.Split(total Cents, n int) ([]Cents, error)` returns exactly `n` parts when it succeeds.
2. [ ] The parts always sum exactly to `total`.
3. [ ] Parts differ by at most one cent, and larger parts come first: `Split(100, 3)` returns `[34 33 33]`, and `Split(101, 3)` returns `[34 34 33]`.
4. [ ] `n` greater than `total` is allowed, and the trailing parts are zero: `Split(2, 5)` returns `[1 1 0 0 0]`, and `Split(0, 3)` returns `[0 0 0]`.
5. [ ] It works for every non-negative `total` up to `math.MaxInt64`: `Split(math.MaxInt64, 2)` returns `[4611686018427387904 4611686018427387903]`.
6. [ ] `n < 1`, `n > money.MaxParts`, or `total < 0` return a nil slice and an error wrapping `money.ErrInvalidSplit`. `MaxParts` is 1000, and `Split(x, 1000)` succeeds.

## Approach

1. Reject invalid arguments first (criterion 6).
2. Compute `base := total / n` and `remainder := total % n` with integer division, so no floating point is involved and nothing can overflow.
3. Give the first `remainder` parts `base + 1` cents and the rest `base`.

`MaxParts` caps the allocation at 1000 parts, which is far above the longest installment plan the invoicing code offers (120 months), and stops a bad argument from allocating gigabytes.

## Open questions

None

## Tasks

1. [ ] Add `Split`, `MaxParts`, and `ErrInvalidSplit` in `internal/money/split.go`.
   - Done when: `TestSplit` passes with the examples from criteria 3, 4, and 5.
2. [ ] Add a property test.
   - Done when: `TestSplitProperties` checks every `total` from 0 to 250 with every `n` from 1 to 12 for criteria 1, 2, and 3.
3. [ ] Add rejection and limit tests.
   - Done when: `TestSplitRejects` covers `n` of 0, -1, and 1001, and `total` of -1 and `math.MinInt64`, each returning a nil slice and `ErrInvalidSplit`; and `TestSplitMaxParts` shows `Split(1001, 1000)` succeeds with 1000 parts.

## Log

1. 2026-11-11: Plan drafted.
