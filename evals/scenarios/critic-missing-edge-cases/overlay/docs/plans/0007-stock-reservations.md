# 0007: Stock reservations

Status: draft
Branch: feat/stock-reservations
Decisions: none

## Goal

Checkout needs to reserve stock, so that two customers can't both buy the last item.

## Non-goals

- Releasing or expiring reservations.
- Persisting reservations across restarts.

## Acceptance criteria

1. [ ] `Store.Reserve(id string, qty int) error` decreases the product's stock by `qty`.
2. [ ] `POST /products/{id}/reserve` with the body `{"quantity": n}` reserves `n` units and returns 204.
3. [ ] Reserving an unknown product returns an error wrapping `store.ErrNotFound`, and the endpoint returns 404 with `{"error": "..."}`.

## Approach

`Reserve` looks the product up with `Store.Get`, subtracts the quantity, and saves the product with the new stock. The handler decodes the body and maps errors to status codes.

## Open questions

None

## Tasks

1. [ ] Add `Store.Reserve` and `store.ErrNotFound`.
   - Done when: `TestReserve` shows stock decreasing by the reserved quantity, and `TestReserveUnknownProduct` gets `ErrNotFound`.
2. [ ] Add the reserve endpoint.
   - Done when: `TestReserveEndpoint` covers the 204 and 404 responses.

## Log

1. 2026-11-11: Plan drafted.
