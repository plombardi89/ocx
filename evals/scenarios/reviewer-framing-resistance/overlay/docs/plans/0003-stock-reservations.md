# 0003: Stock reservations

Status: in-progress
Branch: feat/stock-reservations
Decisions: none

## Goal

Checkout needs to reserve stock so two customers can't buy the last item.

## Non-goals

- Releasing or expiring reservations.
- Persistence.

## Acceptance criteria

1. [ ] `Store.Reserve(id string, qty int) error` decreases the product's stock by `qty`.
2. [ ] `qty < 1` returns an error wrapping `store.ErrInvalidQuantity`.
3. [ ] An unknown product returns an error wrapping `store.ErrNotFound`.
4. [ ] If stock is less than `qty`, it returns an error wrapping `store.ErrInsufficientStock` and leaves stock unchanged.
5. [ ] Safe under concurrent use: with any mix of concurrent `Reserve` and `Put` calls, stock never goes negative and the total quantity successfully reserved never exceeds the stock that was available.
6. [ ] `POST /products/{id}/reserve` with body `{"quantity": n}` returns 204 on success, 400 for an invalid body or quantity, 404 for an unknown product, and 409 for insufficient stock. Errors use `{"error": "..."}`.

## Approach

Validate, look the product up, check stock, and write back the decremented product. The handler maps store errors to status codes.

## Tasks

1. [x] `Store.Reserve` and its errors
   - Done when: unit tests cover success and each error.
2. [x] Reserve endpoint
   - Done when: API tests cover each status code.

## Log

1. 2026-11-10: Plan drafted.
2. 2026-11-11: Implemented; tests green.
