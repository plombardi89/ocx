package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ocagents/internal/config"
	"ocagents/internal/gitx"
)

func hookSource(t *testing.T) string {
	t.Helper()
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, config.HookFile)
}

type repo struct {
	t      *testing.T
	work   string
	origin string
}

func (r repo) git(args ...string) string {
	r.t.Helper()
	out, err := gitx.Run(r.work, args...)
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func (r repo) push(args ...string) (string, error) {
	return gitx.Run(r.work, append([]string{"push"}, args...)...)
}

func (r repo) commit(file, content, message string) string {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.work, file), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
	sha, err := gitx.CommitAll(r.work, message)
	if err != nil {
		r.t.Fatal(err)
	}
	return sha
}

func (r repo) note(sha, firstLine string) {
	r.t.Helper()
	r.git("notes", "--ref=ocreview", "add", "-f", "-m", firstLine+"\n\nreport body", sha)
}

// newRepo returns a work repository with main pushed to a bare origin, the
// hook installed, and a feature branch with one commit checked out.
func newRepo(t *testing.T) repo {
	t.Helper()
	base := t.TempDir()
	r := repo{t: t, work: filepath.Join(base, "work"), origin: filepath.Join(base, "origin.git")}
	if _, err := gitx.Run(base, "init", "-q", "--bare", "-b", "main", r.origin); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gitx.Init(r.work); err != nil {
		t.Fatal(err)
	}
	r.commit("README", "base\n", "Initial commit")
	r.git("remote", "add", "origin", r.origin)
	if _, err := r.push("-q", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(hookSource(t), r.work); err != nil {
		t.Fatal(err)
	}
	r.git("switch", "-q", "-c", "feat/x")
	r.commit("feature.txt", "feature\n", "feat: add feature")
	return r
}

func approve(sha string) string {
	return "ocreview: verdict=APPROVE spec=PASS quality=PASS base=0000000 head=" + sha
}

func wantReject(t *testing.T, out string, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("push succeeded, want rejection containing %q\n%s", fragment, out)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("push rejected without %q:\n%v", fragment, err)
	}
}

func TestRejectsUnreviewedCommit(t *testing.T) {
	r := newRepo(t)
	out, err := r.push("origin", "feat/x")
	wantReject(t, out, err, "has no review")
}

func TestAllowsApprovedCommit(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(head))
	if out, err := r.push("-u", "origin", "feat/x"); err != nil {
		t.Fatalf("approved push rejected: %v\n%s", err, out)
	}
	remote, err := gitx.Run(r.origin, "rev-parse", "refs/heads/feat/x")
	if err != nil || remote != head {
		t.Fatalf("remote feat/x = %q (%v), want %s", remote, err, head)
	}
}

func TestRejectsRequestChanges(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	r.note(head, "ocreview: verdict=REQUEST_CHANGES spec=FAIL quality=FAIL base=0000000 head="+head)
	out, err := r.push("origin", "feat/x")
	wantReject(t, out, err, "reviewed but not approved")
}

func TestRejectsApprovalNamingAnotherCommit(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(strings.Repeat("a", 40)))
	out, err := r.push("origin", "feat/x")
	wantReject(t, out, err, "must approve this exact commit")
}

func TestRejectsCommitAddedAfterApproval(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(head))
	r.commit("feature.txt", "changed after review\n", "fix: tweak")
	out, err := r.push("origin", "feat/x")
	wantReject(t, out, err, "has no review")
}

func TestRejectsPushToMain(t *testing.T) {
	r := newRepo(t)
	r.git("switch", "-q", "main")
	head := r.commit("main.txt", "direct\n", "chore: direct to main")
	r.note(head, approve(head))
	out, err := r.push("origin", "main")
	wantReject(t, out, err, "pushing to main is not allowed")
}

func TestRejectsRefspecTargetingMainOrMaster(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(head))
	for _, target := range []string{"main", "master"} {
		out, err := r.push("origin", "feat/x:"+target)
		wantReject(t, out, err, "pushing to "+target+" is not allowed")
	}
}

func TestAllowsDeletingFeatureBranchAndPushingNotes(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(head))
	if _, err := r.push("origin", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if out, err := r.push("origin", "refs/notes/ocreview"); err != nil {
		t.Fatalf("notes push rejected: %v\n%s", err, out)
	}
	if out, err := r.push("origin", "--delete", "feat/x"); err != nil {
		t.Fatalf("branch delete rejected: %v\n%s", err, out)
	}
}

func TestRequiresApprovalForTaggedCommit(t *testing.T) {
	r := newRepo(t)
	r.git("tag", "-a", "v0.1.0", "-m", "release")
	out, err := r.push("origin", "v0.1.0")
	wantReject(t, out, err, "has no review")
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(head))
	if out, err := r.push("origin", "v0.1.0"); err != nil {
		t.Fatalf("approved tag push rejected: %v\n%s", err, out)
	}
}

func TestChainsExistingHook(t *testing.T) {
	r := newRepo(t)
	hooks := filepath.Join(r.git("rev-parse", "--absolute-git-dir"), "hooks")
	// Replace the managed hook with a foreign one, then reinstall.
	foreign := "#!/bin/sh\ncat > \"$(dirname \"$0\")/local-stdin\"\necho 'local hook says no' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(hookSource(t), r.work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(hooks, "pre-push.local")); err != nil {
		t.Fatalf("foreign hook not preserved: %v", err)
	}
	head := r.git("rev-parse", "HEAD")
	r.note(head, approve(head))
	out, err := r.push("origin", "feat/x")
	wantReject(t, out, err, "local hook says no")
	stdin, err := os.ReadFile(filepath.Join(hooks, "local-stdin"))
	if err != nil || !strings.Contains(string(stdin), head) {
		t.Fatalf("local hook did not receive push refs on stdin: %q (%v)", stdin, err)
	}
	// Reinstalling over a managed hook is a no-op for pre-push.local.
	if _, err := Install(hookSource(t), r.work); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
}
