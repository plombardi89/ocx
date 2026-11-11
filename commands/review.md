---
description: Independent adversarial review of the current branch (argument - optional plan file)
agent: reviewer
subagent: true
---

Review request
Repository: !`git rev-parse --show-toplevel`
Branch: !`git branch --show-current`
Base: !`git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || git merge-base HEAD master`
Head: !`git rev-parse HEAD`
Plan (blank means none): $ARGUMENTS
Round: 1
Previous review: none
