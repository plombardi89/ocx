# shop

A small in-memory product catalog served over HTTP.

## Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Race detector: `go test -race ./...` (needs cgo and a C compiler; if it can't run, say so)
- Vet: `go vet ./...`
- Run: `go run ./cmd/shop -addr 127.0.0.1:8080`

## Layout

- `internal/money/`: currency amounts as integer cents
- `internal/store/`: concurrency-safe in-memory catalog
- `internal/api/`: HTTP handlers (Go 1.22 `ServeMux` patterns)
- `cmd/shop/`: server entry point
- `docs/plans/`: plans for multi-step work

## Conventions

- Money is always `money.Cents`; never use floats for amounts.
- Store methods are safe for concurrent use; handlers hold no state of their own.
- Errors returned to HTTP clients are JSON: `{"error": "..."}`.
