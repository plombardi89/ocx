package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/gitx"
)

func TestRequestUsesTheTemplateOnly(t *testing.T) {
	o := &Outcome{Branch: "feat/x", Base: "aaa", Head: "bbb"}
	got := request("/tmp/w", o, Options{Round: 2, PreviousReview: "ccc", Plan: "docs/plans/0001-x.md"})
	want := "Review request\nRepository: /tmp/w\nBranch: feat/x\nBase: aaa\nHead: bbb\nPlan: docs/plans/0001-x.md\nRound: 2\nPrevious review: ccc"
	if got != want {
		t.Fatalf("request =\n%s\nwant\n%s", got, want)
	}
	if got := request("/tmp/w", o, Options{Round: 1}); !strings.Contains(got, "Plan: none\nRound: 1\nPrevious review: none") {
		t.Fatalf("defaults:\n%s", got)
	}
}

func TestRefusesWhenThereIsNothingToReview(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	repo := t.TempDir()
	if err := gitx.Init(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(repo, "Initial commit"); err != nil {
		t.Fatal(err)
	}
	_, err := Run(Options{Root: t.TempDir(), Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "nothing to review") {
		t.Fatalf("err = %v", err)
	}
}

// fakeReviewer stands in for opencode. Like the reviewer, it records what
// git status shows in its checkout, notes an approval on HEAD, and ends its
// report with a summary line. NOTE_HEAD and SUMMARY_HEAD change the commit
// the note and the summary name.
const fakeReviewer = `#!/bin/sh
git status --porcelain --untracked-files=all > "$REVIEWER_SAW"
head=$(git rev-parse HEAD)
git notes --ref=ocreview add -m "ocreview: verdict=APPROVE spec=NA quality=PASS base=x head=${NOTE_HEAD:-$head}" HEAD
printf '{"type":"text","sessionID":"ses_fake","part":{"messageID":"m1","text":"OCREVIEW verdict=APPROVE spec=NA quality=PASS blockers=0 majors=0 writing=0 minors=0 nits=0 head=%s"}}\n' "${SUMMARY_HEAD:-$head}"
`

// useFakeReviewer makes Run use fakeReviewer and returns the file where it
// records what it saw.
func useFakeReviewer(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("TMPDIR", t.TempDir())
	fake := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(fake, []byte(fakeReviewer), 0o755); err != nil {
		t.Fatal(err)
	}
	saw := filepath.Join(t.TempDir(), "reviewer-saw")
	t.Setenv("OCX_OPENCODE", fake)
	t.Setenv("REVIEWER_SAW", saw)
	return saw
}

// featureRepo returns a repository on a feature branch one commit ahead of
// main, and the sha of main.
func featureRepo(t *testing.T) (repo, base string) {
	t.Helper()
	repo = t.TempDir()
	if err := gitx.Init(repo); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "content")
	if err := os.WriteFile(file, []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := gitx.CommitAll(repo, "Initial commit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(repo, "switch", "-q", "-c", "feat/change"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(repo, "Change content"); err != nil {
		t.Fatal(err)
	}
	return repo, base
}

func TestReviewLeavesTheAuthorsCheckoutAsItWas(t *testing.T) {
	saw := useFakeReviewer(t)
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := featureRepo(t)
	// The author's own untracked project config must stay visible to them.
	if err := os.WriteFile(filepath.Join(repo, "opencode.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exclude := filepath.Join(repo, ".git", "info", "exclude")
	excludeBefore, _ := os.ReadFile(exclude)
	statusBefore, err := gitx.Run(repo, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		t.Fatal(err)
	}

	o, err := Run(Options{Root: root, Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !o.Approved() {
		t.Fatalf("not approved: %+v", o)
	}
	if seen, _ := os.ReadFile(saw); len(seen) != 0 {
		t.Errorf("the reviewer's checkout has untracked files:\n%s", seen)
	}

	if excludeAfter, _ := os.ReadFile(exclude); string(excludeAfter) != string(excludeBefore) {
		t.Errorf("info/exclude changed:\n%s", strings.TrimPrefix(string(excludeAfter), string(excludeBefore)))
	}
	if statusAfter, _ := gitx.Run(repo, "status", "--porcelain", "--untracked-files=all"); statusAfter != statusBefore {
		t.Errorf("git status changed from %q to %q", statusBefore, statusAfter)
	}
	if worktrees, _ := gitx.Run(repo, "worktree", "list", "--porcelain"); strings.Count(worktrees, "worktree ") != 1 {
		t.Errorf("review worktree left behind:\n%s", worktrees)
	}
	if note, _ := gitx.Run(repo, "notes", "--ref=ocreview", "show", o.Head); !strings.Contains(note, "head="+o.Head) {
		t.Errorf("note on %s not visible in the author's repository: %q", o.Head, note)
	}
}

func TestApprovalNamingAnotherCommitIsNotApproval(t *testing.T) {
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"NOTE_HEAD", "SUMMARY_HEAD"} {
		t.Run(which, func(t *testing.T) {
			useFakeReviewer(t)
			repo, base := featureRepo(t)
			t.Setenv(which, base)
			o, err := Run(Options{Root: root, Repo: repo})
			if err != nil {
				t.Fatal(err)
			}
			if o.Approved() {
				t.Fatalf("approved although %s names %s, not %s:\nsummary: %s\nnote: %s", which, base, o.Head, o.Summary, o.NoteFirstLine)
			}
		})
	}
}
