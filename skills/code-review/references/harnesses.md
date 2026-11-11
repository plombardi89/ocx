# Review harnesses

Mechanics for step 6 ("Prove it") and step 7 ("Later rounds") of the code-review skill.

## Rules

- Write files only inside your scratch worktrees under `/tmp/opencode/reviews/`. Never in the author's worktree.
- Give file tools (write, edit, patch) absolute paths under `/tmp/opencode/reviews/`. Relative paths resolve against the author's repository and are denied.
- Use literal paths in commands, not shell variables. Permission checks match the command text, so `git worktree add --detach "$dir" ...` is denied where the literal path is allowed.
- Run harness commands with the shell tool's working directory set to the scratch worktree.
- Prefix every file and test you create with `review_` (Java: `Review...Test`).
- Remove every scratch worktree before you finish.

## Scratch worktrees

Name each one `<repo-dir-name>-<head 7>-<4 random lowercase letters>`, so parallel reviews never collide:

```sh
git worktree add --detach /tmp/opencode/reviews/shop-306ddb2-kqvx 306ddb241b8e58eb3a28dc05a322d522fe8adc75
```

For later rounds, add a second one at the previously reviewed head with a `-prev` suffix:

```sh
git worktree add --detach /tmp/opencode/reviews/shop-306ddb2-kqvx-prev <previous head sha>
```

To copy a file between worktrees, read it and write it with your file tools. Check what you changed in a scratch worktree with `git status --porcelain` and `git diff` run there.

Clean up:

```sh
git worktree remove --force /tmp/opencode/reviews/shop-306ddb2-kqvx
git worktree remove --force /tmp/opencode/reviews/shop-306ddb2-kqvx-prev
git worktree list
```

## Go

- Put the harness in the package under test, in the same package so you can reach unexported code: `internal/store/review_list_test.go`, `func TestReviewListPastEnd(t *testing.T)`.
- Run one test: `go test -run 'TestReview' -count=1 ./internal/store/`
- Fuzz: `func FuzzReviewParse(f *testing.F)` with seeds from the boundaries, then `go test -run '^$' -fuzz FuzzReviewParse -fuzztime 30s ./internal/money/`. A failing input is saved under `testdata/fuzz/`; quote it in the report.
- Exhaustive: when the input space is small (all 2-digit fractions, all byte values, every length up to N), loop over all of it instead of fuzzing.
- Old vs new: copy the old function from `git show <Base>:internal/money/money.go` into the test file under a new name (`reviewOldParseDollars`), then compare old and new over the same inputs and report the first differences.
- Stress: start many goroutines behind a shared start signal (a closed channel), hammer the operation, then check the invariant (stock never negative, total reserved never above initial stock). Repeat a few hundred rounds. This finds atomicity bugs that `-race` can't, because every access is locked.
- Program: a `main` package in `review_<topic>/main.go`, run with `go run ./review_<topic>`.
- Mutations: edit the production file, run the author's tests (`go test -count=1 ./internal/store/`), then restore the original text and confirm with `git diff`.

## Python

- File: `review_<topic>_test.py` next to the module under test (or in `tests/`), functions named `test_review_<case>`.
- Run: `python -m pytest -q review_<topic>_test.py` from the scratch worktree. Use `hypothesis` for property tests only if the project already depends on it.
- The scratch worktree has no virtualenv. If imports fail because dependencies are missing, try the project's own runner (`uv run pytest ...`); if that's not possible, say so under "Could not verify".
- Old vs new: `git show <Base>:pkg/module.py`, saved as `review_old_module.py` in the scratch worktree, imported next to the new module.

## Shell

- File: `review_<topic>.sh`, starting with `set -eu`. Create its temporary files inside the scratch worktree, not in the system temp directory.
- Run: `bash /tmp/opencode/reviews/<name>/review_<topic>.sh`
- Exercise the script under test with crafted arguments, environment variables, file names with spaces, empty values, and missing files. Check exit codes and outputs.

## Java

- File: `src/test/java/<package path>/Review<Topic>Test.java`, JUnit, in the package under test.
- Run one class: `mvn -q test -Dtest=Review<Topic>Test` or `./gradlew test --tests '*Review<Topic>Test'`.
- A fresh worktree means a fresh build; that is slower but expected.

## Reporting

For every harness, the report's "Harnesses" section gets the file path, the exact command, the result with the key output (the panic message, the first differing inputs, the invariant violation), and the code. Keep harness code short enough to paste into a report. A reproduction should be a few dozen lines, not a framework.
