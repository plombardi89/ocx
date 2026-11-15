package main

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// copyRepository copies the checkout, without its git data and eval
// results, into a new temporary directory and returns that directory.
func copyRepository(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "checkout")
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == ".git" || rel == filepath.Join("evals", "results") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer func() { _ = in.Close() }()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				_ = out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// buildOcxIn builds the ocx command from the checkout at root into a new
// directory outside it and returns the binary's path.
func buildOcxIn(t *testing.T, root string, buildvcs bool) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ocx")
	vcsFlag := "-buildvcs=false"
	if buildvcs {
		vcsFlag = "-buildvcs=true"
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", vcsFlag, "-o", bin, "./cmd/ocx")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build in %s: %v\n%s", root, err, out)
	}
	return bin
}

func TestBinaryCarriesTheFilesItWasBuiltWith(t *testing.T) {
	checkout := copyRepository(t)
	built := shippedFilesIn(t, checkout)
	bin := buildOcxIn(t, checkout, false)

	// Changes after the build must not reach what the binary installs.
	writeText(t, filepath.Join(checkout, "config", "opencode.jsonc"), "{ \"changed\": \"after the build\" }\n")
	writeText(t, filepath.Join(checkout, "skills", "added-after-build", "SKILL.md"), "added\n")
	commandFile := shippedPathWithPrefix(t, built, "commands/")
	if err := os.Remove(filepath.Join(checkout, filepath.FromSlash(commandFile))); err != nil {
		t.Fatal(err)
	}

	s := newSandbox(t)
	s.bin = bin
	cfg := newConfigDir(t)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	for rel, want := range built {
		if got := readText(t, filepath.Join(cfg, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s differs from the file the binary was built with", rel)
		}
	}
	if pathExists(filepath.Join(cfg, "skills", "added-after-build")) {
		t.Error("a skill added after the build was installed")
	}
}

func TestVersionNamesTheCommitAndMarksUncommittedChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	checkout := copyRepository(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = checkout
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "-A")
	git("-c", "commit.gpgsign=false", "commit", "-q", "-m", "snapshot")
	commit := git("rev-parse", "HEAD")

	s := newSandbox(t)
	s.bin = buildOcxIn(t, checkout, true)
	if got := strings.TrimSpace(s.run("version").Stdout); got != commit {
		t.Errorf("a clean build printed %q, want the commit %q", got, commit)
	}

	writeText(t, filepath.Join(checkout, "README.md"), "changed after the commit\n")
	s.bin = buildOcxIn(t, checkout, true)
	if got := strings.TrimSpace(s.run("version").Stdout); got != commit+"-modified" {
		t.Errorf("a build with uncommitted changes printed %q, want %q", got, commit+"-modified")
	}
}

func TestVersionNamesTheModuleVersionForABuildWithoutACheckout(t *testing.T) {
	for _, v := range []string{"v1.4.2", "v0.0.0-20261115083908-f1e28a4b2c3d"} {
		info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/plombardi89/ocx", Version: v}}
		if got := version(info, true); got != v {
			t.Errorf("version = %q, want the module version %q", got, v)
		}
	}
}
