package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionNamesTheBuild(t *testing.T) {
	setting := func(k, v string) debug.BuildSetting { return debug.BuildSetting{Key: k, Value: v} }
	sha := strings.Repeat("a1", 20)
	cases := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"a clean checkout", &debug.BuildInfo{Settings: []debug.BuildSetting{setting("vcs.revision", sha), setting("vcs.modified", "false")}}, true, sha},
		{"uncommitted changes", &debug.BuildInfo{Settings: []debug.BuildSetting{setting("vcs.revision", sha), setting("vcs.modified", "true")}}, true, sha + "-modified"},
		{"go install", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261115083908-f1e28a4b2c3d"}}, true, "v0.0.0-20261115083908-f1e28a4b2c3d"},
		{"no build information", nil, false, "unknown"},
	}
	for _, c := range cases {
		if got := version(c.info, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// A build in a git worktree, where Go records no commit, stamped by the
	// Makefile instead.
	commit = sha + "-modified"
	defer func() { commit = "" }()
	if got := version(&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true); got != sha+"-modified" {
		t.Errorf("a stamped worktree build: got %q", got)
	}
}

// TestTheBuiltBinaryWorksOutsideTheCheckout builds ocx the way make apply
// does and runs it from an unrelated directory, the way a downloaded binary
// runs.
func TestTheBuiltBinaryWorksOutsideTheCheckout(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ocx")
	build := exec.CommandContext(t.Context(), "make", "-s", "ocx", "OUT="+bin)
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make ocx: %v\n%s", err, out)
	}
	elsewhere := t.TempDir()
	ocx := func(t *testing.T, env []string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = elsewhere
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ocx %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	head, err := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.TrimSpace(ocx(t, nil, "version")); !regexp.MustCompile(`^` + strings.TrimSpace(string(head)) + `(-modified)?$`).MatchString(v) {
		t.Errorf("version = %q, want the checkout's commit", v)
	}
	installed := []string{"opencode.jsonc", "AGENTS.md", "prompts/reviewer.md", "skills/code-review/SKILL.md", "commands/plan.md", ".ocx-files"}
	cases := []struct {
		name string
		dir  func(home string) string
		env  func(home string) []string
		args []string
	}{
		{"--config-dir", func(home string) string { return filepath.Join(home, "cfg") }, func(string) []string { return nil }, []string{"--config-dir"}},
		{"XDG_CONFIG_HOME", func(home string) string { return filepath.Join(home, "xdg", "opencode") }, func(home string) []string { return []string{"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg")} }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			dir, env := c.dir(home), append(c.env(home), "HOME="+home)
			args := func(cmd string, extra ...string) []string {
				a := append([]string{cmd}, extra...)
				if c.args != nil {
					a = append(a, c.args[0], dir)
				}
				return a
			}
			if out := ocx(t, env, args("apply", "--dry-run")...); !strings.Contains(out, "Dry run") {
				t.Errorf("dry run output: %s", out)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the dry run created %s", dir)
			}
			ocx(t, env, args("apply")...)
			for _, p := range installed {
				fi, err := os.Lstat(filepath.Join(dir, p))
				if err != nil || !fi.Mode().IsRegular() {
					t.Errorf("%s: %v", p, err)
				}
			}
			ocx(t, env, args("remove")...)
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Errorf("left behind after remove: %v", entries)
			}
		})
	}
}

func TestOnlyThreeCommands(t *testing.T) {
	var out, errOut strings.Builder
	if code := run([]string{"check"}, &out, &errOut); code != 2 {
		t.Errorf("ocx check exited %d", code)
	}
	for _, removed := range []string{"check", "eval", "install", "uninstall", "doctor"} {
		if regexp.MustCompile(`(?m)^\s+ocx ` + removed + `\b`).MatchString(usage) {
			t.Errorf("usage still lists %s", removed)
		}
	}
}
