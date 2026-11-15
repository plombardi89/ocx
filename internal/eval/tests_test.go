package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/gitx"
)

const threeCriteriaPlan = `# 0001: Things

## Goal

Do things.

## Acceptance criteria

### Inputs

1. [ ] Accepts a limit.
   1. Nested detail that isn't a criterion.
2. [ ] Caps the limit at 100.

### Outputs

3. [ ] Is fast.

## Tasks

1. [ ] Not a criterion.
`

// testsRepo returns a workspace whose Head has the plan above and one
// source file.
func testsRepo(t *testing.T) *Workspace {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	work := t.TempDir()
	if err := gitx.Init(work); err != nil {
		t.Fatal(err)
	}
	for p, content := range map[string]string{
		"docs/plans/0001-things.md": threeCriteriaPlan,
		"internal/a/a.go":           "package a\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(work, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	head, err := gitx.CommitAll(work, "Initial commit")
	if err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Work: work, Head: head, Base: head, Branch: "main"}
	ws.Before = captureState(work)
	return ws
}

type report struct {
	status                        string
	criteria, passed, failed, gap int
	notRun                        int
	head                          string
	results                       []string // "1. PASSED", ...
	files                         []TestFile
	sections                      []string
}

func (r report) String() string {
	var b strings.Builder
	b.WriteString("# Tests: docs/plans/0001-things.md\n\n")
	sections := r.sections
	if sections == nil {
		sections = TestsSections
	}
	for _, s := range sections {
		fmt.Fprintf(&b, "## %s\n\n", s)
		switch s {
		case "Criteria":
			for _, line := range r.results {
				b.WriteString(line + " - details\n")
			}
		case "Test files":
			if len(r.files) == 0 {
				b.WriteString("None.\n")
			}
			for _, f := range r.files {
				fmt.Fprintf(&b, "### `%s`\n\n```go\n%s```\n\n", f.Path, f.Content)
			}
		default:
			b.WriteString("None.\n")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "OCTESTS status=%s criteria=%d passed=%d failed=%d gaps=%d not_run=%d head=%s\n",
		r.status, r.criteria, r.passed, r.failed, r.gap, r.notRun, r.head)
	return b.String()
}

const goodTest = "package a\n\nimport \"testing\"\n\n// A limit above the maximum is lowered to it.\nfunc TestLimitIsCapped(t *testing.T) {\n\tif got := \"test-writer\"; got == \"\" {\n\t\tt.Fatal(got)\n\t}\n}\n"

func goodReport(ws *Workspace) report {
	return report{
		status: "COMPLETE", criteria: 3, passed: 1, failed: 1, gap: 1, head: ws.Head,
		results: []string{"1. PASSED", "2. **FAILED**", "3. GAP"},
		files:   []TestFile{{Path: "internal/a/limit_test.go", Content: goodTest}},
	}
}

func failing(checks []Check) []string {
	var names []string
	for _, c := range checks {
		if !c.OK {
			names = append(names, c.Name+": "+c.Detail)
		}
	}
	return names
}

func TestParseTestsReport(t *testing.T) {
	ws := testsRepo(t)
	r, ok := ParseTestsReport(goodReport(ws).String())
	if !ok || r.Status != "COMPLETE" || r.Criteria != 3 || r.Passed != 1 || r.Failed != 1 || r.Gaps != 1 || r.Head != ws.Head {
		t.Fatalf("summary = %+v", r)
	}
	if strings.Join(r.Results[2], ",") != "FAILED" || len(r.Results) != 3 {
		t.Fatalf("results = %v", r.Results)
	}
	if len(r.Files) != 1 || r.Files[0].Path != "internal/a/limit_test.go" || r.Files[0].Content != goodTest {
		t.Fatalf("files = %+v", r.Files)
	}
	if strings.Join(r.Sections, "|") != strings.Join(TestsSections, "|") {
		t.Fatalf("sections = %v", r.Sections)
	}
	if got := TestNames(r.Files[0]); strings.Join(got, ",") != "TestLimitIsCapped" {
		t.Fatalf("TestNames = %v", got)
	}
}

func TestPlanCriteria(t *testing.T) {
	if got := fmt.Sprint(PlanCriteria(threeCriteriaPlan)); got != "[1 2 3]" {
		t.Fatalf("PlanCriteria = %s", got)
	}
	if got := PlanCriteria("# Plan\n\n## Goal\n\n1. Not a criterion.\n"); len(got) != 0 {
		t.Fatalf("no criteria section: %v", got)
	}
}

func TestTestsReportChecks(t *testing.T) {
	ws := testsRepo(t)
	if err := os.Symlink("internal", filepath.Join(ws.Work, "linked")); err != nil {
		t.Fatal(err)
	}
	head, err := gitx.CommitAll(ws.Work, "Link a directory")
	if err != nil {
		t.Fatal(err)
	}
	ws.Head = head
	plan := TestsExpect{Plan: "docs/plans/0001-things.md"}
	n := func(v int) *int { return &v }
	file := func(p string) TestFile { return TestFile{Path: p, Content: goodTest} }

	cases := []struct {
		name   string
		report func(r *report)
		expect TestsExpect
		fails  []string // substrings of the failing check names; none means all pass
	}{
		{name: "a consistent report", report: func(*report) {}},
		{name: "wrong head", report: func(r *report) { r.head = strings.Repeat("a", 40) }, fails: []string{"OCTESTS names head"}},
		{name: "counts that don't add up", report: func(r *report) { r.passed = 2 }, fails: []string{"counts add up", "listed results match"}},
		{name: "criteria count differs from the plan", report: func(r *report) { r.criteria, r.gap = 2, 0; r.results = r.results[:2] },
			fails: []string{"criteria matches the plan", "each criterion listed once"}},
		{name: "a criterion listed twice", report: func(r *report) { r.results = append(r.results, "3. GAP") }, fails: []string{"each criterion listed once"}},
		{name: "a criterion the plan doesn't have", report: func(r *report) { r.results = append(r.results, "4. PASSED") }, fails: []string{"each criterion listed once"}},
		{name: "sections out of order", report: func(r *report) {
			r.sections = []string{"Criteria", "Test files", "Failures", "Plan gaps", "How to run"}
		}, fails: []string{"sections in order"}},
		{name: "passed criteria without test files", report: func(r *report) { r.files = nil }, fails: []string{"test files if and only if"}},
		{name: "files in an all-GAP report", report: func(r *report) {
			r.passed, r.failed, r.gap = 0, 0, 3
			r.results = []string{"1. GAP", "2. GAP", "3. GAP"}
		}, fails: []string{"test files if and only if"}},
		{name: "a valid all-GAP report with no files", report: func(r *report) {
			r.passed, r.failed, r.gap, r.files = 0, 0, 3, nil
			r.results = []string{"1. GAP", "2. GAP", "3. GAP"}
		}, expect: TestsExpect{Status: "COMPLETE", Gaps: n(3)}},
		{name: "INCOMPLETE with criteria still to run", report: func(r *report) {
			r.status, r.passed, r.failed, r.gap, r.notRun, r.files = "INCOMPLETE", 0, 0, 0, 3, nil
			r.results = []string{"1. NOT_RUN", "2. NOT_RUN", "3. NOT_RUN"}
		}, expect: TestsExpect{Status: "INCOMPLETE", NotRun: n(3)}},
		{name: "COMPLETE with criteria still to run", report: func(r *report) {
			r.passed, r.failed, r.gap, r.notRun, r.files = 0, 0, 0, 3, nil
			r.results = []string{"1. NOT_RUN", "2. NOT_RUN", "3. NOT_RUN"}
		}, fails: []string{"COMPLETE has nothing left to run"}},
		{name: "an INCOMPLETE report offered to a scenario that expects COMPLETE", report: func(r *report) {
			r.status, r.passed, r.failed, r.gap, r.notRun, r.files = "INCOMPLETE", 0, 0, 0, 3, nil
			r.results = []string{"1. NOT_RUN", "2. NOT_RUN", "3. NOT_RUN"}
		}, expect: TestsExpect{Status: "COMPLETE", Passed: n(3)}, fails: []string{"status COMPLETE", "passed=3"}},
		{name: "a path listed twice with the same content", report: func(r *report) {
			r.files = []TestFile{file("internal/a/x_test.go"), file("internal/a/x_test.go")}
		}, fails: []string{"listed once"}},
		{name: "a path listed twice with different content", report: func(r *report) {
			r.files = []TestFile{file("internal/a/x_test.go"), {Path: "internal/a/x_test.go", Content: "package a\n"}}
		}, fails: []string{"listed once"}},
		{name: "a path spelled two ways", report: func(r *report) {
			r.files = []TestFile{file("internal/a/x_test.go"), file("internal/./a/x_test.go")}
		}, fails: []string{"listed once"}},
		{name: "a path that exists at head", report: func(r *report) { r.files = []TestFile{file("internal/a/a.go")} }, fails: []string{"listed once"}},
		{name: "a path outside the repository", report: func(r *report) { r.files = []TestFile{file("../x_test.go")} }, fails: []string{"listed once"}},
		{name: "an absolute path", report: func(r *report) { r.files = []TestFile{file("/tmp/x_test.go")} }, fails: []string{"listed once"}},
		{name: "a path through a link at head", report: func(r *report) { r.files = []TestFile{file("linked/x_test.go")} }, fails: []string{"goes through a link"}},
		{name: "a test named for the plan", report: func(r *report) {
			r.files = []TestFile{{Path: "internal/a/x_test.go", Content: "package a\n\nimport \"testing\"\n\nfunc TestCriterion2(t *testing.T) {}\n"}}
		}, expect: TestsExpect{NameForbid: []string{`(?i)criteri`}}, fails: []string{"describe behavior"}},
		{name: "a comment that names the source", report: func(r *report) {
			r.files = []TestFile{{Path: "internal/a/x_test.go", Content: "package a\n\nimport \"testing\"\n\n// Written by the test-writer.\nfunc TestCap(t *testing.T) {}\n"}}
		}, expect: TestsExpect{NameForbid: []string{`(?i)test-writer`}}, fails: []string{"describe behavior"}},
		{name: "an apostrophe in a comment that names the source", report: func(r *report) {
			r.files = []TestFile{{Path: "internal/a/x_test.go", Content: "package a\n\nimport \"testing\"\n\n// This test's cap comes from the plan's value.\nfunc TestCap(t *testing.T) {}\n"}}
		}, expect: TestsExpect{NameForbid: []string{`(?i)\bplan\b`}}, fails: []string{"describe behavior"}},
		{name: "a shell heredoc that mentions the plan", report: func(r *report) {
			r.files = []TestFile{{Path: "tests/heredoc_test.sh", Content: "test_reads_input() {\n\tcat <<EOF\n# plan\nEOF\n}\ntest_reads_input\n"}}
		}, expect: TestsExpect{NameForbid: []string{`(?i)\bplan\b`}}},
		{name: "a test-writer literal the behavior needs", report: func(*report) {},
			expect: TestsExpect{NameForbid: []string{`(?i)test-writer`}}},
		{name: "exact results", report: func(*report) {},
			expect: TestsExpect{Results: map[int]string{2: "FAILED", 3: "PASSED"}}, fails: []string{"criterion 3 PASSED"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := goodReport(ws)
			c.report(&r)
			e := c.expect
			e.Plan = plan.Plan
			got := failing(checkTestsReport(ws, r.String(), e))
			if len(c.fails) == 0 && len(got) > 0 {
				t.Fatalf("unexpected failures: %v", got)
			}
			for _, want := range c.fails {
				found := false
				for _, g := range got {
					if strings.Contains(g, want) {
						found = true
					}
				}
				if !found {
					t.Errorf("no failing check containing %q; failures: %v", want, got)
				}
			}
		})
	}
}

func TestMissingPlanReport(t *testing.T) {
	ws := testsRepo(t)
	r := report{status: "INCOMPLETE", head: ws.Head}
	e := TestsExpect{Plan: "docs/plans/0009-missing.md", Status: "INCOMPLETE"}
	if got := failing(checkTestsReport(ws, r.String(), e)); len(got) > 0 {
		t.Fatalf("missing plan: %v", got)
	}
	r.status = "COMPLETE"
	if got := failing(checkTestsReport(ws, r.String(), e)); len(got) == 0 {
		t.Fatal("a COMPLETE report for a missing plan passed")
	}
	if got := failing(checkTestsReport(ws, "no summary here", e)); len(got) != 1 || !strings.Contains(got[0], "OCTESTS line") {
		t.Fatalf("no OCTESTS line: %v", got)
	}
}

func TestUnchangedComparesWithTheStateBeforeTheRun(t *testing.T) {
	unchanged := func(ws *Workspace) []string {
		a := asserter{ws: ws}
		return failing(a.run(Expect{Unchanged: true}))
	}

	ws := testsRepo(t)
	if got := unchanged(ws); len(got) > 0 {
		t.Fatalf("untouched repository: %v", got)
	}

	// An uncommitted change that was there before the run is part of the
	// state to keep.
	dirty := testsRepo(t)
	plan := filepath.Join(dirty.Work, "docs/plans/0001-things.md")
	if err := os.WriteFile(plan, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty.Before = captureState(dirty.Work)
	if got := unchanged(dirty); len(got) > 0 {
		t.Fatalf("pre-existing change: %v", got)
	}
	if err := os.WriteFile(plan, []byte("edited again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := unchanged(dirty); len(got) == 0 {
		t.Fatal("a changed uncommitted file went unnoticed")
	}

	changed := testsRepo(t)
	if err := os.WriteFile(filepath.Join(changed.Work, "internal/a/a.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := unchanged(changed); len(got) == 0 {
		t.Fatal("a changed source file went unnoticed")
	}

	// A worktree that existed before the run must still be there after it.
	kept := testsRepo(t)
	existing := filepath.Join(t.TempDir(), "existing")
	if _, err := gitx.Run(kept.Work, "worktree", "add", "--detach", existing, "HEAD"); err != nil {
		t.Fatal(err)
	}
	kept.Before = captureState(kept.Work)
	if got := unchanged(kept); len(got) > 0 {
		t.Fatalf("an existing worktree left alone: %v", got)
	}
	if _, err := gitx.Run(kept.Work, "worktree", "remove", "--force", existing); err != nil {
		t.Fatal(err)
	}
	if got := unchanged(kept); len(got) != 1 || !strings.Contains(got[0], "removed ["+existing+"]") {
		t.Fatalf("a removed worktree: %v", got)
	}

	leaked := testsRepo(t)
	if _, err := gitx.Run(leaked.Work, "worktree", "add", "--detach", filepath.Join(t.TempDir(), "wt"), "HEAD"); err != nil {
		t.Fatal(err)
	}
	got := unchanged(leaked)
	if len(got) != 1 || !strings.Contains(got[0], "worktrees as they were") {
		t.Fatalf("leaked worktree: %v", got)
	}
}

func TestEditsMustStayInTheAgentsOwnScratchRoot(t *testing.T) {
	cases := []struct {
		agent, path string
		want        bool
	}{
		{"debugger", "/tmp/opencode/debug/x/a_test.go", true},
		{"debugger", "/tmp/opencode/reviews/x/a_test.go", false},
		{"reviewer", "/tmp/opencode/reviews/x/a_test.go", true},
		{"reviewer", "/tmp/opencode/debug/x/a_test.go", false},
		{"build", "/tmp/opencode/debug/x/a_test.go", true},
		{"build", "internal/a/a.go", false},
	}
	for _, c := range cases {
		if got := inScratch(c.agent, c.path); got != c.want {
			t.Errorf("inScratch(%s, %s) = %v, want %v", c.agent, c.path, got, c.want)
		}
	}
}

func TestUnchangedSeesEditsToFilesGitQuotes(t *testing.T) {
	ws := codeRepo(t, map[string]string{"café.txt": "committed\n"})
	writeFiles(t, ws.Work, map[string]string{"café.txt": "before\n"})
	ws.Before = captureState(ws.Work)
	if ws.Before.Files["café.txt"] == "missing" {
		t.Fatalf("recorded state can't read café.txt: %v", ws.Before.Files)
	}
	writeFiles(t, ws.Work, map[string]string{"café.txt": "after\n"})
	a := &asserter{ws: ws}
	if got := failing(a.run(Expect{Unchanged: true})); len(got) == 0 {
		t.Fatal("an edit to café.txt went unnoticed")
	}
}

func TestProseIsTestNamesAndGoComments(t *testing.T) {
	goSrc := "package a\n\nimport \"testing\"\n\n" +
		"const quoted = `func TestAddsTwoNumbers(t *testing.T) {} // not a comment`\n\n" +
		"// it's a comment\nfunc TestReal(t *testing.T) { _ = quoted /* the plan's */ }\n"
	cases := []struct {
		path, src string
		want      []string
	}{
		{"a/a_test.go", goSrc, []string{"TestReal", "// it's a comment", "/* the plan's */"}},
		{"tests/test_a.py", "# the plan's\nimport unittest\n\nclass T(unittest.TestCase):\n    def test_tags(self):\n        pass\n", []string{"test_tags"}},
		{"tests/a_test.sh", "text=$'it\\'s' # the plan's\ncat <<EOF\n# plan\nEOF\ntest_text() { :; }\ntest_text\n", []string{"test_text"}},
	}
	for _, c := range cases {
		got := testProse(TestFile{Path: c.path, Content: c.src})
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: prose = %q, want %q", c.path, got, c.want)
		}
	}
}
