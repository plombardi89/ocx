package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/config"
)

func loadScenario(t *testing.T, root, name string) Scenario {
	t.Helper()
	all, err := LoadScenarios(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no scenario %s", name)
	return Scenario{}
}

// The /write-tests scenarios exercise the command as Phil runs it, so their
// prompts must stay the command's text.
func TestWriteTestsScenariosUseTheCommandText(t *testing.T) {
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	commands, err := config.LoadCommands(root)
	if err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, c := range commands {
		if c.Name == "write-tests" {
			body = strings.TrimSpace(c.Body)
		}
	}
	if !strings.Contains(body, "$ARGUMENTS") {
		t.Fatalf("write-tests command body = %q", body)
	}
	for name, plan := range map[string]string{
		"e2e-test-failed": "docs/plans/0001-product-pagination.md",
		"e2e-test-shell":  "docs/plans/0001-backup-manifest.md",
	} {
		s := loadScenario(t, root, name)
		if got, want := strings.TrimSpace(s.Prompt), strings.ReplaceAll(body, "$ARGUMENTS", plan); got != want {
			t.Errorf("%s prompt differs from /write-tests:\n%s\nwant\n%s", name, got, want)
		}
	}
}

// The stubbed scenarios feed build fixed reports. All but one must be valid
// for their plans, so build is tested on what it does with a good report;
// the invalid one must fail only because it skips a criterion.
func TestStubReportsFitTheirPlans(t *testing.T) {
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	scenarios := filepath.Join(root, config.ScenariosDir)
	read := func(p string) string {
		data, err := os.ReadFile(filepath.Join(scenarios, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	splitPlan := read("reviewer-clean-change/overlay/docs/plans/0002-split-amounts.md")
	vaguePlan := read("test-writer-vague-plan/overlay/docs/plans/0002-split-amounts.md")
	pagination := read("_shared/pagination-fix/docs/plans/0001-product-pagination.md")
	cases := []struct {
		report, plan, planPath string
		invalid                bool
	}{
		{"split-complete.md", splitPlan, "docs/plans/0002-split-amounts.md", false},
		{"split-gap.md", vaguePlan, "docs/plans/0002-split-amounts.md", false},
		{"split-incomplete-flaky.md", splitPlan, "docs/plans/0002-split-amounts.md", false},
		{"split-incomplete-disk.md", splitPlan, "docs/plans/0002-split-amounts.md", false},
		{"pagination-six-criteria.md", pagination, "docs/plans/0001-product-pagination.md", true},
	}
	for _, c := range cases {
		t.Run(c.report, func(t *testing.T) {
			ws := codeRepo(t, map[string]string{c.planPath: c.plan})
			report := strings.ReplaceAll(read("_shared/stub-reports/"+c.report), "{{HEAD}}", ws.Head)
			got := failing(checkTestsReport(ws, report, TestsExpect{Plan: c.planPath}))
			if !c.invalid && len(got) > 0 {
				t.Fatalf("report doesn't fit its plan: %v", got)
			}
			if c.invalid {
				joined := strings.Join(got, "\n")
				if !strings.Contains(joined, "criterion 5 listed 0 times") || !strings.Contains(joined, "criteria matches the plan") {
					t.Fatalf("invalid report failed for the wrong reasons: %v", got)
				}
			}
		})
	}
}
