---
name: plan-critique
description: Use when you are the independent critic of a plan someone else wrote - a docs/plans file that needs a verdict before implementation starts.
---

# Plan critique

You produce a verdict on one plan file before any code is written. The code reviewer will later check the code against this plan's acceptance criteria, so judge the plan the way that reviewer will depend on it. Follow the steps in order.

## 1. Read

1. Read the plan file in full.
2. Read the project's `AGENTS.md` for its conventions and commands.
3. Read the code the plan will change: every file, type, function, endpoint, and config key it names or implies. Find them with `grep` and `glob`, and read their callers. Note every place where the plan's assumptions about the code are wrong.
4. Read any decision records the plan links.
5. Load the `writing-style` skill for step 5.

## 2. Check the goal and scope

1. The goal says who benefits and what changes for them.
2. The non-goals rule out the obvious ways the work could grow.
3. Nothing in the criteria or tasks goes beyond the goal (scope creep), and nothing the goal promises is missing from them (a gap).

## 3. Check every acceptance criterion

For each criterion, in order:

1. **Checkable?** A reviewer must be able to prove it MET or NOT MET by running something or reading code. "Fast", "robust", "handles errors gracefully", "user-friendly", and "scales well" are not checkable. A checkable criterion names observable behavior with concrete values: inputs, outputs, status codes, limits, error types.
2. **Complete?** Attack the behavior it describes the way the code reviewer will:
   1. Empty, zero, nil, or missing values.
   2. Negative numbers, boundaries (0, 1, max, max+1), overflow.
   3. Invalid or malformed input.
   4. Concurrent callers and partial failure.
   5. Errors and timeouts from dependencies.
   6. Existing data, and compatibility with current callers and APIs.

   When the goal clearly covers a situation and the plan leaves its behavior undefined, that's a finding. Write the missing criterion.
3. **Consistent?** With the other criteria, the goal, and the existing code. For example, a plan that promises plain-text errors in an API whose other endpoints return JSON errors.

## 4. Check the approach and tasks

1. The approach works with the code as it actually is. A wrong assumption about the code is a BLOCKER.
2. Every task is small and has a "Done when" line that names a test or command.
3. The tasks cover every acceptance criterion, and each criterion has a planned test.
4. Risky steps (data migrations, API changes, deletions) say how compatibility is kept or how to roll back.

## 5. Check the writing

Apply the `writing-style` skill to the plan. Report each clear break as a WRITING finding with the quoted text, the rule, and a rewrite.

## 6. Collect questions for Phil

1. Every entry in the plan's "Open questions" section goes under "Questions for Phil". A plan with open questions can't be approved.
2. When a criterion needs a product decision the goal doesn't settle (for example, whether `limit=0` returns an empty page or a 400), add it as a question with a suggested default, and report the gap as a MAJOR finding.

## 7. Classify

| Severity | Meaning |
| --- | --- |
| BLOCKER | The plan can't be implemented or reviewed as written: a criterion that can't be checked, a contradiction, a wrong assumption about the code, or core behavior from the goal with no criterion. |
| MAJOR | A gap likely to ship a bug or start a review dispute: undefined behavior for an edge case the goal covers, missing error behavior, a task without a check, a criterion without a planned test, an unaddressed compatibility or security risk, or a criterion that depends on an unanswered question. |
| WRITING | A clear break of the writing rules. |
| MINOR | A clarity or structure improvement that doesn't change what gets built. |
| NIT | Preference. Never blocks. |

Every BLOCKER and MAJOR quotes the plan text (or names what's missing), describes a concrete situation the plan leaves undefined or gets wrong (an input, a sequence of calls, or a code reference), and suggests a change, preferably as criterion text.

## 8. Decide

APPROVE only if there are no BLOCKER, MAJOR, or WRITING findings and no questions for Phil. Otherwise REVISE.

## 9. Write the report

Your final message is exactly this structure:

````markdown
# Plan critique: <plan path>

**Verdict:** APPROVE | REVISE

## Findings
1. **[BLOCKER]** <section, or criterion number> - <one-line title>
   1. Plan text: `<quote>`, or "missing"
   2. Problem: <the concrete situation the plan leaves undefined or gets wrong>
   3. Suggested change: <replacement or new criterion text>

## Questions for Phil
1. <question, most important first> Suggested default: <answer>

## Checked
- <what you verified against the code>

OCPLAN verdict=<APPROVE|REVISE> blockers=<n> majors=<n> writing=<n> minors=<n> nits=<n>
````

Write "None" under any empty section. Order findings by severity: BLOCKER, MAJOR, WRITING, MINOR, NIT. The last line must be the OCPLAN line exactly as shown.

## Red flags

| Thought | Reality |
| --- | --- |
| "The plan is short, so it must be simple." | Short plans leave the most undefined. Check every criterion. |
| "The Log says round 1 findings were fixed." | That's a claim. Read the plan text. |
| "I'd build this differently." | Only report what breaks the goal, the code, or the reviewer's ability to check. Preferences are NITs. |
| "The plan should also do X." | If the goal doesn't need X, it's scope creep, not a gap. Ask Phil if you're unsure. |
| "I'll decide what the edge case should do." | Product decisions are Phil's. Ask, with a suggested default. |
| "The criterion is vague, but everyone knows what it means." | The code reviewer can't check "everyone knows". It's a BLOCKER. |
