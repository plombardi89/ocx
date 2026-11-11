# Go review checklist

Check each item that applies to the change. Each one is a common source of real bugs.

## Errors
- Every returned error is checked. `_ = f()` and bare `f()` calls on functions that return errors need a reason.
- Errors are wrapped with `%w` when callers may test them with `errors.Is` or `errors.As`; sentinel comparisons use `errors.Is`, not `==`, once wrapping is involved.
- Errors are not both logged and returned (double reporting), and not swallowed by logging alone.
- `defer f.Close()` on a file opened for writing loses the close error; writes need an explicit checked `Close`.

## Slices, maps, and nil
- Slice expressions `s[a:b]`: can `a > b`, or either bound exceed `len(s)`, on any reachable input?
- Index access after a length check that uses the wrong bound (`<=` vs `<`).
- Writes to a nil map panic; maps in structs need initialization in every constructor.
- Returning an internal slice or map lets callers mutate shared state; check whether a copy is needed.
- `append` to a slice that aliases another slice's backing array.

## Concurrency
- Any write (including `m[k]++` and `delete`) while holding only `RLock` is a data race. With maps, it can crash the process with "concurrent map writes".
- Shared state touched from goroutines or HTTP handlers needs a lock or atomic. `go test -race` only finds races that tests execute; read the code too.
- `sync.Mutex` or a struct containing one copied by value (value receivers, range copies).
- Goroutines without a way to finish: no context cancellation, unbuffered channel sends with no receiver, `time.After` in long loops.
- `context.Context` is passed through and its cancellation respected; contexts are not stored in structs.
- `sync.WaitGroup.Add` called inside the goroutine instead of before it.

## HTTP and I/O
- Response bodies closed on every path (`defer resp.Body.Close()` after the error check).
- Request bodies size-limited (`http.MaxBytesReader`) before decoding untrusted input.
- Handlers set status codes before writing the body; errors are not reported as 200.
- Servers and clients have timeouts; no `http.Get` with the default client in production paths.
- JSON: struct tags match the documented API, `omitempty` doesn't hide meaningful zero values, unknown fields are handled deliberately.
- `strconv` parse errors handled; integer conversions can't overflow (`int64` to `int`, `len` to `int32`).

## API and behavior
- Exported identifiers changed or removed: find every caller. Is this a breaking change the plan intends?
- Zero values of new types are safe or documented as unusable.
- Time: `time.Now()` in logic that needs to be testable; time zones; `time.Duration` unit mistakes.

## Tests
- Table tests cover boundaries and error paths, not just the happy path.
- Assertions check values, not just `err == nil`.
- `t.Parallel()` tests share mutable fixtures, or loop variables are captured incorrectly.
- Tests that sleep for synchronization instead of waiting on a condition.
- New behavior in a concurrent path has a test that runs callers concurrently under `-race`.
