package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/gitx"
)

// codeRepo returns a workspace whose Head holds files.
func codeRepo(t *testing.T, files map[string]string) *Workspace {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	work := t.TempDir()
	if err := gitx.Init(work); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, work, files)
	head, err := gitx.CommitAll(work, "Initial commit")
	if err != nil {
		t.Fatal(err)
	}
	return &Workspace{Work: work, Head: head, Base: head, Branch: "main"}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// overlay writes files into a new directory under dir and returns its name.
func overlay(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	writeFiles(t, filepath.Join(dir, name), files)
	return name
}

const (
	addCorrect = "package calc\n\nfunc Add(a, b int) int { return a + b }\n"
	// An existing test that checks Add(2, 3) catches this; a test that only
	// checks Add(0, 0) doesn't.
	addOffByOne  = "package calc\n\nfunc Add(a, b int) int {\n\tif a > 0 {\n\t\treturn a + b + 1\n\t}\n\treturn a + b\n}\n"
	addAlwaysOne = "package calc\n\nfunc Add(a, b int) int { return 1 }\n"
	addWrong     = "package calc\n\nfunc Add(a, b int) int { return a - b }\n"
)

func goRepo(t *testing.T, impl string) *Workspace {
	return codeRepo(t, map[string]string{
		"go.mod":            "module example.com/m\n\ngo 1.22\n",
		"calc/calc.go":      impl,
		"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestAddsTwoNumbers(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(Add(2, 3))\n\t}\n}\n",
	})
}

func goTest(name, a, b, want string) string {
	return "func " + name + "(t *testing.T) {\n\tif got := Add(" + a + ", " + b + "); got != " + want + " {\n\t\tt.Fatalf(\"got %d\", got)\n\t}\n}\n"
}

func goFile(tests ...string) TestFile {
	return TestFile{Path: "calc/sum_test.go", Content: "package calc\n\nimport \"testing\"\n\n" + strings.Join(tests, "\n")}
}

func TestReturnedTestsOnly(t *testing.T) {
	dir := t.TempDir()
	correct := overlay(t, dir, "correct", map[string]string{"calc/calc.go": addCorrect})
	offByOne := overlay(t, dir, "off-by-one", map[string]string{"calc/calc.go": addOffByOne})
	alwaysOne := overlay(t, dir, "always-one", map[string]string{"calc/calc.go": addAlwaysOne})
	stillWrong := overlay(t, dir, "still-wrong", map[string]string{"calc/calc.go": addWrong})

	cases := []struct {
		name   string
		impl   string
		files  []TestFile
		failed int
		expect TestsExpect
		fails  []string
	}{
		{
			name:   "tests that pass on head and catch a mutant",
			impl:   addCorrect,
			files:  []TestFile{goFile(goTest("TestAddingZeroes", "0", "0", "0"))},
			expect: TestsExpect{Fixed: []string{correct}, Mutants: []Mutant{{Name: "always-one", Overlay: alwaysOne}}},
		},
		{
			name:   "a mutant only an existing test catches",
			impl:   addCorrect,
			files:  []TestFile{goFile(goTest("TestAddingZeroes", "0", "0", "0"))},
			expect: TestsExpect{Mutants: []Mutant{{Name: "off-by-one", Overlay: offByOne}}},
			fails:  []string{"catch mutant off-by-one"},
		},
		{
			name:  "a report that claims passing tests that fail",
			impl:  addCorrect,
			files: []TestFile{goFile(goTest("TestAddingOnes", "1", "1", "3"))},
			fails: []string{"if and only if a criterion FAILED"},
		},
		{
			name:   "failing tests that pass with the correct implementation",
			impl:   addWrong,
			files:  []TestFile{goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))},
			failed: 1,
			expect: TestsExpect{Fixed: []string{correct}},
		},
		{
			name:   "failing tests that still fail with a supposedly correct implementation",
			impl:   addWrong,
			files:  []TestFile{goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))},
			failed: 1,
			expect: TestsExpect{Fixed: []string{stillWrong}},
			fails:  []string{"pass with still-wrong"},
		},
		{
			name:   "broken test code",
			impl:   addCorrect,
			files:  []TestFile{goFile("func TestBroken(t *testing.T) {\n\tif Add(1, 1) != {\n}\n")},
			failed: 1,
			fails:  []string{"isn't broken"},
		},
		{
			name:  "a file with no tests in it",
			impl:  addCorrect,
			files: []TestFile{goFile("func helper() int { return Add(1, 1) }\n")},
			fails: []string{"run on head"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := goRepo(t, c.impl)
			r := TestsReport{Failed: c.failed, Files: c.files}
			got := failing(checkReturnedTests(ws, dir, r, c.expect))
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

func TestReturnedPythonAndShellTests(t *testing.T) {
	ws := codeRepo(t, map[string]string{
		"catalog/__init__.py": "",
		"catalog/tags.py":     "def tag(name):\n    return name.upper()\n",
		"tests/__init__.py":   "",
		"bin/greet.sh":        "#!/bin/sh\necho \"hello $1\"\n",
	})
	py := TestFile{Path: "tests/test_tag_case.py", Content: "import unittest\n\nfrom catalog.tags import tag\n\n\nclass TagTest(unittest.TestCase):\n    def test_tags_are_upper_case(self):\n        self.assertEqual(tag(\"new\"), \"NEW\")\n"}
	sh := TestFile{Path: "tests/greet_test.sh", Content: "#!/usr/bin/env bash\nset -euo pipefail\nfailures=0\ntest_greets_by_name() {\n\t[ \"$(sh bin/greet.sh Ann)\" = \"hello Ann\" ] || failures=$((failures + 1))\n}\ntest_greets_by_name\n[ \"$failures\" -eq 0 ]\n"}
	if got := failing(checkReturnedTests(ws, t.TempDir(), TestsReport{Files: []TestFile{py, sh}}, TestsExpect{})); len(got) > 0 {
		t.Fatalf("passing Python and shell tests: %v", got)
	}

	shFails := TestFile{Path: sh.Path, Content: strings.Replace(sh.Content, "hello Ann", "hi Ann", 1)}
	got := failing(checkReturnedTests(ws, t.TempDir(), TestsReport{Files: []TestFile{shFails}}, TestsExpect{}))
	if len(got) != 1 || !strings.Contains(got[0], "if and only if a criterion FAILED") {
		t.Fatalf("failing shell test reported as passing: %v", got)
	}

	pyBroken := TestFile{Path: py.Path, Content: "import unittest\nfrom catalog.nope import tag\n"}
	got = failing(checkReturnedTests(ws, t.TempDir(), TestsReport{Failed: 1, Files: []TestFile{pyBroken}}, TestsExpect{}))
	if !strings.Contains(strings.Join(got, "\n"), "isn't broken") {
		t.Fatalf("Python import error not flagged: %v", got)
	}
}

func TestNoReturnedFilesMeansNoRuns(t *testing.T) {
	ws := goRepo(t, addCorrect)
	if checks := checkReturnedTests(ws, t.TempDir(), TestsReport{}, TestsExpect{}); len(checks) != 0 {
		t.Fatalf("checks without files: %+v", checks)
	}
}

func TestReturnedFilesAreNeverWrittenThroughALink(t *testing.T) {
	ws := codeRepo(t, map[string]string{"readme.txt": "x\n"})
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws.Work, "tests")); err != nil {
		t.Fatal(err)
	}
	head, err := gitx.CommitAll(ws.Work, "Link the tests directory")
	if err != nil {
		t.Fatal(err)
	}
	ws.Head = head
	f := TestFile{Path: "tests/escaped_test.sh", Content: "test_ok() { :; }\ntest_ok\n"}

	if files := runnableFiles(ws, []TestFile{f}); len(files) != 0 {
		t.Fatalf("a path through a link counts as runnable: %v", files)
	}
	if run := runReturnedTests(ws, []TestFile{f}, nil, ""); run.Err == nil {
		t.Fatal("writing through a link succeeded")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote %d files outside the checkout", len(entries))
	}
}

func TestShellTestsCountOnlyWhenCalled(t *testing.T) {
	ws := codeRepo(t, map[string]string{"readme.txt": "x\n"})
	defined := TestFile{Path: "tests/defined_test.sh", Content: "set -euo pipefail\ntest_checks_output() { return 1; }\n"}
	got := failing(checkReturnedTests(ws, t.TempDir(), TestsReport{Passed: 1, Files: []TestFile{defined}}, TestsExpect{}))
	if !strings.Contains(strings.Join(got, "\n"), "run on head") {
		t.Fatalf("a test function that's never called counted as run: %v", got)
	}
	called := TestFile{Path: defined.Path, Content: "set -euo pipefail\ntest_checks_output() { :; }\ntest_checks_output\n"}
	if got := failing(checkReturnedTests(ws, t.TempDir(), TestsReport{Passed: 1, Files: []TestFile{called}}, TestsExpect{})); len(got) > 0 {
		t.Fatalf("a called test function: %v", got)
	}
}

// A test declared inside a string isn't a returned test, so an existing
// test with that name never runs and can't take the credit for a mutant.
func TestTestNamesInStringsDontSelectExistingTests(t *testing.T) {
	dir := t.TempDir()
	offByOne := overlay(t, dir, "off-by-one", map[string]string{"calc/calc.go": addOffByOne})
	ws := goRepo(t, addCorrect)
	f := goFile(goTest("TestAddingZeroes", "0", "0", "0"), "const named = `\nfunc TestAddsTwoNumbers(t *testing.T) {}\n`\n")
	if names := TestNames(f); strings.Join(names, ",") != "TestAddingZeroes" {
		t.Fatalf("TestNames = %v", names)
	}
	got := strings.Join(failing(checkReturnedTests(ws, dir, TestsReport{Files: []TestFile{f}}, TestsExpect{Mutants: []Mutant{{Name: "off-by-one", Overlay: offByOne}}})), "\n")
	if !strings.Contains(got, "catch mutant off-by-one") {
		t.Fatalf("an existing test caught the mutant for the returned ones; failures:\n%s", got)
	}
}
