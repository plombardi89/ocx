package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/plombardi89/ocx/internal/gitx"
	"github.com/plombardi89/ocx/internal/stage"
)

// FilesArg stands for the returned files' paths in a scenario's test
// command, for runners that don't find new tests by themselves.
const FilesArg = "{files}"

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

// runTests checks out rev of src with the overlays laid over it, writes the
// files, and runs command there. FilesArg in command becomes the files'
// paths, or nothing when there are none. failed reports whether the command
// exited with an error.
func runTests(src, rev string, overlays []string, files []TestFile, command []string) (out string, failed bool, err error) {
	repo, cleanup, err := checkout(src, rev, overlays)
	defer cleanup()
	if err != nil {
		return "", false, err
	}
	var paths []string
	for _, f := range files {
		p, _ := cleanRepoPath(f.Path)
		full, err := writablePath(repo, p)
		if err != nil {
			return "", false, err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", false, err
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			return "", false, err
		}
		paths = append(paths, p)
	}
	var args []string
	for _, a := range command[1:] {
		if a == FilesArg {
			args = append(args, paths...)
		} else {
			args = append(args, a)
		}
	}
	return runCommand(repo, command[0], args...)
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

func runCommand(dir string, name string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
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

// checkReturnedTests judges a report's test files by the difference they
// make to the scenario's test command, the same for every language. A
// failure counts for the returned tests only when the command fails with
// them and passes without them, so nothing about which tests run, or what
// the returned tests call, can give them credit for what existing tests
// already catch. The checks run on Head, on each correct implementation,
// and on each mutant; overlay paths are relative to dir.
func checkReturnedTests(ws *Workspace, dir string, r TestsReport, e TestsExpect) []Check {
	var checks []Check
	add := func(name string, ok bool, detail string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}
	files := runnableFiles(ws, r.Files)
	if len(files) == 0 {
		return nil
	}
	if len(e.Command) == 0 {
		add("scenario has a test command", false, "set tests.command to run the returned tests")
		return checks
	}
	resolve := func(o string) []string {
		if o == "" {
			return nil
		}
		if filepath.IsAbs(o) {
			return []string{o}
		}
		return []string{filepath.Join(dir, o)}
	}
	run := func(overlay string, with []TestFile) (string, bool, error) {
		return runTests(ws.Work, ws.Head, resolve(overlay), with, e.Command)
	}
	excerpt := func(out string) string { return truncate(strings.TrimSpace(out), 600) }

	out, failed, err := run("", nil)
	add("test command passes on head without the returned tests", err == nil && !failed, "err=%v: %s", err, excerpt(out))
	out, failed, err = run("", files)
	add("returned tests fail on head if and only if a criterion FAILED", err == nil && failed == (r.Failed > 0),
		"err=%v failed=%v, report failed=%d: %s", err, failed, r.Failed, excerpt(out))

	for _, o := range e.Fixed {
		out, failed, err := run(o, files)
		add("returned tests pass with "+o, err == nil && !failed, "err=%v: %s", err, excerpt(out))
	}
	for _, m := range e.Mutants {
		out, failed, err := run(m.Overlay, nil)
		add("existing tests miss mutant "+m.Name, err == nil && !failed,
			"the scenario's mutant is caught without the returned tests, so it can't measure them: err=%v: %s", err, excerpt(out))
		out, failed, err = run(m.Overlay, files)
		add("returned tests catch mutant "+m.Name, err == nil && failed, "err=%v: %s", err, excerpt(out))
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
