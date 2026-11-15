package ocx

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// nestedRunEnv is set in the copies of the repository that these tests run
// make in, so the tests that start make don't start it again from there.
const nestedRunEnv = "OCX_NESTED_MAKE_RUN"

func skipInNestedRun(t *testing.T) {
	t.Helper()
	if os.Getenv(nestedRunEnv) != "" {
		t.Skip("already running inside a copy made by another test")
	}
}

func requireMake(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make isn't installed")
	}
}

// makeEnv returns the environment for a make run: the current one without
// the variables a parent make or the check targets set, plus extra.
func makeEnv(extra ...string) []string {
	drop := []string{"MAKEFLAGS=", "MFLAGS=", "MAKELEVEL=", "MAKEOVERRIDES=", "OCX_TEST_OPENCODE=", "GNUMAKEFLAGS="}
	var env []string
next:
	for _, kv := range os.Environ() {
		for _, d := range drop {
			if strings.HasPrefix(kv, d) {
				continue next
			}
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

type commandResult struct {
	out  string
	code int
}

func runIn(t *testing.T, dir string, env []string, timeout time.Duration, name string, args ...string) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	res := commandResult{out: string(out)}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.code = exit.ExitCode()
	default:
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return res
}

func dryRunMake(t *testing.T, target string, args ...string) commandResult {
	t.Helper()
	requireMake(t)
	return runIn(t, ".", makeEnv(), 2*time.Minute, "make", append([]string{"-n", target}, args...)...)
}

func TestMakefileKeepsTheCheckAndEvalTargets(t *testing.T) {
	for _, target := range []string{"test", "check", "check-offline", "eval", "eval-reviewer", "eval-critic", "eval-debug", "eval-tests", "eval-e2e", "bakeoff", "verify", "apply"} {
		t.Run(target, func(t *testing.T) {
			r := dryRunMake(t, target)
			if r.code != 0 {
				t.Errorf("make -n %s exited %d:\n%s", target, r.code, r.out)
			}
			if strings.TrimSpace(r.out) == "" || strings.Contains(r.out, "Nothing to be done") {
				t.Errorf("make -n %s has nothing to run:\n%s", target, r.out)
			}
		})
	}
}

func TestMakefileNoLongerHasTheInstallTargets(t *testing.T) {
	for _, target := range []string{"install", "uninstall", "doctor"} {
		t.Run(target, func(t *testing.T) {
			r := dryRunMake(t, target)
			noRule := r.code != 0 && strings.Contains(r.out, "No rule to make target")
			noRecipe := r.code == 0 && strings.Contains(r.out, "Nothing to be done")
			if !noRule && !noRecipe {
				t.Errorf("make -n %s still has something to run (exit %d):\n%s", target, r.code, r.out)
			}
		})
	}
}

func TestEvalTargetsDoNotRunThroughTheOcxBinary(t *testing.T) {
	for _, target := range []string{"eval", "eval-reviewer", "eval-critic", "eval-debug", "eval-tests", "eval-e2e", "bakeoff"} {
		t.Run(target, func(t *testing.T) {
			r := dryRunMake(t, target)
			if r.code != 0 {
				t.Fatalf("make -n %s exited %d:\n%s", target, r.code, r.out)
			}
			if strings.Contains(r.out, "cmd/ocx") || regexp.MustCompile(`\bocx\s+eval\b`).MatchString(r.out) {
				t.Errorf("make %s still runs the ocx binary:\n%s", target, r.out)
			}
		})
	}
}

// pinnedGoEnv keeps the Go caches where they are while a test points HOME
// at an empty directory.
func pinnedGoEnv(t *testing.T) []string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOCACHE", "GOMODCACHE", "GOPATH").Output()
	if err != nil {
		t.Fatal(err)
	}
	vars := []string{"GOCACHE=", "GOMODCACHE=", "GOPATH="}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(vars) {
		t.Fatalf("go env printed %q", out)
	}
	var env []string
	for i, v := range vars {
		env = append(env, v+lines[i])
	}
	return env
}

// isolatedMakeEnv gives make an empty home directory and temporary
// directory and no XDG_CONFIG_HOME, so ocx apply can't reach the real
// configuration. It returns the home directory too.
func isolatedMakeEnv(t *testing.T) ([]string, string) {
	t.Helper()
	pins := pinnedGoEnv(t)
	home, tmp := t.TempDir(), t.TempDir()
	env := makeEnv()
	var kept []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			continue
		}
		kept = append(kept, kv)
	}
	kept = append(kept, pins...)
	return append(kept, "HOME="+home, "TMPDIR="+tmp), home
}

func TestMakeEvalListsEveryScenario(t *testing.T) {
	requireMake(t)
	env, _ := isolatedMakeEnv(t)
	r := runIn(t, ".", env, 5*time.Minute, "make", "eval", "ARGS=--list")
	if r.code != 0 {
		t.Fatalf("make eval ARGS=--list exited %d:\n%s", r.code, r.out)
	}
	entries, err := os.ReadDir(filepath.Join("evals", "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	listed := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		listed++
		if !strings.Contains(r.out, e.Name()) {
			t.Errorf("the list doesn't include the scenario %s", e.Name())
		}
	}
	if listed == 0 {
		t.Fatal("found no scenarios to compare with")
	}
}

func TestMakeApplyInstallsIntoTheDirectoryGivenInARGS(t *testing.T) {
	requireMake(t)
	env, home := isolatedMakeEnv(t)
	cfg := filepath.Join(t.TempDir(), "opencode")

	r := runIn(t, ".", env, 5*time.Minute, "make", "apply", "ARGS=--config-dir "+cfg)
	if r.code != 0 {
		t.Fatalf("make apply exited %d:\n%s", r.code, r.out)
	}
	for _, rel := range []string{"opencode.jsonc", "AGENTS.md", ".ocx-files"} {
		if _, err := os.Lstat(filepath.Join(cfg, rel)); err != nil {
			t.Errorf("make apply didn't write %s into the directory in ARGS: %v", rel, err)
		}
	}
	want, err := os.ReadFile(filepath.Join("config", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(cfg, "opencode.jsonc")); string(got) != string(want) {
		t.Error("the installed opencode.jsonc differs from this checkout's")
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "opencode")); err == nil {
		t.Error("make apply also wrote into the default directory, so ARGS wasn't the only place it went")
	}
}

func TestMakeApplyPassesDryRunThroughAndWritesNothing(t *testing.T) {
	requireMake(t)
	env, home := isolatedMakeEnv(t)
	cfg := filepath.Join(t.TempDir(), "opencode")

	r := runIn(t, ".", env, 5*time.Minute, "make", "apply", "ARGS=--dry-run --config-dir "+cfg)
	if r.code != 0 {
		t.Fatalf("make apply --dry-run exited %d:\n%s", r.code, r.out)
	}
	if _, err := os.Lstat(cfg); err == nil {
		t.Error("make apply with --dry-run created the directory")
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "opencode")); err == nil {
		t.Error("make apply with --dry-run wrote into the default directory")
	}
	if !strings.Contains(r.out, "opencode.jsonc") {
		t.Errorf("make apply with --dry-run didn't print the files it would write:\n%s", r.out)
	}
}

func TestMakeApplyWithoutARGSUsesTheDefaultDirectory(t *testing.T) {
	requireMake(t)
	env, home := isolatedMakeEnv(t)
	r := runIn(t, ".", env, 5*time.Minute, "make", "apply")
	if r.code != 0 {
		t.Fatalf("make apply exited %d:\n%s", r.code, r.out)
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "opencode", "opencode.jsonc")); err != nil {
		t.Errorf("make apply didn't install into ~/.config/opencode: %v", err)
	}
}

// copyCheckoutForMake copies the checkout, without git data and eval
// results, so a test can break the copy and run make in it.
func copyCheckoutForMake(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(".")
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

func TestMakeTestFailsWhenASkillHasNoName(t *testing.T) {
	skipInNestedRun(t)
	requireMake(t)
	copyDir := copyCheckoutForMake(t)
	env := makeEnv(nestedRunEnv + "=1")

	if r := runIn(t, copyDir, env, 10*time.Minute, "go", "test", "-count=1", "./internal/check/"); r.code != 0 {
		t.Fatalf("the checks fail on an unmodified copy:\n%s", r.out)
	}

	skill := filepath.Join(copyDir, "skills", "writing-style", "SKILL.md")
	data, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	nameLine := regexp.MustCompile(`(?m)^name:[^\n]*\n`)
	if !nameLine.Match(data) {
		t.Fatal("the skill has no name line to remove")
	}
	if err := os.WriteFile(skill, nameLine.ReplaceAll(data, nil), 0o644); err != nil {
		t.Fatal(err)
	}

	r := runIn(t, copyDir, env, 10*time.Minute, "make", "test")
	if r.code == 0 {
		t.Fatalf("make test passed with a skill that has no name:\n%s", r.out)
	}
	if !strings.Contains(r.out, "writing-style") || !strings.Contains(r.out, "name") {
		t.Errorf("make test failed, but not about the skill's missing name:\n%s", r.out)
	}
}

func TestMakeCheckRunsThePermissionProbesAgainstOpenCode(t *testing.T) {
	skipInNestedRun(t)
	if os.Getenv("OCX_TEST_OPENCODE") != "1" {
		t.Skip("runs OpenCode, so it only runs under make check")
	}
	requireMake(t)
	copyDir := copyCheckoutForMake(t)

	probes := filepath.Join(copyDir, "tests", "permissions.yaml")
	data, err := os.ReadFile(probes)
	if err != nil {
		t.Fatal(err)
	}
	probe := regexp.MustCompile(`(?m)^(\s*- \{[^\n]*resource: "?([^",}]+)"?[^\n]*expect: )allow( \}.*)$`)
	m := probe.FindSubmatch(data)
	if m == nil {
		t.Fatal("found no probe that expects allow")
	}
	loc := probe.FindIndex(data)
	line := probe.ReplaceAll(data[loc[0]:loc[1]], []byte("${1}deny${3}"))
	flipped := append(append(append([]byte{}, data[:loc[0]]...), line...), data[loc[1]:]...)
	if err := os.WriteFile(probes, flipped, 0o644); err != nil {
		t.Fatal(err)
	}

	r := runIn(t, copyDir, makeEnv(nestedRunEnv+"=1"), 15*time.Minute, "make", "check")
	if r.code == 0 {
		t.Fatalf("make check passed although a permission probe expects the wrong decision:\n%s", r.out)
	}
	if !strings.Contains(r.out, string(m[2])) {
		t.Errorf("make check failed, but its output doesn't name the probe on %s:\n%s", m[2], r.out)
	}
}
