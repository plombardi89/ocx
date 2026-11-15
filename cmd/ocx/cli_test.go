package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

var (
	builtOnce sync.Once
	builtDir  string
	builtPath string
	errBuild  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if builtDir != "" {
		_ = os.RemoveAll(builtDir)
	}
	os.Exit(code)
}

// ocxBinary builds the ocx command once per test run and returns its path.
func ocxBinary(t *testing.T) string {
	t.Helper()
	builtOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ocx-cli-") //nolint:usetesting // Built once and shared by every test, so it outlives any one test's TempDir.
		if err != nil {
			errBuild = err
			return
		}
		builtDir = dir
		path := filepath.Join(dir, "ocx")
		if out, err := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", path, ".").CombinedOutput(); err != nil {
			errBuild = fmt.Errorf("go build: %w\n%s", err, out)
			return
		}
		builtPath = path
	})
	if errBuild != nil {
		t.Fatal(errBuild)
	}
	return builtPath
}

type cliResult struct {
	Stdout string
	Stderr string
	Code   int
}

func (r cliResult) combined() string { return r.Stdout + r.Stderr }

// shown is the command output, shortened for failure messages.
func (r cliResult) shown() string { return clip(r.combined()) }

// clip shortens long command output for failure messages.
func clip(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// sandbox runs the built ocx binary from a directory outside the checkout,
// with HOME pointing at an empty directory and no XDG_CONFIG_HOME, so a
// command can never touch the real OpenCode configuration.
type sandbox struct {
	t    *testing.T
	home string
	env  []string
	bin  string
}

func newSandbox(t *testing.T, extraEnv ...string) *sandbox {
	t.Helper()
	home := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home)
	env = append(env, extraEnv...)
	return &sandbox{t: t, home: home, env: env}
}

func (s *sandbox) run(args ...string) cliResult {
	s.t.Helper()
	bin := s.bin
	if bin == "" {
		bin = ocxBinary(s.t)
	}
	cmd := exec.CommandContext(s.t.Context(), bin, args...)
	cmd.Dir = s.t.TempDir()
	cmd.Env = s.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := cliResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.Code = exit.ExitCode()
	default:
		s.t.Fatalf("running ocx %v: %v", args, err)
	}
	return res
}

func (s *sandbox) apply(cfg string, flags ...string) cliResult {
	s.t.Helper()
	return s.run(append([]string{"apply", "--config-dir", cfg}, flags...)...)
}

func (s *sandbox) remove(cfg string) cliResult {
	s.t.Helper()
	return s.run("remove", "--config-dir", cfg)
}

// install runs apply and fails the test if it doesn't succeed.
func (s *sandbox) install(cfg string) {
	s.t.Helper()
	if r := s.apply(cfg); r.Code != 0 {
		s.t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
}

func newConfigDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "opencode")
}

func writeText(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func makeLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func removeTree(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

// treeState describes everything under dir: file contents, directories
// (with a trailing slash), and links with their targets. A missing dir
// gives a single "<missing>" entry, so a test can tell it wasn't created.
func treeState(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			state[rel] = "link to " + target
		case d.IsDir():
			state[rel+"/"] = ""
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			state[rel] = string(data)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{"<missing>": ""}
	}
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requireUnchanged(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	var diffs []string
	for k, v := range before {
		if got, ok := after[k]; !ok {
			diffs = append(diffs, "removed "+k)
		} else if got != v {
			diffs = append(diffs, "changed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			diffs = append(diffs, "added "+k)
		}
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		more := ""
		if len(diffs) > 6 {
			more = fmt.Sprintf(" and %d more", len(diffs)-6)
			diffs = diffs[:6]
		}
		t.Errorf("%s changed: %s%s", what, strings.Join(diffs, ", "), more)
	}
}

// shippedFilesIn reads the configuration files of the checkout at root, keyed
// by the path they are installed at inside OpenCode's configuration directory.
func shippedFilesIn(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{
		"opencode.jsonc": readText(t, filepath.Join(root, "config", "opencode.jsonc")),
		"AGENTS.md":      readText(t, filepath.Join(root, "config", "global-AGENTS.md")),
	}
	trees := [][2]string{{"config/prompts", "prompts"}, {"skills", "skills"}, {"commands", "commands"}}
	for _, tree := range trees {
		base := filepath.Join(root, filepath.FromSlash(tree[0]))
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(base, p)
			files[tree[1]+"/"+filepath.ToSlash(rel)] = readText(t, p)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func shippedFiles(t *testing.T) map[string]string {
	t.Helper()
	return shippedFilesIn(t, filepath.Join("..", ".."))
}

// shippedPathWithPrefix returns the first shipped path that starts with prefix.
func shippedPathWithPrefix(t *testing.T, shipped map[string]string, prefix string) string {
	t.Helper()
	var paths []string
	for p := range shipped {
		if strings.HasPrefix(p, prefix) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		t.Fatalf("no shipped file under %s", prefix)
	}
	sort.Strings(paths)
	return paths[0]
}

func listedPaths(t *testing.T, cfg string) []string {
	t.Helper()
	var out []string
	for line := range strings.SplitSeq(readText(t, filepath.Join(cfg, ".ocx-files")), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func setList(t *testing.T, cfg string, entries ...string) {
	t.Helper()
	writeText(t, filepath.Join(cfg, ".ocx-files"), strings.Join(entries, "\n")+"\n")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
