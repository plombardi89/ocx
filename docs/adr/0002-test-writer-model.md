# 0002: The test-writer runs on Claude Sonnet 5.5

Status: accepted
Date: 2026-11-12
Replaces: none

## Context

The test-writer's tests are only independent if it doesn't share build's assumptions, and build runs on Claude Opus. Its model is pinned, like every agent's.

## Decision

The test-writer runs on Claude Sonnet 5.5 at xhigh effort. In a comparison of 45 runs over 5 scenarios, it passed 15 of 15 and was the fastest. GPT-6.1 Sol passed 13 of 15, and GPT-6 Astra 12 of 15.

## Alternatives considered

- GPT-6.1 Sol: a different family from build, but it failed more runs, was slower, and once missed a mutant that sorted items in the wrong order.
- GPT-6 Astra: also a different family, but it failed the most runs.

## Consequences

Sonnet is the same family as build's Opus. The comparison only prefers a different family to break a tie, and there was none. A new comparison with `make bakeoff TAG=tests` can replace this decision.
