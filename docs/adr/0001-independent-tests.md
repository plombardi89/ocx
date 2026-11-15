# 0001: Independent tests from the plan's acceptance criteria

Status: accepted
Date: 2026-11-11
Replaces: none

## Context

Build writes the code and its tests, so the tests share its blind spots. The reviewer catches some of what they miss, but only after the branch is done, and its harnesses are evidence, not lasting coverage of what Phil asked for.

## Decision

1. After implementing planned work and before requesting review, build launches the `test-writer` subagent. It's required for every plan with acceptance criteria and optional otherwise.
2. The test-writer starts with no context and runs on a different model from build. It takes expected results only from the plan, writes tests in its own scratch root, `/tmp/opencode/tests/`, and runs them against build's committed work.
3. Each criterion gets one result: PASSED, FAILED, GAP when the plan doesn't give a value to check, or NOT_RUN, which is allowed only in an INCOMPLETE report.
4. Build adopts the returned files unchanged. When they fail, build fixes the code or brings the disagreement to Phil. It never changes an expected result on its own.
5. After an INCOMPLETE report, build may fix the cause and request one new run for each cause without asking. Correcting one of the test-writer's tests needs Phil's approval.
6. The report gives expected and actual results. Naming the cause of a failure is the reviewer's and the debugger's job, not the test-writer's.

## Alternatives considered

- Tests written first, before the code: not how Phil works, and the test-writer would have no code to run against.
- Build writes the tests itself, and the reviewer checks them: they keep build's blind spots.
- Coverage targets: they measure lines run, not whether the code does what Phil asked.

## Consequences

Planned work takes one more step before review, and every plan's criteria have to be checkable with concrete values, or they come back as GAPs. Regression tests for review findings still come from the reviewer's harnesses.
