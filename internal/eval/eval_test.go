package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelMatches(t *testing.T) {
	got := ModelRef{ProviderID: "github-copilot", ID: "gpt-6.1-sol", Variant: "xhigh"}
	cases := map[string]bool{
		"github-copilot/gpt-6.1-sol#xhigh": true,
		"github-copilot/gpt-6.1-sol":       true, // no variant pinned: any variant
		"github-copilot/gpt-6.1-sol#high":  false,
		"github-copilot/gpt-6-astra#xhigh": false,
		"openai/gpt-6.1-sol#xhigh":         false,
		"not a model":                      false,
	}
	for want, ok := range cases {
		if modelMatches(got, want) != ok {
			t.Errorf("modelMatches(%s, %q) = %v, want %v", got, want, !ok, ok)
		}
	}
}

func TestChildSessionID(t *testing.T) {
	meta := json.RawMessage(`{"metadata":{"sessionID":"ses_child","status":"completed"},"content":[{"type":"text","text":"<subagent sessionID=\"ses_child\">"}]}`)
	p := Part{SessionID: "ses_parent", State: &ToolState{Metadata: meta}}
	if got := ChildSessionID(p); got != "ses_child" {
		t.Fatalf("ChildSessionID = %q", got)
	}
	if got := ChildSessionID(Part{SessionID: "ses_parent"}); got != "" {
		t.Fatalf("no metadata: got %q", got)
	}
}

func TestFinalMessageUsesLastMessage(t *testing.T) {
	tr := &Transcript{Texts: []Part{
		{MessageID: "m1", Text: "thinking out loud about panics"},
		{MessageID: "m2", Text: "# Review\n"},
		{MessageID: "m2", Text: "OCREVIEW verdict=APPROVE"},
	}}
	if got := tr.FinalMessage(); got != "# Review\nOCREVIEW verdict=APPROVE" {
		t.Fatalf("FinalMessage = %q", got)
	}
}

func TestSummaryAndNoteLines(t *testing.T) {
	head := "306ddb241b8e58eb3a28dc05a322d522fe8adc75"
	report := "...\nOCREVIEW verdict=REQUEST_CHANGES spec=FAIL quality=FAIL blockers=3 majors=1 writing=2 minors=0 nits=4 head=" + head
	s, ok := parseSummary(report)
	if !ok || s.Verdict != "REQUEST_CHANGES" || s.Blockers != 3 || s.Writing != 2 || s.Nits != 4 || s.Head != head {
		t.Fatalf("summary parse = %+v", s)
	}
	if s.Counts() != "3/1/2/0/4" {
		t.Fatalf("Counts = %s", s.Counts())
	}
	old, ok := parseSummary("OCREVIEW verdict=APPROVE spec=PASS quality=PASS blockers=0 majors=0 minors=1 nits=0 head=" + head)
	if !ok || old.Writing != 0 || old.Minors != 1 || old.Head != head {
		t.Fatalf("summary without writing parse = %+v", old)
	}
	n := noteLine.FindStringSubmatch("ocreview: verdict=APPROVE spec=PASS quality=PASS base=abc head=" + head)
	if n == nil || n[1] != "APPROVE" || n[2] != head {
		t.Fatalf("note parse = %v", n)
	}
	plan, ok := parseSummary("# Plan critique\n...\nOCPLAN verdict=REVISE blockers=2 majors=3 writing=1 minors=0 nits=1")
	if !ok || plan.Kind != "OCPLAN" || plan.Verdict != "REVISE" || plan.Blockers != 2 || plan.Majors != 3 || plan.Writing != 1 || plan.Head != "" {
		t.Fatalf("plan summary parse = %+v", plan)
	}
}

func TestSelect(t *testing.T) {
	all := []Scenario{
		{Name: "reviewer-a", Tags: []string{"reviewer"}},
		{Name: "reviewer-b", Tags: []string{"reviewer"}},
		{Name: "e2e-x", Tags: []string{"e2e"}},
	}
	got, err := Select(all, []string{"reviewer-*"}, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("glob: %v %v", got, err)
	}
	got, err = Select(all, nil, []string{"e2e"})
	if err != nil || len(got) != 1 || got[0].Name != "e2e-x" {
		t.Fatalf("tag: %v %v", got, err)
	}
	if _, err := Select(all, []string{"nope"}, nil); err == nil {
		t.Fatal("no match should be an error")
	}
}

func TestRenderPrompt(t *testing.T) {
	ws := &Workspace{Work: "/w", Branch: "feat/x", Base: "b", Head: "h"}
	got, err := renderPrompt("Repository: {{.Repo}}\nBranch: {{.Branch}}\nBase: {{.Base}}\nHead: {{.Head}}", ws)
	if err != nil || got != "Repository: /w\nBranch: feat/x\nBase: b\nHead: h" {
		t.Fatalf("renderPrompt = %q, %v", got, err)
	}
	if _, err := renderPrompt("{{.Nope}}", ws); err == nil {
		t.Fatal("unknown field should fail")
	}
}

func TestWriteSummary(t *testing.T) {
	dir := t.TempDir()
	results := []Result{
		{Scenario: "s1", Model: "p/a", Status: "pass", DurationSec: 60, Verdict: "APPROVE",
			Summary: "OCREVIEW verdict=APPROVE spec=PASS quality=PASS blockers=0 majors=0 writing=0 minors=2 nits=1 head=abc"},
		{Scenario: "s1", Model: "p/a", Status: "fail", DurationSec: 120},
		{Scenario: "s2", Model: "p/b", Status: "pass", DurationSec: 30},
	}
	if err := writeSummary(dir, results); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"| s1 | p/a | 0 | pass | APPROVE | 0/0/0/2/1 | 1m0s |",
		"| p/a | 1/2 | 1m30s | 1/2 | - |",
		"| p/b | 1/1 | 30s | - | 1/1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestEditTargets(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: /tmp/opencode/reviews/x/a_test.go\n+package a\n*** Update File: internal/store/store.go\n@@\n*** Move to: /tmp/opencode/reviews/x/b.go\n*** End Patch"
	got := editTargets(map[string]any{"patchText": patch})
	want := []string{"/tmp/opencode/reviews/x/a_test.go", "internal/store/store.go", "/tmp/opencode/reviews/x/b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("patch targets = %v, want %v", got, want)
	}
	if got := editTargets(map[string]any{"path": "/tmp/x", "oldString": "a"}); len(got) != 1 || got[0] != "/tmp/x" {
		t.Fatalf("edit targets = %v", got)
	}
}

func TestRenderTemplateCommitsAndShort(t *testing.T) {
	ws := &Workspace{Base: "0123456789abcdef", Commits: []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}, Head: "bbbbbbbbbbbb"}
	got, err := renderTemplate("t", "{{short .Base}} {{index .Commits 0}} {{short .Head}}", ws.templateData())
	if err != nil || got != "0123456 aaaaaaaaaaaa bbbbbbb" {
		t.Fatalf("renderTemplate = %q, %v", got, err)
	}
}

func TestCommitSpecDirs(t *testing.T) {
	c := CommitSpec{Overlay: "a", Overlays: []string{"b", "c"}}
	if got := strings.Join(c.Dirs(), ","); got != "a,b,c" {
		t.Fatalf("Dirs = %s", got)
	}
}
