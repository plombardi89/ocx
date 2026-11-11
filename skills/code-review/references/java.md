# Java review checklist

Check each item that applies to the change.

## Correctness
- `==` used to compare strings, boxed numbers, or other objects instead of `equals`.
- `equals` overridden without `hashCode` (or the reverse); mutable fields used in `hashCode` of keys in hash collections.
- Integer overflow in arithmetic on `int` (lengths, money, time in ms); money in `double` instead of `BigDecimal` or long cents.
- `BigDecimal` compared with `equals` where scale differs (use `compareTo`).
- Off-by-one in `subList`, `substring`, loops; `Optional.get()` without a presence check.
- Iterating a collection while modifying it (`ConcurrentModificationException`).

## Errors and resources
- Exceptions swallowed (`catch (Exception e) {}`) or logged and ignored where callers need to know.
- Checked exceptions wrapped without the cause.
- Streams, readers, connections, and statements not closed with try-with-resources.
- `InterruptedException` caught without restoring the interrupt flag.

## Concurrency
- Shared mutable state accessed without synchronization; `HashMap` used from multiple threads.
- Check-then-act races (`if (!map.containsKey(k)) map.put(...)`) instead of `computeIfAbsent`/atomic operations.
- Double-checked locking without `volatile`.
- Executors that are never shut down; unbounded thread or queue creation.

## Security
- SQL built by string concatenation instead of prepared statements.
- Deserialization of untrusted data; XML parsers without XXE protection.
- Paths built from user input without normalization and containment checks.
- Secrets in logs or exception messages.

## API
- Public signatures changed: find callers and check semantic versioning expectations.
- `null` returned where callers expect empty collections or `Optional`.

## Tests
- Assertions check values, not just "no exception".
- Tests that depend on execution order, wall-clock time, or shared static state.
- Edge cases from the plan have tests.
