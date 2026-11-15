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
	// The existing test, which checks Add(2, 3), misses this one.
	addZeroIsOne = "package calc\n\nfunc Add(a, b int) int {\n\tif a == 0 && b == 0 {\n\t\treturn 1\n\t}\n\treturn a + b\n}\n"
	addWrong     = "package calc\n\nfunc Add(a, b int) int { return a - b }\n"
)

// existingAddTest is the repository's own test. It checks Add(2, 3) only,
// so it misses addZeroIsOne and catches addOffByOne and addWrong.
const existingAddTest = "package calc\n\nimport \"testing\"\n\nfunc TestAddsTwoNumbers(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(Add(2, 3))\n\t}\n}\n"

func goRepo(t *testing.T, impl string) *Workspace {
	t.Helper()
	return goRepoWith(t, impl, existingAddTest)
}

func goRepoWith(t *testing.T, impl, existing string) *Workspace {
	t.Helper()
	return codeRepo(t, map[string]string{
		"go.mod":            "module example.com/m\n\ngo 1.22\n",
		"calc/calc.go":      impl,
		"calc/calc_test.go": existing,
	})
}

var goTestCommand = []string{"go", "test", "-count=1", "./..."}

func goTest(name, a, b, want string) string {
	return "func " + name + "(t *testing.T) {\n\tif got := Add(" + a + ", " + b + "); got != " + want + " {\n\t\tt.Fatalf(\"got %d\", got)\n\t}\n}\n"
}

func goFile(tests ...string) TestFile {
	return TestFile{Path: "calc/sum_test.go", Content: "package calc\n\nimport \"testing\"\n\n" + strings.Join(tests, "\n")}
}

func TestReturnedTestsGetCreditOnlyForTheDifferenceTheyMake(t *testing.T) {
	dir := t.TempDir()
	correct := overlay(t, dir, "correct", map[string]string{"calc/calc.go": addCorrect})
	zeroIsOne := overlay(t, dir, "zero-is-one", map[string]string{"calc/calc.go": addZeroIsOne})
	offByOne := overlay(t, dir, "off-by-one", map[string]string{"calc/calc.go": addOffByOne})
	stillWrong := overlay(t, dir, "still-wrong", map[string]string{"calc/calc.go": addWrong})
	// An existing test that passes on the wrong implementation, for heads
	// that hold the bug.
	existingZeroes := "package calc\n\nimport \"testing\"\n\nfunc TestAddsZeroes(t *testing.T) {\n\tif Add(0, 0) != 0 {\n\t\tt.Fatal()\n\t}\n}\n"

	cases := []struct {
		name           string
		impl, existing string
		files          []TestFile
		failed         int
		expect         TestsExpect
		fails          []string
	}{
		{
			name:   "tests that pass on head and catch a mutant the existing tests miss",
			impl:   addCorrect,
			files:  []TestFile{goFile(goTest("TestAddingZeroes", "0", "0", "0"))},
			expect: TestsExpect{Fixed: []string{correct}, Mutants: []Mutant{{Name: "zero-is-one", Overlay: zeroIsOne}}},
		},
		{
			name:   "a mutant the existing tests already catch",
			impl:   addCorrect,
			files:  []TestFile{goFile(goTest("TestAddingZeroes", "0", "0", "0"))},
			expect: TestsExpect{Mutants: []Mutant{{Name: "off-by-one", Overlay: offByOne}}},
			fails:  []string{"existing tests miss mutant off-by-one"},
		},
		{
			// Calling, importing, or inheriting an existing test can't earn
			// credit: the mutant fails the existing tests on their own.
			name:   "a returned test that calls an existing one",
			impl:   addCorrect,
			files:  []TestFile{goFile("func TestAddingZeroesAndMore(t *testing.T) {\n\tTestAddsTwoNumbers(t)\n}\n")},
			expect: TestsExpect{Mutants: []Mutant{{Name: "off-by-one", Overlay: offByOne}}},
			fails:  []string{"existing tests miss mutant off-by-one"},
		},
		{
			name:   "tests that miss a mutant",
			impl:   addCorrect,
			files:  []TestFile{goFile(goTest("TestAddingOnes", "1", "1", "2"))},
			expect: TestsExpect{Mutants: []Mutant{{Name: "zero-is-one", Overlay: zeroIsOne}}},
			fails:  []string{"catch mutant zero-is-one"},
		},
		{
			name:  "a report that claims passing tests that fail",
			impl:  addCorrect,
			files: []TestFile{goFile(goTest("TestAddingOnes", "1", "1", "3"))},
			fails: []string{"if and only if a criterion FAILED"},
		},
		{
			name:     "failing tests that pass with the correct implementation",
			impl:     addWrong,
			existing: existingZeroes,
			files:    []TestFile{goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))},
			failed:   1,
			expect:   TestsExpect{Fixed: []string{correct}},
		},
		{
			name:     "failing tests that still fail with a supposedly correct implementation",
			impl:     addWrong,
			existing: existingZeroes,
			files:    []TestFile{goFile(goTest("TestAddingTwoAndThree", "2", "3", "5"))},
			failed:   1,
			expect:   TestsExpect{Fixed: []string{stillWrong}},
			fails:    []string{"pass with still-wrong"},
		},
		{
			name:   "broken test code reported as a failure",
			impl:   addCorrect,
			files:  []TestFile{goFile("func TestBroken(t *testing.T) {\n\tif Add(1, 1) != {\n}\n")},
			failed: 1,
			expect: TestsExpect{Fixed: []string{correct}},
			fails:  []string{"pass with correct"},
		},
		{
			name:     "a head whose existing tests already fail",
			impl:     addWrong,
			existing: existingAddTest,
			files:    []TestFile{goFile(goTest("TestAddingZeroes", "0", "0", "0"))},
			fails:    []string{"passes on head without the returned tests"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			existing := c.existing
			if existing == "" {
				existing = existingAddTest
			}
			ws := goRepoWith(t, c.impl, existing)
			c.expect.Command = goTestCommand
			got := failing(checkReturnedTests(ws, dir, TestsReport{Failed: c.failed, Files: c.files}, c.expect))
			if len(c.fails) == 0 && len(got) > 0 {
				t.Fatalf("unexpected failures: %v", got)
			}
			for _, want := range c.fails {
				if !strings.Contains(strings.Join(got, "\n"), want) {
					t.Errorf("no failing check containing %q; failures: %v", want, got)
				}
			}
		})
	}
}

func TestReturnedPythonAndShellTests(t *testing.T) {
	ws := codeRepo(t, map[string]string{
		"catalog/__init__.py":    "",
		"catalog/tags.py":        "def tag(name):\n    return name.upper()\n",
		"tests/__init__.py":      "",
		"tests/test_existing.py": "import unittest\n\nfrom catalog.tags import tag\n\n\nclass TagTest(unittest.TestCase):\n    def test_upper(self):\n        self.assertEqual(tag(\"a\"), \"A\")\n",
		"bin/greet.sh":           "#!/bin/sh\necho \"hello $1\"\n",
	})
	dir := t.TempDir()
	emptyName := overlay(t, dir, "empty-name", map[string]string{"catalog/tags.py": "def tag(name):\n    return name.upper() or \"?\"\n"})
	pyCommand := []string{"python3", "-m", "unittest", "discover", "-s", "tests"}
	shCommand := []string{"sh", "-c", `for f in "$@"; do bash "$f" || exit 1; done`, "_", FilesArg}

	py := TestFile{Path: "tests/test_empty_tag.py", Content: "import unittest\n\nfrom catalog.tags import tag\n\n\nclass EmptyTagTest(unittest.TestCase):\n    def test_empty_name_stays_empty(self):\n        self.assertEqual(tag(\"\"), \"\")\n"}
	got := failing(checkReturnedTests(ws, dir, TestsReport{Files: []TestFile{py}}, TestsExpect{Command: pyCommand, Mutants: []Mutant{{Name: "empty name", Overlay: emptyName}}}))
	if len(got) > 0 {
		t.Fatalf("Python tests that catch a mutant: %v", got)
	}
	// Inheriting the existing class runs its tests too, but they pass on the
	// mutant, so they add nothing and the returned test gets no credit.
	inherits := TestFile{Path: py.Path, Content: "from tests.test_existing import TagTest\n\n\nclass MoreTagTest(TagTest):\n    pass\n"}
	got = failing(checkReturnedTests(ws, dir, TestsReport{Files: []TestFile{inherits}}, TestsExpect{Command: pyCommand, Mutants: []Mutant{{Name: "empty name", Overlay: emptyName}}}))
	if !strings.Contains(strings.Join(got, "\n"), "catch mutant empty name") {
		t.Fatalf("a Python class that only inherits existing tests got credit: %v", got)
	}

	sh := TestFile{Path: "tests/greet_test.sh", Content: "set -euo pipefail\n[ \"$(sh bin/greet.sh Ann)\" = \"hi Ann\" ]\n"}
	got = failing(checkReturnedTests(ws, dir, TestsReport{Files: []TestFile{sh}}, TestsExpect{Command: shCommand}))
	if len(got) != 1 || !strings.Contains(got[0], "if and only if a criterion FAILED") {
		t.Fatalf("a failing shell test reported as passing: %v", got)
	}
}

func TestNoReturnedFilesMeansNoRuns(t *testing.T) {
	ws := goRepo(t, addCorrect)
	if checks := checkReturnedTests(ws, t.TempDir(), TestsReport{}, TestsExpect{Command: goTestCommand}); len(checks) != 0 {
		t.Fatalf("checks without files: %+v", checks)
	}
	got := failing(checkReturnedTests(ws, t.TempDir(), TestsReport{Files: []TestFile{goFile(goTest("TestA", "0", "0", "0"))}}, TestsExpect{}))
	if len(got) != 1 || !strings.Contains(got[0], "scenario has a test command") {
		t.Fatalf("files but no command: %v", got)
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
	f := TestFile{Path: "tests/escaped_test.sh", Content: "true\n"}

	if files := runnableFiles(ws, []TestFile{f}); len(files) != 0 {
		t.Fatalf("a path through a link counts as runnable: %v", files)
	}
	if _, _, err := runTests(ws.Work, ws.Head, nil, []TestFile{f}, []string{"true"}); err == nil {
		t.Fatal("writing through a link succeeded")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote %d files outside the checkout", len(entries))
	}
}
