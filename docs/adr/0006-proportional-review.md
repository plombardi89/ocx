# 0006: After round 1, reviews only block on bugs

Status: accepted
Date: 2026-11-15
Replaces: none

## Context

The reviewer is tuned to find gaps, and each fix gives it new surface for the next one. A four-line policy change took six review rounds, each finding another edge case, and the test-writer work took six as well. The cap of three rounds kept being stretched instead of stopping the loop.

## Decision

1. Round 1 works as before: BLOCKER, MAJOR, and WRITING findings block approval.
2. From round 2, only BLOCKER findings, and earlier findings that aren't resolved, block. An earlier BLOCKER or MAJOR is resolved only when it's fixed and has a regression test that fails on the previously reviewed commit.
3. New MAJOR and WRITING findings from round 2 on are still reported, but don't block. Build lists them for Phil under "Decisions for Phil" instead of fixing them on the branch.
4. Round 3 stays a hard stop: if it doesn't approve, build brings the findings to Phil.

## Alternatives considered

- Only BLOCKERs block, in every round: round 1 is where MAJOR and WRITING findings are cheapest to fix.
- Keep the severities and only enforce the cap: the cap was already there, and the rounds kept finding new issues until it was stretched.

## Consequences

Reviews converge in two rounds in the usual case. Some MAJOR issues found late reach Phil as decisions instead of being fixed before the push.
