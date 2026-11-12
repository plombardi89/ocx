package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/gitx"
	"github.com/plombardi89/ocx/internal/stage"
)

func launch(agent, prompt, output string) Part {
	return Part{Tool: "subagent", State: &ToolState{Status: "completed", Input: map[string]any{"agent": agent, "prompt": prompt}, Output: output}}
}

func TestDispatchChecks(t *testing.T) {
	tr := &Transcript{Tools: []Part{
		launch("test-writer", "Test request\nRepository: /w", ""),
		launch("reviewer", "Review request\nRepository: /w\nThe tests all pass.", ""),
		launch("test-writer", "Test request\nRepository: /w", ""),
	}}
	a := &asserter{tr: tr, ws: &Workspace{}}
	one, two := 1, 2
	cases := []struct {
		name string
		e    E2E
		want []string // substrings of failing checks
	}{
		{name: "counts within limits", e: E2E{Dispatches: []Dispatch{{Agent: "test-writer", Prefix: "Test request", Min: 1, Max: &two}}}},
		{name: "too many launches", e: E2E{Dispatches: []Dispatch{{Agent: "test-writer", Max: &one}}}, want: []string{"at most 1"}},
		{name: "a launch that must not happen", e: E2E{Dispatches: []Dispatch{{Agent: "consultant", Max: new(int)}, {Agent: "reviewer", Max: new(int)}}}, want: []string{"reviewer launched at most 0"}},
		{name: "framing", e: E2E{Dispatches: []Dispatch{{Agent: "reviewer", Forbid: []string{`(?i)tests all pass`}}}}, want: []string{"reviewer dispatch 1 has no framing"}},
		{name: "the old single-agent fields", e: E2E{DispatchMin: 2}, want: []string{"reviewer launched at least 2"}},
		{name: "order", e: E2E{DispatchOrder: []string{"test-writer", "reviewer"}}},
		{name: "order reversed", e: E2E{DispatchOrder: []string{"reviewer", "test-writer"}}, want: []string{"first launches in order"}},
		{name: "an agent never launched", e: E2E{DispatchOrder: []string{"test-writer", "debugger"}}, want: []string{"debugger was never launched"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := failing(a.e2e(c.e))
			if len(c.want) == 0 && len(got) > 0 {
				t.Fatalf("unexpected failures: %v", got)
			}
			for _, w := range c.want {
				if !strings.Contains(strings.Join(got, "\n"), w) {
					t.Errorf("no failing check containing %q; failures: %v", w, got)
				}
			}
		})
	}
}

// pushedRepo returns a workspace whose main has the buggy Add and an
// existing test that doesn't catch it, and whose origin has branch
// feat/add with one commit per entry in commits.
func pushedRepo(t *testing.T, commits ...map[string]string) *Workspace {
	return branchRepo(t, true, commits...)
}

// branchRepo is pushedRepo, with feat/add pushed only if push is set.
func branchRepo(t *testing.T, push bool, commits ...map[string]string) *Workspace {
	t.Helper()
	ws := goRepo(t, addWrong)
	writeFiles(t, ws.Work, map[string]string{"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestAddsZeroes(t *testing.T) {\n\tif Add(0, 0) != 0 {\n\t\tt.Fatal()\n\t}\n}\n"})
	base, err := gitx.CommitAll(ws.Work, "Existing test")
	if err != nil {
		t.Fatal(err)
	}
	ws.Base = base
	ws.Origin = filepath.Join(t.TempDir(), "origin.git")
	for _, args := range [][]string{
		{"init", "-q", "--bare", "-b", "main", ws.Origin},
		{"-C", ws.Work, "remote", "add", "origin", ws.Origin},
		{"-C", ws.Work, "push", "-q", "origin", "main"},
		{"-C", ws.Work, "switch", "-q", "-c", "feat/add"},
	} {
		if _, err := gitx.Run(ws.Work, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i, files := range commits {
		writeFiles(t, ws.Work, files)
		if _, err := gitx.CommitAll(ws.Work, "commit "+string(rune('1'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if push {
		if _, err := gitx.Run(ws.Work, "push", "-q", "origin", "feat/add"); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func returnedReport(files ...TestFile) string {
	r := report{status: "COMPLETE", criteria: 1, failed: 1, results: []string{"1. FAILED"}, files: files, head: strings.Repeat("a", 40)}
	return "<subagent>\n" + r.String() + "</subagent>"
}

func TestBuildSideChecks(t *testing.T) {
	dir := t.TempDir()
	buggy := overlay(t, dir, "buggy", map[string]string{"calc/calc.go": addWrong})
	test := goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))
	weakened := goFile(goTest("TestAddingTwoAndThree", "0", "0", "0"))
	fix := map[string]string{"calc/calc.go": addCorrect}
	adoptAndFix := []map[string]string{{test.Path: test.Content}, fix}

	cases := []struct {
		name    string
		commits []map[string]string
		output  string
		e       E2E
		want    []string
	}{
		{name: "adopted exactly, fixed, and still catching the bug", commits: adoptAndFix,
			e: E2E{AdoptedExactly: true, Buggy: []string{buggy}, NormalTest: []string{"go", "test", "./..."}, RegistrationCommit: []string{"calc/calc.go"}}},
		{name: "adopted with a changed expectation", commits: []map[string]string{{test.Path: weakened.Content}, fix},
			e: E2E{AdoptedExactly: true}, want: []string{"adopted the returned test files byte for byte"}},
		{name: "adopted together with the fix", commits: []map[string]string{{test.Path: test.Content, "calc/calc.go": addCorrect}},
			e: E2E{AdoptedExactly: true}, want: []string{"adopted the returned test files byte for byte"}},
		{name: "weakened after adopting", commits: append(adoptAndFix, map[string]string{test.Path: weakened.Content}),
			e: E2E{Buggy: []string{buggy}, NormalTest: []string{"go", "test", "./..."}}, want: []string{"adopted tests still catch the original bug", "catches the original bug"}},
		{name: "no report with files", commits: adoptAndFix, output: "no report",
			e: E2E{AdoptedExactly: true}, want: []string{"the last test-writer report has test files"}},
		{name: "no registration-only commit", commits: []map[string]string{{test.Path: test.Content, "calc/calc.go": addCorrect}},
			e: E2E{RegistrationCommit: []string{"calc/calc.go"}}, want: []string{"a commit that changes only"}},
		{name: "something was pushed", commits: adoptAndFix, e: E2E{NoPush: true}, want: []string{"nothing pushed"}},
		{name: "a pushed test named for the plan", commits: []map[string]string{{"calc/plan_test.go": "package calc\n\nimport \"testing\"\n\n// Covers criterion 2 of the plan.\nfunc TestAC2(t *testing.T) {}\n"}},
			e: E2E{TestProseForbid: []string{`(?i)\bcriteri`, `AC\d`}}, want: []string{"test names and comments describe behavior"}},
		{name: "a pushed file that mentions what it must", commits: adoptAndFix,
			e:    E2E{PushedFileMentions: []FileMention{{Path: "calc/calc.go", Pattern: `a \+ b`}, {Path: "calc/calc.go", Pattern: `a - b`}}},
			want: []string{"mentions a - b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := pushedRepo(t, c.commits...)
			output := c.output
			if output == "" {
				output = returnedReport(test)
			}
			a := &asserter{ws: ws, dir: dir, tr: &Transcript{Tools: []Part{launch("test-writer", "Test request", output)}}}
			got := failing(a.e2e(c.e))
			if len(c.want) == 0 && len(got) > 0 {
				t.Fatalf("unexpected failures: %v", got)
			}
			for _, w := range c.want {
				if !strings.Contains(strings.Join(got, "\n"), w) {
					t.Errorf("no failing check containing %q; failures: %v", w, got)
				}
			}
		})
	}
}

func TestStubbedAgentAndUncommittedChanges(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	sdir := t.TempDir()
	writeFiles(t, sdir, map[string]string{
		"first.md":                "first report for {{HEAD}}\n",
		"second.md":               "second report for {{HEAD}}\n",
		"dirty/docs/plans/x.md":   "edited, not committed\n",
		"branch/docs/plans/x.md":  "committed\n",
		"branch/internal/money/x": "x\n",
	})
	s := Scenario{
		Name: "stub", Fixture: "go-service", Dir: sdir, Agent: "build",
		Branch: "feat/x", Commits: []CommitSpec{{Message: "add plan", Overlay: "branch"}},
		Uncommitted: "dirty",
		StubAgents:  map[string][]string{"debugger": {"first.md", "second.md"}},
	}
	ws, err := Prepare(root, s, filepath.Join(t.TempDir(), "ws"), stage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	stubDir := filepath.Join(config.ScratchRoots["debugger"], "stub-ws")
	t.Cleanup(func() { _ = ws.Remove() })

	if !strings.Contains(ws.Before.Status, "docs/plans/x.md") {
		t.Fatalf("uncommitted change missing from the recorded state: %q", ws.Before.Status)
	}
	if data, _ := os.ReadFile(filepath.Join(ws.Work, "docs/plans/x.md")); string(data) != "edited, not committed\n" {
		t.Fatalf("work tree plan = %q", data)
	}
	if committed, _ := gitx.Run(ws.Work, "show", "HEAD:docs/plans/x.md"); committed != "committed" {
		t.Fatalf("committed plan = %q", committed)
	}

	prompt, err := os.ReadFile(filepath.Join(ws.Work, ".opencode/prompts/debugger.md"))
	script := filepath.Join(stubDir, "next.sh")
	if err != nil || !strings.Contains(string(prompt), "bash "+script) {
		t.Fatalf("stub prompt = %q, %v", prompt, err)
	}
	var got []string
	for range 3 {
		cmd := exec.Command("bash", script)
		cmd.Dir = ws.Work
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.TrimSpace(string(out)))
	}
	want := []string{"first report for " + ws.Head, "second report for " + ws.Head, "second report for " + ws.Head}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("stub reports = %q, want %q", got, want)
	}
	_ = ws.Remove()
	if _, err := os.Stat(stubDir); !os.IsNotExist(err) {
		t.Fatalf("stub directory left behind: %v", err)
	}
}

func TestChecksUseTheLocalBranchWhenNothingWasPushed(t *testing.T) {
	dir := t.TempDir()
	buggy := overlay(t, dir, "buggy", map[string]string{"calc/calc.go": addWrong})
	test := goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))
	e := E2E{AdoptedExactly: true, Buggy: []string{buggy}, NormalTest: []string{"go", "test", "./..."},
		RegistrationCommit: []string{"calc/calc.go"}, ApprovedIfPushed: true, NoPush: true}

	ws := branchRepo(t, false, map[string]string{test.Path: test.Content}, map[string]string{"calc/calc.go": addCorrect})
	a := &asserter{ws: ws, dir: dir, tr: &Transcript{Tools: []Part{launch("test-writer", "Test request", returnedReport(test))}}}
	if got := failing(a.e2e(e)); len(got) > 0 {
		t.Fatalf("local branch: %v", got)
	}

	// The same branch, pushed without an approval.
	pushed := branchRepo(t, true, map[string]string{test.Path: test.Content}, map[string]string{"calc/calc.go": addCorrect})
	a = &asserter{ws: pushed, dir: dir, tr: &Transcript{Tools: []Part{launch("test-writer", "Test request", returnedReport(test))}}}
	got := strings.Join(failing(a.e2e(E2E{ApprovedIfPushed: true})), "\n")
	if !strings.Contains(got, "anything pushed is approved") {
		t.Fatalf("unapproved push: %v", got)
	}
}

func TestAdoptionMustMatchEveryByte(t *testing.T) {
	f := goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))
	for name, committed := range map[string]string{
		"without the final newline":  strings.TrimSuffix(f.Content, "\n"),
		"with a leading blank line":  "\n" + f.Content,
		"with a trailing blank line": f.Content + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			ws := branchRepo(t, false, map[string]string{f.Path: committed})
			a := &asserter{ws: ws, tr: &Transcript{Tools: []Part{launch("test-writer", "Test request", returnedReport(f))}}}
			if got := failing(a.buildSide(E2E{AdoptedExactly: true}, nil)); len(got) == 0 {
				t.Fatal("adoption accepted a file that differs from the returned one")
			}
		})
	}
}

func TestTheFinalCodeMustPassTheAdoptedTests(t *testing.T) {
	dir := t.TempDir()
	buggy := overlay(t, dir, "buggy", map[string]string{"calc/calc.go": addWrong})
	test := goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))
	// The tests are adopted, but the code is never fixed.
	ws := branchRepo(t, false, map[string]string{test.Path: test.Content})
	a := &asserter{ws: ws, dir: dir, tr: &Transcript{Tools: []Part{launch("test-writer", "Test request", returnedReport(test))}}}
	got := strings.Join(failing(a.buildSide(E2E{Buggy: []string{buggy}, NormalTest: []string{"go", "test", "./..."}}, nil)), "\n")
	for _, want := range []string{"adopted tests pass on the final code", "passes on the final code"} {
		if !strings.Contains(got, want) {
			t.Errorf("no failing check %q; failures:\n%s", want, got)
		}
	}
}
