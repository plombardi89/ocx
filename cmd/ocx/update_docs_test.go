package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var updCommandWords = []string{"apply", "remove", "update", "version"}

var updUsageLine = regexp.MustCompile(`(?m)^\s+ocx\s+([a-z][a-z-]*)\b`)

func updListedCommands(usageText string) []string {
	var names []string
	for _, m := range updUsageLine.FindAllStringSubmatch(usageText, -1) {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
}

func updReadText(t *testing.T, rel ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// updLineWith returns the first line of text that contains marker.
func updLineWith(t *testing.T, text, marker string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("no line contains %q", marker)
	return ""
}

func TestUsageNamesApplyRemoveUpdateAndVersion(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(nil, &out, &errOut); code != 2 {
		t.Errorf("ocx with no arguments exited %d, want 2", code)
	}
	if got := updListedCommands(errOut.String()); strings.Join(got, ",") != strings.Join(updCommandWords, ",") {
		t.Errorf("the usage lists %v, want %v\n%s", got, updCommandWords, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"help"}, &out, &errOut); code != 0 {
		t.Errorf("ocx help exited %d, want 0", code)
	}
	if got := updListedCommands(out.String()); strings.Join(got, ",") != strings.Join(updCommandWords, ",") {
		t.Errorf("the help lists %v, want %v\n%s", got, updCommandWords, out.String())
	}
}

func TestReadmeLayoutAndAgentsListTheFourCommands(t *testing.T) {
	cases := []struct{ file, marker string }{
		{"README.md", "cmd/ocx/"},
		{"AGENTS.md", "`cmd/ocx/`"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			line := updLineWith(t, updReadText(t, c.file), c.marker)
			for _, word := range updCommandWords {
				if !strings.Contains(line, word) {
					t.Errorf("the %s line for cmd/ocx doesn't mention %s: %s", c.file, word, line)
				}
			}
		})
	}
}

func TestTheUsageTestIsNamedForTheFourCommands(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				declared[fn.Name.Name] = true
			}
		}
	}
	if !declared["TestUsageListsOnlyTheFourCommands"] {
		t.Error("there is no test named TestUsageListsOnlyTheFourCommands")
	}
	if declared["TestUsageListsOnlyApplyRemoveAndVersion"] {
		t.Error("the test named TestUsageListsOnlyApplyRemoveAndVersion still exists")
	}
}

func TestANewADRReplacesADR0008AndDescribesUpdate(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "docs", "adr"))
	if err != nil {
		t.Fatal(err)
	}
	replacesOld := regexp.MustCompile(`(?m)^Replaces:\s*0008\b`)
	var replacing []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0008-") {
			t.Errorf("ADR 0008 still exists: %s", e.Name())
		}
		if replacesOld.MatchString(updReadText(t, "docs", "adr", e.Name())) {
			replacing = append(replacing, e.Name())
		}
	}
	if len(replacing) != 1 {
		t.Fatalf("%d ADRs name 0008 in Replaces: %v, want exactly one", len(replacing), replacing)
	}
	text := updReadText(t, "docs", "adr", replacing[0])
	for _, want := range []string{
		"ocx update", "OCX_RELEASE_URL",
		"hooks", "branch protection", "--no-verify", "git note", "backups",
		"apply", "remove", "version", "amd64", "arm64", "latest", "go install",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the ADR %s doesn't mention %q", replacing[0], want)
		}
	}
}

func TestReadmeUpgradeStepIsOcxUpdateApply(t *testing.T) {
	item := updLineWith(t, updReadText(t, "README.md"), "**Upgrade:**")
	for _, want := range []string{
		"ocx update --apply",
		"ocx apply",
		"again",
		"differs",
		"go install",
	} {
		if !strings.Contains(item, want) {
			t.Errorf("the README's upgrade step doesn't mention %q: %s", want, item)
		}
	}
}
