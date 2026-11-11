// Package review runs the independent reviewer on a repository's current
// commit without installing anything: it checks the commit out into a
// temporary worktree, stages the ocx configuration there, and runs the
// reviewer headlessly. Git notes are shared between worktrees, so the
// reviewer's verdict lands where the pre-push hook looks for it.
package review

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/plombardi89/ocx/internal/check"
	"github.com/plombardi89/ocx/internal/eval"
	"github.com/plombardi89/ocx/internal/stage"
)

// Options describe one review.
type Options struct {
	Root           string // the ocx repository
	Repo           string // the repository to review
	Plan           string // repository-relative plan file, or ""
	Round          int
	PreviousReview string // full sha of the previously reviewed head, or ""
	Model          string // override the pinned reviewer model, or ""
	Timeout        time.Duration
	Log            func(format string, args ...any)
}

// Outcome is the result of a review.
type Outcome struct {
	Branch, Base, Head string
	Verdict            string
	Summary            string // the report's summary line
	SummaryHead        string // the head the summary line names
	Report             string
	NoteFirstLine      string
	EventsPath         string
	Duration           time.Duration
}

// Approved reports whether the reviewer approved this exact commit, both in
// its report and in the note the pre-push hook checks.
func (o *Outcome) Approved() bool {
	return o.Head != "" &&
		o.Verdict == "APPROVE" && o.SummaryHead == o.Head &&
		strings.HasPrefix(o.NoteFirstLine, "ocreview: verdict=APPROVE ") && noteHead(o.NoteFirstLine) == o.Head
}

// noteHead returns the value of the last head= field in a note's first line,
// matching how the pre-push hook reads it.
func noteHead(line string) string {
	head := ""
	for _, f := range strings.Fields(line) {
		if v, ok := strings.CutPrefix(f, "head="); ok {
			head = v
		}
	}
	return head
}

// Run reviews the commit checked out in opts.Repo.
func Run(opts Options) (*Outcome, error) {
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	if opts.Round < 1 {
		opts.Round = 1
	}
	if opts.Timeout == 0 {
		opts.Timeout = 45 * time.Minute
	}
	top, err := git(opts.Repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", opts.Repo, err)
	}
	o := &Outcome{}
	if o.Head, err = git(top, "rev-parse", "HEAD"); err != nil {
		return nil, err
	}
	if o.Branch, _ = git(top, "branch", "--show-current"); o.Branch == "" {
		o.Branch = "(detached)"
	}
	for _, ref := range []string{"origin/main", "main", "master"} {
		if o.Base, err = git(top, "merge-base", "HEAD", ref); err == nil {
			break
		}
	}
	if o.Base == "" {
		return nil, errors.New("can't find a base: no origin/main, main, or master to compare against")
	}
	if o.Base == o.Head {
		return nil, fmt.Errorf("nothing to review: HEAD (%s) has no commits beyond its base", short(o.Head))
	}
	if dirty, _ := git(top, "status", "--porcelain", "--untracked-files=no"); dirty != "" {
		opts.Log("note: uncommitted changes are not part of the review; only commit %s is reviewed", short(o.Head))
	}
	if opts.Plan != "" {
		if _, err := os.Stat(filepath.Join(top, opts.Plan)); err != nil {
			return nil, fmt.Errorf("plan %s: %w", opts.Plan, err)
		}
	}

	dir := filepath.Join(check.ScratchDir(), "ocx-review", fmt.Sprintf("%s-%s-%s", filepath.Base(top), short(o.Head), randomSuffix()))
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	if _, err := git(top, "worktree", "add", "--detach", dir, o.Head); err != nil {
		return nil, err
	}
	defer func() {
		if _, err := git(top, "worktree", "remove", "--force", dir); err != nil {
			opts.Log("warning: couldn't remove %s: %v", dir, err)
		}
	}()
	if err := stage.Stage(opts.Root, dir, stage.Options{NonInteractive: true}); err != nil {
		return nil, err
	}

	o.EventsPath = dir + ".events.jsonl"
	prompt := request(dir, o, opts)
	opts.Log("reviewing %s (%s..%s) in %s", o.Branch, short(o.Base), short(o.Head), dir)
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	tr, runErr := eval.RunOpencode(ctx, dir, "reviewer", opts.Model, prompt, o.EventsPath, dir+".stderr.log")
	if tr != nil {
		o.Duration = tr.Duration
		o.Report = tr.FinalMessage()
	}
	if runErr != nil {
		return o, runErr
	}
	if sum, ok := eval.ParseSummary(o.Report); ok {
		o.Verdict, o.Summary, o.SummaryHead = sum.Verdict, sum.Line, sum.Head
	}
	note, _ := git(top, "notes", "--ref=ocreview", "show", o.Head)
	o.NoteFirstLine, _, _ = strings.Cut(note, "\n")
	return o, nil
}

func request(dir string, o *Outcome, opts Options) string {
	plan := opts.Plan
	if plan == "" {
		plan = "none"
	}
	previous := opts.PreviousReview
	if previous == "" {
		previous = "none"
	}
	return strings.Join([]string{
		"Review request",
		"Repository: " + dir,
		"Branch: " + o.Branch,
		"Base: " + o.Base,
		"Head: " + o.Head,
		"Plan: " + plan,
		"Round: " + strconv.Itoa(opts.Round),
		"Previous review: " + previous,
	}, "\n")
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = 'a' + b[i]%26
	}
	return string(b)
}
