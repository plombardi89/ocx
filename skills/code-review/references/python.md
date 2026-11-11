# Python review checklist

Check each item that applies to the change.

## Correctness
- Mutable default arguments (`def f(x=[])`, `def f(d={})`) shared across calls.
- `is` used for value comparison (`x is 0`, `s is "a"`); `==` against `None` instead of `is None`.
- Truthiness bugs: `if value:` when `0`, `""`, or `[]` are valid values.
- Integer vs float division, `round()` banker's rounding, float money math (use `Decimal` or integer cents).
- Late-binding closures in loops (`lambda: i`).
- Iterating over a dict or list while mutating it.
- `datetime.now()` without a timezone mixed with aware datetimes.

## Errors and resources
- Bare `except:` or `except Exception:` that swallows errors, including `KeyboardInterrupt` with bare except.
- Exceptions re-raised without `from` when the cause matters, or with the traceback lost.
- Files, sockets, locks, and DB connections opened without `with`.
- `subprocess` calls without `check=True` (or explicit return code handling) and without timeouts.

## Security
- `subprocess` with `shell=True` and interpolated input; `os.system`.
- SQL built with f-strings or `%` instead of parameters.
- `pickle`, `yaml.load` (not `safe_load`), or `eval` on untrusted input.
- Paths joined from user input without normalization and containment checks.
- Secrets logged or included in exceptions.

## Concurrency and async
- Blocking calls (`requests`, `time.sleep`, file I/O) inside `async def`.
- Coroutines that are created but never awaited.
- Shared state across threads without a lock.

## API and typing
- Public function signatures changed: find callers. Type hints match actual returns, including `None`.
- `Optional` values dereferenced without checks.

## Tests
- Tests assert on values and error types, not just "no exception".
- Fixtures with shared mutable state across tests.
- Mocks that replace the code under test, so the test cannot fail.
- Edge cases from the plan (empty, boundary, invalid) have tests.
