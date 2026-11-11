package eval

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	if err := gitx.Init(ws.Work); err != nil {
		return nil, err
	}
	base, err := gitx.CommitAll(ws.Work, "Initial commit")
	if err != nil {
		return nil, err
	}
	ws.Base, ws.Head = base, base
	if _, err := gitx.Run(dir, "init", "-q", "--bare", "-b", "main", ws.Origin); err != nil {
		return nil, err
	}
	steps := [][]string{
		{"remote", "add", "origin", ws.Origin},
		{"push", "-q", "-u", "origin", "main"},
	}
	for _, st := range steps {
		if _, err := gitx.Run(ws.Work, st...); err != nil {
			return nil, err
		}
	}
	if _, err := hook.Install(filepath.Join(root, config.HookFile), ws.Work); err != nil {
		return nil, err
	}
	if s.Branch != "" {
		if _, err := gitx.Run(ws.Work, "switch", "-q", "-c", s.Branch); err != nil {
			return nil, err
		}
		ws.Branch = s.Branch
		for i, c := range s.Commits {
			for _, d := range c.Dirs() {
				if err := stage.CopyTree(filepath.Join(s.Dir, d), ws.Work); err != nil {
					return nil, fmt.Errorf("commit %d: apply overlay %s: %w", i+1, d, err)
				}
			}
			sha, err := gitx.CommitAll(ws.Work, c.Message)
			if err != nil {
				return nil, err
			}
			ws.Head = sha
			ws.Commits = append(ws.Commits, sha)
			if c.Note != "" {
				if err := seedNote(ws, filepath.Join(s.Dir, c.Note), sha); err != nil {
					return nil, fmt.Errorf("commit %d: %w", i+1, err)
				}
			}
		}
	}
	if err := stage.Stage(root, ws.Work, opts); err != nil {
		return nil, err
	}
	return ws, nil
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

// Remove deletes the workspace.
func (ws *Workspace) Remove() error { return os.RemoveAll(ws.Dir) }
