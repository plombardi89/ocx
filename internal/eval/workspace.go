package eval

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/gitx"
	"github.com/plombardi89/ocx/internal/hook"
	"github.com/plombardi89/ocx/internal/stage"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// Workspace is a throwaway project for one run: a work repository with the
// review gate installed, and a bare origin it pushes to.
type Workspace struct {
	Dir    string
	Work   string
	Origin string
	Branch string
	// Base is main at setup; Head is HEAD after the scenario commits, and
	// Commits lists the scenario commits in order.
	Base, Head string
	Commits    []string
	// Before is the work repository's state just before the agent runs.
	Before repoState
	// stubs are directories outside Dir that Remove also deletes.
	stubs []string
}

// repoState is what a run must leave as it found it, unless the scenario
// expects changes: HEAD, the branch, git status, the content of every file
// that differs from HEAD, and the registered worktrees.
type repoState struct {
	Head, Branch, Status string
	Files                map[string]string
	Worktrees            []string
}

func captureState(work string) repoState {
	s := repoState{
		Head:   gitOut(work, "rev-parse", "HEAD"),
		Branch: gitOut(work, "branch", "--show-current"),
		Files:  map[string]string{},
	}
	// NUL-separated, so paths come through unquoted and nothing is trimmed.
	raw, err := gitRaw(work, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		s.Status = "error: " + err.Error()
	} else {
		s.Status = string(raw)
	}
	entries := strings.Split(string(raw), "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		code, p := e[:2], e[3:]
		if strings.ContainsAny(code, "RC") {
			i++ // the next entry is the path it was renamed or copied from
		}
		data, err := os.ReadFile(filepath.Join(work, p))
		switch {
		case errors.Is(err, os.ErrNotExist):
			s.Files[p] = "missing"
		case err != nil:
			s.Files[p] = "unreadable: " + err.Error()
		default:
			s.Files[p] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}
	for _, line := range strings.Split(gitOut(work, "worktree", "list", "--porcelain"), "\n") {
		if wt, ok := strings.CutPrefix(line, "worktree "); ok {
			s.Worktrees = append(s.Worktrees, wt)
		}
	}
	return s
}

// gitRaw runs an isolated git command and returns its output untouched.
func gitRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitx.Env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// changes describes how after differs from s, ignoring worktrees.
func (s repoState) changes(after repoState) []string {
	var out []string
	if after.Head != s.Head {
		out = append(out, fmt.Sprintf("HEAD moved from %s to %s", s.Head, after.Head))
	}
	if after.Branch != s.Branch {
		out = append(out, fmt.Sprintf("branch changed from %q to %q", s.Branch, after.Branch))
	}
	if after.Status != s.Status {
		out = append(out, fmt.Sprintf("git status changed from %q to %q", s.Status, after.Status))
	}
	for p, sum := range after.Files {
		if before, ok := s.Files[p]; ok && before != sum {
			out = append(out, p+" changed")
		}
	}
	sort.Strings(out)
	return out
}

// templateData is what scenario prompts and seeded notes can reference.
func (ws *Workspace) templateData() map[string]any {
	return map[string]any{
		"Repo":    ws.Work,
		"Branch":  ws.Branch,
		"Base":    ws.Base,
		"Head":    ws.Head,
		"Commits": ws.Commits,
	}
}

func renderTemplate(name, text string, data map[string]any) (string, error) {
	t, err := template.New(name).
		Option("missingkey=error").
		Funcs(template.FuncMap{"short": func(sha string) string {
			if len(sha) > 7 {
				return sha[:7]
			}
			return sha
		}}).
		Parse(text)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	err = t.Execute(&b, data)
	return b.String(), err
}

// Prepare builds the workspace for scenario s in dir.
func Prepare(root string, s Scenario, dir string, opts stage.Options) (*Workspace, error) {
	ws := &Workspace{
		Dir:    dir,
		Work:   filepath.Join(dir, "work"),
		Origin: filepath.Join(dir, "origin.git"),
		Branch: "main",
	}
	if err := stage.CopyTree(filepath.Join(root, config.FixturesDir, s.Fixture), ws.Work); err != nil {
		return nil, fmt.Errorf("copy fixture: %w", err)
	}
	for _, d := range s.BaseOverlays {
		if err := stage.CopyTree(filepath.Join(s.Dir, d), ws.Work); err != nil {
			return nil, fmt.Errorf("apply base overlay %s: %w", d, err)
		}
	}
	if err := gitx.Init(ws.Work); err != nil {
		return nil, err
	}
	base, err := gitx.CommitAll(ws.Work, "Initial commit")
	if err != nil {
		return nil, err
	}
	ws.Base, ws.Head = base, base

	// Commits on main land before main is pushed, so origin matches main and
	// Base is main's final state.
	if s.Branch == "main" {
		if err := applyCommits(ws, s); err != nil {
			return nil, err
		}
		ws.Base = ws.Head
	}
	if _, err := gitx.Run(dir, "init", "-q", "--bare", "-b", "main", ws.Origin); err != nil {
		return nil, err
	}
	for _, st := range [][]string{
		{"remote", "add", "origin", ws.Origin},
		{"push", "-q", "-u", "origin", "main"},
	} {
		if _, err := gitx.Run(ws.Work, st...); err != nil {
			return nil, err
		}
	}
	if _, err := hook.Install(filepath.Join(root, config.HookFile), ws.Work); err != nil {
		return nil, err
	}
	if s.Branch != "" && s.Branch != "main" {
		if _, err := gitx.Run(ws.Work, "switch", "-q", "-c", s.Branch); err != nil {
			return nil, err
		}
		ws.Branch = s.Branch
		if err := applyCommits(ws, s); err != nil {
			return nil, err
		}
	}
	if s.Uncommitted != "" {
		if err := stage.CopyTree(filepath.Join(s.Dir, s.Uncommitted), ws.Work); err != nil {
			return nil, fmt.Errorf("apply uncommitted overlay: %w", err)
		}
	}
	if err := stage.Stage(root, ws.Work, opts); err != nil {
		return nil, err
	}
	agents := make([]string, 0, len(s.StubAgents))
	for a := range s.StubAgents {
		agents = append(agents, a)
	}
	sort.Strings(agents)
	for _, agent := range agents {
		var reports []string
		for _, r := range s.StubAgents[agent] {
			reports = append(reports, filepath.Join(s.Dir, r))
		}
		if err := ws.stub(agent, reports); err != nil {
			return nil, fmt.Errorf("stub %s: %w", agent, err)
		}
	}
	ws.Before = captureState(ws.Work)
	return ws, nil
}

// stubPrompt replaces a stubbed agent's prompt. The agent runs a script in
// its own scratch root, which is the one place its permissions let it run
// scripts, and returns what the script prints.
const stubPrompt = `You are standing in for this agent while the agent that launched you is being tested. Do nothing else: run exactly this command, then make its output, unchanged, your entire final message.

bash %s
`

// stubScript prints the next report on each run and repeats the last one.
const stubScript = `#!/usr/bin/env bash
set -euo pipefail
dir=%q
n=$(cat "$dir/count" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" >"$dir/count"
if [ "$n" -gt %d ]; then n=%d; fi
head=$(git rev-parse HEAD 2>/dev/null || echo unknown)
sed "s/{{HEAD}}/$head/g" "$dir/report-$n.md"
`

// stub makes agent return the reports, one per launch, by replacing its
// staged prompt.
func (ws *Workspace) stub(agent string, reports []string) error {
	root, ok := config.ScratchRoots[agent]
	if !ok {
		return fmt.Errorf("%s has no scratch root", agent)
	}
	prompt := filepath.Join(ws.Work, ".opencode", "prompts", agent+".md")
	if _, err := os.Stat(prompt); err != nil {
		return err
	}
	dir := filepath.Join(root, "stub-"+filepath.Base(ws.Dir))
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ws.stubs = append(ws.stubs, dir)
	for i, r := range reports {
		if err := stage.CopyTree(r, filepath.Join(dir, fmt.Sprintf("report-%d.md", i+1))); err != nil {
			return err
		}
	}
	script := filepath.Join(dir, "next.sh")
	if err := os.WriteFile(script, []byte(fmt.Sprintf(stubScript, dir, len(reports), len(reports))), 0o755); err != nil {
		return err
	}
	return os.WriteFile(prompt, []byte(fmt.Sprintf(stubPrompt, script)), 0o644)
}

// applyCommits makes the scenario's commits on the current branch.
func applyCommits(ws *Workspace, s Scenario) error {
	for i, c := range s.Commits {
		for _, d := range c.Dirs() {
			if err := stage.CopyTree(filepath.Join(s.Dir, d), ws.Work); err != nil {
				return fmt.Errorf("commit %d: apply overlay %s: %w", i+1, d, err)
			}
		}
		sha, err := gitx.CommitAll(ws.Work, c.Message)
		if err != nil {
			return err
		}
		ws.Head = sha
		ws.Commits = append(ws.Commits, sha)
		if c.Note != "" {
			if err := seedNote(ws, filepath.Join(s.Dir, c.Note), sha); err != nil {
				return fmt.Errorf("commit %d: %w", i+1, err)
			}
		}
	}
	return nil
}

// seedNote attaches a rendered review note to sha, as if an earlier review
// round had recorded it.
func seedNote(ws *Workspace, path, sha string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data := ws.templateData()
	data["Head"] = sha
	note, err := renderTemplate(filepath.Base(path), string(text), data)
	if err != nil {
		return fmt.Errorf("render note: %w", err)
	}
	_, err = gitx.RunInput(ws.Work, note, "notes", "--ref=ocreview", "add", "-f", "-F", "-", sha)
	return err
}

// Remove deletes the workspace and its stub directories.
func (ws *Workspace) Remove() error {
	for _, d := range ws.stubs {
		_ = os.RemoveAll(d)
	}
	return os.RemoveAll(ws.Dir)
}
