package eval

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// isTestFile reports whether a repository path names a test file in one of
// the languages the evals cover.
func isTestFile(p string) bool {
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, ".py"):
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
	case strings.HasSuffix(base, ".sh"), strings.HasSuffix(base, ".bash"):
		return strings.HasPrefix(p, "tests/") || strings.Contains(p, "/tests/") || strings.Contains(base, "test")
	}
	return false
}

// lastTestsReport returns the last report the test-writer returned to
// build, if any.
func (a *asserter) lastTestsReport() (TestsReport, bool) {
	launches := a.dispatches("test-writer")
	for i := len(launches) - 1; i >= 0; i-- {
		out, _ := launches[i].State.Output.(string)
		if r, ok := ParseTestsReport(out); ok {
			return r, true
		}
	}
	return TestsReport{}, false
}

// commitChange is one file a commit changes, as git diff-tree reports it.
type commitChange struct{ Status, Path string }

func commitChanges(repo, sha string) []commitChange {
	var out []commitChange
	for _, line := range strings.Split(gitOut(repo, "diff-tree", "--no-commit-id", "-r", "--name-status", "--root", sha), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		out = append(out, commitChange{Status: fields[0][:1], Path: fields[len(fields)-1]})
	}
	return out
}

// result is a branch build produced: a pushed branch in origin, or, when
// nothing was pushed, the branch build left checked out in the work
// repository.
type result struct {
	Name, Repo, Ref string
}

func (a *asserter) results(pushed []string) []result {
	var out []result
	for _, b := range pushed {
		out = append(out, result{Name: b, Repo: a.ws.Origin, Ref: "refs/heads/" + b})
	}
	if len(out) == 0 && a.ws.Work != "" {
		name := gitOut(a.ws.Work, "branch", "--show-current")
		out = append(out, result{Name: name + " (not pushed)", Repo: a.ws.Work, Ref: "HEAD"})
	}
	return out
}

// commits lists the commits a result adds to main, oldest first.
func (a *asserter) commits(r result) []string {
	out := gitOut(r.Repo, "rev-list", "--reverse", a.ws.Base+".."+r.Ref)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// buildSide checks what build did with the test-writer's tests: in the
// branches it pushed, or, when it pushed nothing, in the branch it left
// checked out.
func (a *asserter) buildSide(e E2E, pushed []string) []Check {
	var checks []Check
	add := func(name string, ok bool, detail string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}
	resolve := func(o string) string {
		if filepath.IsAbs(o) {
			return o
		}
		return filepath.Join(a.dir, o)
	}

	if e.NoPush {
		add("nothing pushed", len(pushed) == 0, "pushed %v", pushed)
	}
	if e.ApprovedIfPushed {
		var unapproved []string
		for _, b := range pushed {
			sha := gitOut(a.ws.Origin, "rev-parse", "refs/heads/"+b)
			m := noteLine.FindStringSubmatch(firstLine(gitOut(a.ws.Work, "notes", "--ref=ocreview", "show", sha)))
			if m == nil || m[1] != "APPROVE" || m[2] != sha {
				unapproved = append(unapproved, b)
			}
		}
		add("anything pushed is approved", len(unapproved) == 0, "pushed without a matching approval: %v", unapproved)
	}
	results := a.results(pushed)

	var adopted []TestFile
	if e.AdoptedExactly || len(e.Buggy) > 0 {
		r, ok := a.lastTestsReport()
		seen := map[string]bool{}
		for _, f := range r.Files {
			if p, inside := cleanRepoPath(f.Path); inside && !seen[p] {
				seen[p] = true
				adopted = append(adopted, TestFile{Path: p, Content: f.Content})
			}
		}
		if !ok || len(adopted) == 0 {
			add("the last test-writer report has test files", false, "found report=%v with %d files", ok, len(adopted))
			return checks
		}
	}

	if e.AdoptedExactly {
		want := map[string]string{}
		for _, f := range adopted {
			want[f.Path] = f.Content
		}
		found, detail := false, "no commit adds exactly the returned files"
		for _, r := range results {
			for _, sha := range a.commits(r) {
				changes := commitChanges(r.Repo, sha)
				if len(changes) != len(want) {
					continue
				}
				same := true
				for _, c := range changes {
					content, ok := want[c.Path]
					blob, err := gitRaw(r.Repo, "cat-file", "blob", sha+":"+c.Path)
					if c.Status != "A" || !ok || err != nil || string(blob) != content {
						same = false
						break
					}
				}
				if same {
					found, detail = true, ""
				}
			}
		}
		add("adopted the returned test files byte for byte, in a commit of their own", found, "%s", detail)
	}

	if len(e.Buggy) > 0 {
		overlays := make([]string, len(e.Buggy))
		for i, o := range e.Buggy {
			overlays[i] = resolve(o)
		}
		for _, r := range results {
			b := r.Name
			sha := gitOut(r.Repo, "rev-parse", r.Ref)
			var files []TestFile
			var missing []string
			for _, f := range adopted {
				content, err := gitRaw(r.Repo, "cat-file", "blob", sha+":"+f.Path)
				if err != nil {
					missing = append(missing, f.Path)
					continue
				}
				files = append(files, TestFile{Path: f.Path, Content: string(content)})
			}
			if len(missing) > 0 {
				add(b+": adopted tests still there", false, "missing %v", missing)
				continue
			}
			// The final code must pass before the original bug's failure
			// means anything.
			final := runReturnedTestsAt(r.Repo, sha, files, nil, "")
			add(b+": adopted tests pass on the final code", final.Err == nil && !final.Failed && final.Ran > 0 && len(final.Broken) == 0,
				"err=%v failed=%v ran=%d broken=%q: %s", final.Err, final.Failed, final.Ran, final.Broken, truncate(strings.TrimSpace(final.Output), 600))
			out, failed, err := runNormal(r.Repo, sha, nil, e.NormalTest)
			add(b+": "+strings.Join(e.NormalTest, " ")+" passes on the final code", err == nil && !failed, "err=%v failed=%v: %s", err, failed, truncate(strings.TrimSpace(out), 600))
			run := runReturnedTestsAt(r.Repo, sha, files, overlays, "")
			add(b+": adopted tests still catch the original bug", run.Err == nil && run.Failed && run.Ran > 0 && len(run.Broken) == 0,
				"err=%v failed=%v ran=%d broken=%q: %s", run.Err, run.Failed, run.Ran, run.Broken, truncate(strings.TrimSpace(run.Output), 600))
			out, failed, err = runNormal(r.Repo, sha, overlays, e.NormalTest)
			add(b+": "+strings.Join(e.NormalTest, " ")+" catches the original bug", err == nil && failed, "err=%v failed=%v: %s", err, failed, truncate(strings.TrimSpace(out), 600))
		}
	}

	if len(e.RegistrationCommit) > 0 {
		found := false
		for _, r := range results {
			for _, sha := range a.commits(r) {
				changes := commitChanges(r.Repo, sha)
				only := len(changes) > 0
				for _, c := range changes {
					if !slices.Contains(e.RegistrationCommit, c.Path) {
						only = false
					}
				}
				found = found || only
			}
		}
		add(fmt.Sprintf("a commit that changes only %v", e.RegistrationCommit), found, "no commit changes only those files")
	}

	if len(e.TestProseForbid) > 0 {
		var hits []string
		for _, r := range results {
			for _, p := range strings.Split(gitOut(r.Repo, "diff", "--name-only", "--diff-filter=AM", a.ws.Base, r.Ref), "\n") {
				if p == "" || !isTestFile(p) {
					continue
				}
				content, err := gitShow(r.Repo, r.Ref, p)
				if err != nil {
					continue
				}
				for _, text := range testProse(TestFile{Path: p, Content: content}) {
					for _, f := range e.TestProseForbid {
						if regexp.MustCompile(f).MatchString(text) {
							hits = append(hits, fmt.Sprintf("%s: %q", p, strings.TrimSpace(text)))
						}
					}
				}
			}
		}
		sort.Strings(hits)
		add("test names and comments describe behavior", len(hits) == 0, "%s", strings.Join(hits, "; "))
	}

	for _, m := range e.PushedFileMentions {
		re := regexp.MustCompile(m.Pattern)
		ok := false
		for _, b := range pushed {
			if content, err := gitShow(a.ws.Origin, "refs/heads/"+b, m.Path); err == nil && re.MatchString(content) {
				ok = true
			}
		}
		add(fmt.Sprintf("pushed %s mentions %s", m.Path, m.Pattern), ok, "not found in any pushed branch")
	}
	return checks
}

// runNormal runs the project's normal test command on rev with the
// overlays laid over it.
func runNormal(src, rev string, overlays []string, command []string) (string, bool, error) {
	repo, cleanup, err := checkout(src, rev, overlays)
	defer cleanup()
	if err != nil {
		return "", false, err
	}
	return runCommand(repo, command[0], command[1:]...)
}
