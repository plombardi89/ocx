# 0003: Evals credit returned tests only with the difference they make

Status: accepted
Date: 2026-11-12
Replaces: none

## Context

Test-writer scenarios check that the returned tests catch planted bugs and mutants. A returned test file could reach the repository's existing tests by calling, importing, or inheriting them, and get credit for what those tests already catch. Each language needed its own fix: test names inside Go strings, inherited Python test classes, uncalled shell functions. A test file can always reach existing tests some other way, so those fixes could never end.

## Decision

The eval runs each scenario's own test command in a fresh checkout, with and without the returned files, the same way for every language, and credits the returned tests only with the difference:

1. Without them, the command passes on Head. With them, it fails exactly when the report has FAILED criteria.
2. With the scenario's correct implementation, it passes with them.
3. Each mutant must pass without them, so the existing tests miss it, and fail with them. A mutant the existing tests already catch is a flaw in the scenario.

## Alternatives considered

- Choosing which tests run, per language: the approach that kept needing new fixes.

## Consequences

Every test-writer scenario needs a `command:`. Mutants must be ones the existing tests miss, so adding a mutant means checking that first.
