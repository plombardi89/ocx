---
description: Debug a problem - reproduce it, find the root cause, fix it with a regression test, and get the fix reviewed
agent: build
---

Debug this by following the `systematic-debugging` skill: reproduce it with a failing check before changing any code, find and explain the root cause, fix it at the root with a regression test, and get the fix reviewed before pushing. If three attempts fail, get a second opinion from the `consultant` subagent before trying more.

The problem: $ARGUMENTS
