ocreview: verdict=REQUEST_CHANGES spec=FAIL quality=FAIL base={{.Base}} head={{.Head}}

# Review: feat/product-pagination ({{short .Head}})

**Verdict:** REQUEST_CHANGES
**Spec:** FAIL   **Quality:** FAIL
**Range:** {{short .Base}}..{{short .Head}} (1 commits, 5 files)

## Acceptance criteria
1. MET: `GET /products` accepts `offset` and `limit` (`TestListPagination`).
2. MET: `offset` defaults to 0; `limit` defaults to 20 (`internal/api/api.go`).
3. NOT MET: `limit` is capped at 100 (finding 2).
4. MET: negative or non-integer `offset`/`limit` return 400 with a JSON error (`TestListInvalidParameters`).
5. NOT MET: an `offset` at or past the end returns 200 with an empty `items` array (finding 1).
6. MET: the response is `{"items": [...], "total": N}`.
7. MET: items stay sorted by ID.

## Earlier findings
None

## Findings
1. **[BLOCKER]** `internal/store/store.go:81` - Offsets past the end panic.
   1. Evidence (executed): `TestReviewListPastEnd` panics with `runtime error: slice bounds out of range [3:2]`.
   2. Impact: `GET /products?offset=3` on a two-product catalog aborts the request instead of returning an empty page.
   3. Fix: return an empty, non-nil page when `offset >= len(all)`, and compute the end of the page without overflowing `offset + limit`.
2. **[BLOCKER]** `internal/api/api.go:49` - `limit` is not capped at 100.
   1. Evidence (executed): `TestReviewLimitCap` with 101 products and `?limit=500` returned 101 items.
   2. Impact: clients can request pages of any size, violating the acceptance criterion.
   3. Fix: clamp `limit` to 100 before calling `Store.List`.

## TODOs for Phil
None

## Claims in the request
None

## Harnesses
### `internal/store/review_list_test.go` (finding 1)
Command: `go test -run TestReviewListPastEnd -count=1 ./internal/store/`
Result: FAIL - panic: runtime error: slice bounds out of range [3:2]
```go
package store

import "testing"

func TestReviewListPastEnd(t *testing.T) {
	s := New()
	for _, id := range []string{"a", "b"} {
		if err := s.Put(Product{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	page, total := s.List(3, 20)
	if len(page) != 0 || total != 2 {
		t.Fatalf("List(3, 20) = %v, %d; want an empty page of 2", page, total)
	}
}
```

### `internal/api/review_cap_test.go` (finding 2)
Command: `go test -run TestReviewLimitCap -count=1 ./internal/api/`
Result: FAIL - got 101 items, want 100
```go
package api

import (
	"fmt"
	"testing"

	"example.com/shop/internal/store"
)

func TestReviewLimitCap(t *testing.T) {
	srv, s := newServer(t)
	for i := 0; i < 99; i++ {
		if err := s.Put(store.Product{ID: fmt.Sprintf("p%03d", i), Name: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	_, body := getList(t, srv.URL+"/products?limit=500")
	if len(body.Items) != 100 {
		t.Fatalf("got %d items, want 100", len(body.Items))
	}
}
```

Mutation checks:
None

## Verified
- `go build ./...`, `go vet ./...`, `go test ./...` - PASS on {{short .Head}}.

## Could not verify
- `go test -race ./...` - cgo is not available in this environment.

OCREVIEW verdict=REQUEST_CHANGES spec=FAIL quality=FAIL blockers=2 majors=0 writing=0 minors=0 nits=0 head={{.Head}}
