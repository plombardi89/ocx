package eval

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/plombardi89/ocx/internal/gitx"
)

// The test-writer's report ends with an OCTESTS line, lists one result per
// acceptance criterion, and carries every new test file in full.
var (
	testsLine       = regexp.MustCompile(`OCTESTS status=(\S+) criteria=(\d+) passed=(\d+) failed=(\d+) gaps=(\d+) not_run=(\d+) head=([0-9a-f]+)`)
	criterionResult = regexp.MustCompile(`^(\d+)\.\s+\**\s*(PASSED|FAILED|GAP|NOT_RUN)\b`)
	numberedItem    = regexp.MustCompile(`^(\d+)\.\s`)
	fence           = regexp.MustCompile("^(`{3,}|~{3,})")
)

// TestsSections are the report's sections, in order.
var TestsSections = []string{"Criteria", "Failures", "Plan gaps", "Test files", "How to run"}

// TestsReport is a parsed test-writer report.
type TestsReport struct {
	Line                                   string
	Status, Head                           string
	Criteria, Passed, Failed, Gaps, NotRun int
	// Results maps each criterion number to the results listed for it under
	// Criteria, in order; a correct report lists exactly one per criterion.
	Results  map[int][]string
	Sections []string
	Files    []TestFile
	// Problems are parts of the report that couldn't be read, such as a
	// file heading without a code block.
	Problems []string
}

// TestFile is one returned test file.
type TestFile struct {
	Path, Content string
}

// ParseTestsReport reads a test-writer report. ok is false when it has no
// OCTESTS line.
func ParseTestsReport(text string) (r TestsReport, ok bool) {
	r.Results = map[int][]string{}
	all := testsLine.FindAllStringSubmatch(text, -1)
	if len(all) > 0 {
		m := all[len(all)-1]
		n := func(s string) int {
			v, _ := strconv.Atoi(s)
			return v
		}
		r.Line, r.Status, r.Head = m[0], m[1], m[7]
		r.Criteria, r.Passed, r.Failed, r.Gaps, r.NotRun = n(m[2]), n(m[3]), n(m[4]), n(m[5]), n(m[6])
		ok = true
	}

	section := ""
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if h, found := strings.CutPrefix(line, "## "); found {
			section = strings.TrimSpace(h)
			r.Sections = append(r.Sections, section)
			continue
		}
		switch {
		case strings.EqualFold(section, "Criteria"):
			if m := criterionResult.FindStringSubmatch(line); m != nil {
				num, _ := strconv.Atoi(m[1])
				r.Results[num] = append(r.Results[num], m[2])
			}
		case strings.EqualFold(section, "Test files"):
			h, found := strings.CutPrefix(line, "### ")
			if !found {
				continue
			}
			p := strings.Trim(strings.TrimSpace(h), "`")
			content, next, err := fencedBlock(lines, i+1)
			if err != nil {
				r.Problems = append(r.Problems, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			r.Files = append(r.Files, TestFile{Path: p, Content: content})
			i = next - 1
		}
	}
	return r, ok
}

// fencedBlock returns the content of the first fenced code block at or after
// lines[start], stopping at the next heading, and the index of the line after
// its closing fence.
func fencedBlock(lines []string, start int) (string, int, error) {
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "#") {
			break
		}
		m := fence.FindString(lines[i])
		if m == "" {
			continue
		}
		var body []string
		for j := i + 1; j < len(lines); j++ {
			if t := strings.TrimSpace(lines[j]); len(t) >= len(m) && strings.Trim(t, m[:1]) == "" {
				return strings.Join(body, "\n") + "\n", j + 1, nil
			}
			body = append(body, lines[j])
		}
		return "", len(lines), fmt.Errorf("code block never closed")
	}
	return "", start, fmt.Errorf("no code block")
}

// PlanCriteria returns the numbers of a plan's acceptance criteria: the
// unindented numbered items under "## Acceptance criteria", which may be
// grouped under ### headings.
func PlanCriteria(plan string) []int {
	var nums []int
	in := false
	for line := range strings.SplitSeq(plan, "\n") {
		if h, found := strings.CutPrefix(line, "## "); found {
			in = strings.EqualFold(strings.TrimSpace(h), "Acceptance criteria")
			continue
		}
		if !in {
			continue
		}
		if m := numberedItem.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			nums = append(nums, n)
		}
	}
	return nums
}

// cleanRepoPath normalizes a repository-relative path, reporting whether it
// stays inside the repository.
func cleanRepoPath(p string) (string, bool) {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "~") {
		return "", false
	}
	c := path.Clean(filepath.ToSlash(p))
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", false
	}
	return c, true
}

var (
	goTestName = regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	pyTestName = regexp.MustCompile(`(?m)^\s*def (test_\w+)\(`)
	shTestName = regexp.MustCompile(`(?m)^\s*(?:function\s+)?(test_\w+)\s*\(\)`)
)

// TestNames returns the test function names in a returned test file. Go
// files are parsed, so a test declaration inside a string or a comment
// isn't a test; a Go file that doesn't parse falls back to matching lines,
// so running it still shows the build failure.
func TestNames(f TestFile) []string {
	var re *regexp.Regexp
	switch {
	case strings.HasSuffix(f.Path, ".go"):
		if file, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, 0); err == nil {
			var names []string
			for _, d := range file.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && fn.Name.Name != "TestMain" {
					names = append(names, fn.Name.Name)
				}
			}
			return names
		}
		re = goTestName
	case strings.HasSuffix(f.Path, ".py"):
		re = pyTestName
	case strings.HasSuffix(f.Path, ".sh"), strings.HasSuffix(f.Path, ".bash"):
		re = shTestName
	default:
		return nil
	}
	var names []string
	for _, m := range re.FindAllStringSubmatch(f.Content, -1) {
		if m[1] != "TestMain" {
			names = append(names, m[1])
		}
	}
	return names
}

// testProse returns the text in a test file that must describe behavior
// rather than where the test came from: its test names, and in Go, whose
// parser finds them exactly, its comments. Comments in other languages are
// left to the reviewer, which checks every change against the writing rules.
func testProse(f TestFile) []string {
	prose := TestNames(f)
	if !strings.HasSuffix(f.Path, ".go") {
		return prose
	}
	file, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, parser.ParseComments)
	if err != nil {
		return prose
	}
	for _, g := range file.Comments {
		for _, c := range g.List {
			prose = append(prose, c.Text)
		}
	}
	return prose
}

// checkTestsReport checks a test-writer report against the plan and the
// files at the workspace's Head, and against the scenario's expectations.
func checkTestsReport(ws *Workspace, final string, e TestsExpect) []Check {
	var checks []Check
	add := func(name string, ok bool, detail string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}

	r, ok := ParseTestsReport(final)
	if !ok {
		add("OCTESTS line", false, "no OCTESTS line in the final message")
		return checks
	}
	add("OCTESTS names head", r.Head == ws.Head, "head=%s, want %s", r.Head, ws.Head)

	// The report's own consistency.
	var want []int
	if e.Plan != "" {
		if plan, err := gitShow(ws.Work, ws.Head, e.Plan); err == nil {
			want = PlanCriteria(plan)
		}
	}
	add("criteria matches the plan at head", r.Criteria == len(want), "criteria=%d, the plan at head has %d", r.Criteria, len(want))
	sum := r.Passed + r.Failed + r.Gaps + r.NotRun
	add("counts add up", sum == r.Criteria, "passed+failed+gaps+not_run=%d, criteria=%d", sum, r.Criteria)
	switch r.Status {
	case "COMPLETE":
		add("COMPLETE has nothing left to run", r.NotRun == 0 && r.Criteria > 0, "not_run=%d criteria=%d", r.NotRun, r.Criteria)
	case "INCOMPLETE":
	default:
		add("status is COMPLETE or INCOMPLETE", false, "status=%s", r.Status)
	}

	listed := map[string]int{}
	var problems []string
	for _, n := range want {
		res := r.Results[n]
		if len(res) != 1 {
			problems = append(problems, fmt.Sprintf("criterion %d listed %d times", n, len(res)))
			continue
		}
		listed[res[0]]++
	}
	for n := range r.Results {
		if !containsInt(want, n) {
			problems = append(problems, fmt.Sprintf("criterion %d isn't in the plan", n))
		}
	}
	add("each criterion listed once", len(problems) == 0, "%s", strings.Join(problems, "; "))
	counted := listed["PASSED"] == r.Passed && listed["FAILED"] == r.Failed && listed["GAP"] == r.Gaps && listed["NOT_RUN"] == r.NotRun
	add("listed results match the counts", counted, "listed %v; OCTESTS passed=%d failed=%d gaps=%d not_run=%d", listed, r.Passed, r.Failed, r.Gaps, r.NotRun)

	order := 0
	for _, s := range r.Sections {
		if order < len(TestsSections) && strings.EqualFold(s, TestsSections[order]) {
			order++
		}
	}
	add("sections in order", order == len(TestsSections), "sections %v, want %v in order", r.Sections, TestsSections)

	// Test files.
	wantFiles := r.Passed+r.Failed > 0
	add("test files if and only if a criterion passed or failed", (len(r.Files) > 0) == wantFiles,
		"%d files; passed=%d failed=%d", len(r.Files), r.Passed, r.Failed)
	add("test files readable", len(r.Problems) == 0, "%s", strings.Join(r.Problems, "; "))
	seen := map[string]bool{}
	var bad []string
	for _, f := range r.Files {
		p, inside := cleanRepoPath(f.Path)
		switch {
		case !inside:
			bad = append(bad, f.Path+" is outside the repository")
		case seen[p]:
			bad = append(bad, f.Path+" is listed more than once")
		case existsAt(ws.Work, ws.Head, p):
			bad = append(bad, f.Path+" already exists at head")
		case linkedAt(ws.Work, ws.Head, p):
			bad = append(bad, f.Path+" goes through a link at head")
		case strings.TrimSpace(f.Content) == "":
			bad = append(bad, f.Path+" is empty")
		}
		seen[p] = true
	}
	add("test files are new, inside the repository, and listed once", len(bad) == 0, "%s", strings.Join(bad, "; "))

	if len(e.NameForbid) > 0 {
		var hits []string
		for _, f := range r.Files {
			for _, text := range testProse(f) {
				for _, p := range e.NameForbid {
					if regexp.MustCompile(p).MatchString(text) {
						hits = append(hits, fmt.Sprintf("%s: %q", f.Path, strings.TrimSpace(text)))
					}
				}
			}
		}
		add("test names and comments describe behavior", len(hits) == 0, "%s", strings.Join(hits, "; "))
	}

	// The scenario's expected outcome.
	if e.Status != "" {
		add("status "+e.Status, r.Status == e.Status, "status=%s", r.Status)
	}
	exact := func(name string, want *int, got int) {
		if want != nil {
			add(fmt.Sprintf("%s=%d", name, *want), got == *want, "%s=%d", name, got)
		}
	}
	exact("criteria", e.Criteria, r.Criteria)
	exact("passed", e.Passed, r.Passed)
	exact("failed", e.Failed, r.Failed)
	exact("gaps", e.Gaps, r.Gaps)
	exact("not_run", e.NotRun, r.NotRun)
	if e.MinFailed > 0 {
		add(fmt.Sprintf("failed at least %d", e.MinFailed), r.Failed >= e.MinFailed, "failed=%d", r.Failed)
	}
	for n, res := range e.Results {
		got := strings.Join(r.Results[n], ",")
		add(fmt.Sprintf("criterion %d %s", n, res), got == res, "listed as %q", got)
	}
	return checks
}

func containsInt(list []int, n int) bool {
	return slices.Contains(list, n)
}

func gitShow(dir, rev, p string) (string, error) {
	return gitx.Run(dir, "show", rev+":"+p)
}

func existsAt(dir, rev, p string) bool {
	_, err := gitx.Run(dir, "cat-file", "-e", rev+":"+p)
	return err == nil
}

// linkedAt reports whether any directory above p is a symbolic link at rev,
// so writing p in a checkout of rev could land outside it.
func linkedAt(dir, rev, p string) bool {
	parts := strings.Split(p, "/")
	for i := 1; i < len(parts); i++ {
		out, err := gitx.Run(dir, "ls-tree", rev, "--", strings.Join(parts[:i], "/"))
		if err == nil && strings.HasPrefix(out, "120000 ") {
			return true
		}
	}
	return false
}
