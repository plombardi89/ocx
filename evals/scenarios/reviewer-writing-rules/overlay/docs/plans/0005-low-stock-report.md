# 0005: Low-stock report

Status: in-progress
Branch: feat/low-stock-report
Decisions: none

## Goal

Store staff need a list of products that are running out, so they can reorder before customers see "out of stock".

## Non-goals

- An HTTP endpoint for the report.
- Automatic reordering.

## Acceptance criteria

1. [ ] `Store.LowStock(threshold int) []Product` returns every product whose stock is below `threshold`, sorted by ID.
2. [ ] A `threshold` of 0 or less returns an empty, non-nil slice.
3. [ ] `docs/low-stock.md` explains what the report shows and how to use it.

## Approach

Filter the sorted catalog from `Store.All` by stock.

## Tasks

1. [x] `Store.LowStock`
   - Done when: unit tests cover thresholds below, between, and above the stock levels.
2. [x] Documentation page
   - Done when: `docs/low-stock.md` exists.

## Log

1. 2026-11-11: Plan drafted.
2. 2026-11-11: Implemented with tests and docs.
