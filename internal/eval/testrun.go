package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/plombardi89/ocx/internal/gitx"
	"github.com/plombardi89/ocx/internal/stage"
)

// Output that means the returned test code itself is broken, rather than
// that a test found a bug, by language.
var defaultBroken = map[string]string{
	"go":     `\[build failed\]|\[setup failed\]`,
	"python": `(?m)\b(SyntaxError|IndentationError|ImportError|ModuleNotFoundError)\b`,
	"shell":  `(?m)syntax error|command not found`,
}

var (
	goRanTest = regexp.MustCompile(`(?m)^=== RUN\s+(Test\w+)`)
	pyRan     = regexp.MustCompile(`(?m)^Ran (\d+) tests?`)
)

// testRun is the outcome of running the returned tests once.
type testRun struct {
	Failed bool
	Ran    int // tests seen running; shell counts the test functions defined
	Broken []string
	Output string
	Err    error // the tests couldn't be run at all
}

// checkout clones src into a temporary directory, checks out rev, and lays
// the overlays over it in order. cleanup removes the directory.
func checkout(src, rev string, overlays []string) (repo string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "ocx-tests-run-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	repo = filepath.Join(dir, "repo")
	if _, err := gitx.Run(dir, "clone", "-q", "--no-checkout", src, repo); err != nil {
		return "", cleanup, err
	}
	if _, err := gitx.Run(repo, "checkout", "-q", "--detach", rev); err != nil {
		return "", cleanup, err
	}
	for _, o := range overlays {
		if err := stage.CopyTree(o, repo); err != nil {
			return "", cleanup, fmt.Errorf("overlay %s: %w", o, err)
		}
	}
	return repo, cleanup, nil
}

// runReturnedTests runs the returned tests on the workspace's Head with the
// overlays laid over it.
func runReturnedTests(ws *Workspace, files []TestFile, overlays []string, broken string) testRun {
	return runReturnedTestsAt(ws.Work, ws.Head, files, overlays, broken)
}

// runReturnedTestsAt checks out rev of src with the overlays, adds the
// returned test files, and runs only those tests.
func runReturnedTestsAt(src, rev string, files []TestFile, overlays []string, broken string) testRun {
	repo, cleanup, err := checkout(src, rev, overlays)
	defer cleanup()
	if err != nil {
		return testRun{Err: err}
	}
	byLang := map[string][]TestFile{}
	for _, f := range files {
		p, _ := cleanRepoPath(f.Path)
		full, err := writablePath(repo, p)
		if err != nil {
			return testRun{Err: err}
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return testRun{Err: err}
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			return testRun{Err: err}
		}
		lang := language(p)
		byLang[lang] = append(byLang[lang], TestFile{Path: p, Content: f.Content})
	}

	var out testRun
	var output strings.Builder
	langs := make([]string, 0, len(byLang))
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		var r testRun
		switch lang {
		case "go":
			r = runGo(repo, byLang[lang])
		case "python":
			r = runPython(repo, byLang[lang])
		case "shell":
			r = runShell(repo, byLang[lang])
		default:
			r = testRun{Err: fmt.Errorf("can't run %s files", lang)}
		}
		if r.Err != nil {
			return r
		}
		pattern := broken
		if pattern == "" {
			pattern = defaultBroken[lang]
		}
		if pattern != "" {
			for _, m := range regexp.MustCompile(pattern).FindAllString(r.Output, 5) {
				out.Broken = append(out.Broken, m)
			}
		}
		out.Failed = out.Failed || r.Failed
		out.Ran += r.Ran
		output.WriteString(r.Output)
	}
	out.Output = output.String()
	return out
}

// writablePath returns where to write the repository-relative path p in
// repo. It refuses a path that goes through a link, which could point
// outside the checkout.
func writablePath(repo, p string) (string, error) {
	full := repo
	for _, part := range strings.Split(p, "/") {
		full = filepath.Join(full, part)
		fi, err := os.Lstat(full)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s goes through a link", p)
		}
	}
	return filepath.Join(repo, filepath.FromSlash(p)), nil
}

func language(p string) string {
	switch path.Ext(p) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".sh", ".bash":
		return "shell"
	}
	return path.Ext(p)
}

func runCommand(dir string, name string, args ...string) (string, bool, error) {
	return runCommandWith(dir, nil, nil, name, args...)
}

// runCommandWith is runCommand with extra environment variables and extra
// open files, which the command sees as file descriptors 3 and up.
func runCommandWith(dir string, env []string, files []*os.File, name string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1"), env...)
	cmd.ExtraFiles = files
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if ctx.Err() != nil {
		return buf.String(), true, fmt.Errorf("%s timed out", name)
	}
	if _, ok := err.(*exec.ExitError); ok {
		return buf.String(), true, nil
	}
	return buf.String(), false, err
}

// runGo runs the returned test functions, package by package.
func runGo(repo string, files []TestFile) testRun {
	byPkg := map[string][]string{}
	for _, f := range files {
		pkg := "./" + path.Dir(f.Path)
		byPkg[pkg] = append(byPkg[pkg], TestNames(f)...)
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	var r testRun
	for _, pkg := range pkgs {
		names := byPkg[pkg]
		if len(names) == 0 {
			continue
		}
		pattern := "^(" + strings.Join(names, "|") + ")$"
		out, failed, err := runCommand(repo, "go", "test", "-count=1", "-v", "-run", pattern, pkg)
		if err != nil {
			return testRun{Err: err, Output: out}
		}
		r.Failed = r.Failed || failed
		r.Output += out
		for _, m := range goRanTest.FindAllStringSubmatch(out, -1) {
			for _, n := range names {
				if m[1] == n {
					r.Ran++
				}
			}
		}
	}
	return r
}

// runPython runs the returned test modules with unittest from the
// repository root.
func runPython(repo string, files []TestFile) testRun {
	var modules []string
	for _, f := range files {
		modules = append(modules, strings.ReplaceAll(strings.TrimSuffix(f.Path, ".py"), "/", "."))
	}
	out, failed, err := runCommand(repo, "python3", append([]string{"-m", "unittest", "-v"}, modules...)...)
	if err != nil {
		return testRun{Err: err, Output: out}
	}
	r := testRun{Failed: failed, Output: out}
	if m := pyRan.FindStringSubmatch(out); m != nil {
		fmt.Sscan(m[1], &r.Ran)
	}
	return r
}

var shTraceCall = regexp.MustCompile(`(?m)^\++ (test_\w+)(?:\s|$)`)

// runShell runs each returned script on its own, tracing it to a separate
// file, and counts the test functions the trace shows were called. Defining
// a test function isn't running it.
func runShell(repo string, files []TestFile) testRun {
	var r testRun
	for _, f := range files {
		trace, err := os.CreateTemp("", "ocx-trace-")
		if err != nil {
			return testRun{Err: err}
		}
		out, failed, err := runCommandWith(repo, []string{"BASH_XTRACEFD=3"}, []*os.File{trace}, "bash", "-x", f.Path)
		data, _ := os.ReadFile(trace.Name())
		trace.Close()
		os.Remove(trace.Name())
		if err != nil {
			return testRun{Err: err, Output: out}
		}
		r.Failed = r.Failed || failed
		r.Output += out
		defined := map[string]bool{}
		for _, n := range TestNames(f) {
			defined[n] = true
		}
		called := map[string]bool{}
		for _, m := range shTraceCall.FindAllStringSubmatch(string(data), -1) {
			if defined[m[1]] {
				called[m[1]] = true
			}
		}
		r.Ran += len(called)
	}
	return r
}

// checkReturnedTests runs a report's test files on Head, on each correct
// implementation, and on each mutant. Overlay paths are relative to dir.
func checkReturnedTests(ws *Workspace, dir string, r TestsReport, e TestsExpect) []Check {
	var checks []Check
	add := func(name string, ok bool, detail string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}
	files := runnableFiles(ws, r.Files)
	if len(files) == 0 {
		return nil
	}
	resolve := func(o string) string {
		if filepath.IsAbs(o) {
			return o
		}
		return filepath.Join(dir, o)
	}
	excerpt := func(run testRun) string { return truncate(strings.TrimSpace(run.Output), 600) }

	head := runReturnedTests(ws, files, nil, e.Broken)
	if head.Err != nil {
		add("returned tests run on head", false, "%v: %s", head.Err, excerpt(head))
		return checks
	}
	add("returned tests run on head", head.Ran > 0, "no returned test ran: %s", excerpt(head))
	add("returned test code isn't broken", len(head.Broken) == 0, "%q in: %s", head.Broken, excerpt(head))
	add("returned tests fail on head if and only if a criterion FAILED", head.Failed == (r.Failed > 0),
		"run failed=%v, report failed=%d: %s", head.Failed, r.Failed, excerpt(head))

	for _, o := range e.Fixed {
		run := runReturnedTests(ws, files, []string{resolve(o)}, e.Broken)
		add("returned tests pass with "+o, run.Err == nil && !run.Failed && run.Ran > 0 && len(run.Broken) == 0,
			"err=%v failed=%v ran=%d: %s", run.Err, run.Failed, run.Ran, excerpt(run))
	}
	for _, m := range e.Mutants {
		run := runReturnedTests(ws, files, []string{resolve(m.Overlay)}, e.Broken)
		add("returned tests catch mutant "+m.Name, run.Err == nil && run.Failed && len(run.Broken) == 0,
			"err=%v failed=%v broken=%q: %s", run.Err, run.Failed, run.Broken, excerpt(run))
	}
	return checks
}

// runnableFiles keeps the returned files that are safe to write: inside
// the repository, new at Head, and listed once. The report checks flag the
// others.
func runnableFiles(ws *Workspace, files []TestFile) []TestFile {
	seen := map[string]bool{}
	var out []TestFile
	for _, f := range files {
		p, ok := cleanRepoPath(f.Path)
		if !ok || seen[p] || existsAt(ws.Work, ws.Head, p) || linkedAt(ws.Work, ws.Head, p) {
			continue
		}
		seen[p] = true
		out = append(out, TestFile{Path: p, Content: f.Content})
	}
	return out
}
