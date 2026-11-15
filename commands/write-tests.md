---
description: Get independent tests for a plan, adopt them, fix the code until they pass, and get it reviewed (argument - the plan file)
agent: build
---

Get independent tests for the plan $ARGUMENTS by following the `requesting-tests` skill. First check that every task in the plan is implemented and committed on the current branch, and that the project's tests pass. When every adopted test passes and nothing is waiting for Phil, follow the `requesting-review` skill, and push the branch once it's approved.
