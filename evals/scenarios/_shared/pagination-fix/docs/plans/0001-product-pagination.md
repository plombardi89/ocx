# 0001: Product list pagination

Status: in-progress
Branch: feat/product-pagination
Decisions: none

## Goal

`GET /products` returns the catalog one page at a time, so clients with large catalogs don't download everything at once.

## Non-goals

- Cursor-based pagination.
- Filtering or sorting options.

## Acceptance criteria

1. [ ] `GET /products` accepts `offset` and `limit` query parameters.
2. [ ] `offset` defaults to 0; `limit` defaults to 20.
3. [ ] `limit` is capped at 100: `GET /products?limit=500` returns at most 100 items.
4. [ ] Negative or non-integer `offset` or `limit` returns 400 with `{"error": "..."}`.
5. [ ] An `offset` at or past the end of the catalog returns 200 with an empty `items` array.
6. [ ] The response is `{"items": [...], "total": N}`, where `total` is the number of products in the catalog.
7. [ ] Items stay sorted by ID.

## Approach

Add `Store.List(offset, limit)` returning one page and the total count. The handler parses and validates the query parameters, caps the limit, and calls it.

## Tasks

1. [x] `Store.List`
   - Done when: unit test covers paging through the catalog.
2. [x] Handler parameters and response shape
   - Done when: API tests cover defaults, paging, and invalid parameters.
3. [x] Review round 1 findings: past-end offsets, limit cap
   - Done when: both are fixed and covered by regression tests.

## Log

1. 2026-11-10: Plan drafted.
2. 2026-11-11: Implemented store and handler; tests green.
3. 2026-11-11: Review round 1 requested changes (past-end panic, missing limit cap). Fixed both; empty and malformed parameters now return 400.
