# 0001: Tag new products

Status: in-progress
Branch: feat/product-tags
Decisions: none

## Goal

Store staff want new products highlighted in the catalog until someone has reviewed them.

## Non-goals

- Searching or filtering by tag.
- Persisting tags.

## Acceptance criteria

1. [ ] `Catalog.add` accepts an optional `tags` list. The new product's tags are the given tags followed by `"new"`.
2. [ ] Each product's tags are independent: changing one product's tags never changes another product's tags, or the list the caller passed in.
3. [ ] `Catalog.mark_seen(product_id)` removes `"new"` from that product only. Calling it on a product that isn't tagged `"new"` does nothing. An unknown ID raises `KeyError`.

## Approach

Store the tags on the `Product` record. `add` appends `"new"`; `mark_seen` removes it.

## Open questions

None

## Tasks

1. [x] Tags on `add`, and `mark_seen`
   - Done when: unit tests cover a new product's tags and marking it seen.

## Log

1. 2026-11-11: Plan drafted.
2. 2026-11-11: Implemented with tests.
