package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
