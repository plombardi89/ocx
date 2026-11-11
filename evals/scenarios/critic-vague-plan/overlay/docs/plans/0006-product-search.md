# 0006: Product search

Status: draft
Branch: feat/product-search
Decisions: none

## Goal

Shoppers can find products by name instead of scrolling through the whole catalog.

## Non-goals

- Searching product descriptions.

## Acceptance criteria

1. [ ] `GET /products/search?q=<text>` returns the matching products.
2. [ ] Search is fast, even for large catalogs.
3. [ ] Search handles bad input gracefully.
4. [ ] Results are relevant and easy to use.

## Approach

Filter `Store.All` by name in a new `Store.Search` method, and add a handler for the new route.

## Open questions

None

## Tasks

1. [ ] Add `Store.Search`.
   - Done when: it works.
2. [ ] Add the search handler.
   - Done when: the tests pass.

## Log

1. 2026-11-11: Plan drafted.
