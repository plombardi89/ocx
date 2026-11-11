// Package doctor checks that ocx is installed and working: the global links,
// what OpenCode actually loads, the tools agents rely on, and, for each
// project repository, the review gate and branch protection.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/plombardi89/ocx/internal/check"
	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/hook"
	"github.com/plombardi89/ocx/internal/install"
)

// Options select what doctor checks.
type Options struct {
	Root      string   // the ocx repository
	ConfigDir string   // OpenCode's global configuration directory
	Repos     []string // project repositories to check
	Offline   bool     // skip checks that run OpenCode or call GitHub
}

// Run performs every check.
func Run(opts Options) []check.Result {
	installed := checkInstall(opts.Root, opts.ConfigDir)
	results := []check.Result{installed}
	if opts.Offline {
		results = append(results, check.Result{Name: "OpenCode loads the install", Status: check.Skip, Details: []string{"offline"}})
	} else if installed.Status == check.Fail {
		results = append(results, check.Result{Name: "OpenCode loads the install", Status: check.Skip, Details: []string{"fix the install first"}})
	} else {
		results = append(results, checkLoaded(opts))
	}
	results = append(results, checkTools(opts.Offline)...)
	for _, repo := range opts.Repos {
		results = append(results, checkRepo(opts, repo)...)
	}
	return results
}

func result(name string, problems []string) check.Result {
	if len(problems) == 0 {
		return check.Result{Name: name, Status: check.Pass}
	}
	return check.Result{Name: name, Status: check.Fail, Details: problems}
}

func warn(name string, details ...string) check.Result {
	return check.Result{Name: name, Status: check.Warn, Details: details}
}

func checkInstall(root, dir string) check.Result {
	const name = "install"
	items, err := install.Desired(root)
	if err != nil {
		return result(name, []string{err.Error()})
	}
	m, err := install.ReadManifest(dir)
	if err != nil {
		return result(name, []string{err.Error()})
	}
	if m == nil {
		return result(name, []string{"ocx is not installed in " + dir, "run: make install APPLY=1"})
	}
	var problems []string
	if m.Root != root {
		problems = append(problems, fmt.Sprintf("installed from %s, not this repository (%s)", m.Root, root))
	}
	for _, it := range items {
		got, err := os.Readlink(filepath.Join(dir, it.Path))
		switch {
		case err != nil:
			problems = append(problems, it.Path+" is missing or not a link")
		case got != it.Target:
			problems = append(problems, fmt.Sprintf("%s points to %s, want %s", it.Path, got, it.Target))
		}
	}
	if len(problems) > 0 {
		problems = append(problems, "run: make install APPLY=1")
		return result(name, problems)
	}
	return check.Result{Name: name, Status: check.Pass, Details: []string{fmt.Sprintf("%d links in %s", len(items), dir)}}
}

// checkLoaded asks OpenCode, from an empty project, which agents it loads,
// and compares them and their permissions with this repository.
func checkLoaded(opts Options) check.Result {
	const name = "OpenCode loads the install"
	dir, err := os.MkdirTemp(check.ScratchDir(), "ocx-doctor-")
	if err != nil {
		return result(name, []string{err.Error()})
	}
	defer os.RemoveAll(dir)
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		return result(name, []string{strings.TrimSpace(string(out))})
	}
	var env []string
	if def, err := install.ConfigDir(); err == nil && filepath.Clean(def) != filepath.Clean(opts.ConfigDir) {
		if filepath.Base(opts.ConfigDir) != "opencode" {
			return warn(name, "can only check a configuration directory named opencode")
		}
		env = append(env, "XDG_CONFIG_HOME="+filepath.Dir(opts.ConfigDir))
	}
	agents, err := check.ResolveIn(dir, env)
	if err != nil {
		return result(name, []string{err.Error()})
	}
	cfgPath := filepath.Join(opts.Root, config.ConfigFile)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return result(name, []string{err.Error()})
	}
	return check.CompareResolved(name, opts.Root, cfgPath, cfg, agents)
}

func checkTools(offline bool) []check.Result {
	var out []check.Result
	if _, err := exec.LookPath("gh"); err != nil {
		out = append(out, warn("gh", "not installed; doctor can't check GitHub branch protection"))
	} else if offline {
		// gh auth status checks the stored credentials with GitHub.
		out = append(out, check.Result{Name: "gh", Status: check.Skip, Details: []string{"installed; login not checked offline"}})
	} else if err := run(10*time.Second, "", "gh", "auth", "status"); err != nil {
		out = append(out, warn("gh", "not logged in; run: gh auth login"))
	} else {
		out = append(out, check.Result{Name: "gh", Status: check.Pass})
	}
	if _, err := exec.LookPath("gcc"); err != nil {
		out = append(out, warn("gcc", "not installed; reviewers can't run go test -race"))
	} else {
		out = append(out, check.Result{Name: "gcc", Status: check.Pass})
	}
	if _, err := exec.LookPath("shellcheck"); err != nil {
		out = append(out, warn("shellcheck", "not installed; reviewers can only syntax-check shell scripts"))
	} else {
		out = append(out, check.Result{Name: "shellcheck", Status: check.Pass})
	}
	out = append(out, checkCredentialHelper(""))
	return out
}

// checkCredentialHelper finds git credential helpers that can't run here,
// such as a Windows program configured inside WSL.
func checkCredentialHelper(repo string) check.Result {
	const name = "git credential helper"
	args := []string{"config", "--get-all", "credential.helper"}
	if repo != "" {
		args = append([]string{"-C", repo}, args...)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return check.Result{Name: name, Status: check.Pass, Details: []string{"none configured"}}
	}
	var problems []string
	for _, helper := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		helper = strings.TrimSpace(helper)
		if helper == "" || strings.HasPrefix(helper, "!") {
			continue
		}
		program := helperProgram(helper)
		if !strings.Contains(program, "/") {
			path, err := findHelper("git-credential-" + program)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%q: git-credential-%s is not installed", helper, program))
				continue
			}
			program = path
		}
		if err := starts(program); err != nil {
			problems = append(problems, fmt.Sprintf("%q can't run here: %v", program, err))
		}
	}
	if len(problems) > 0 {
		return warn(name, append(problems, "pushes over HTTPS will fail; run: gh auth setup-git")...)
	}
	return check.Result{Name: name, Status: check.Pass}
}

// helperProgram returns the program a credential.helper value runs: its
// first word, which may be quoted or contain backslash-escaped spaces.
func helperProgram(helper string) string {
	if q := helper[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(helper[1:], q); end >= 0 {
			return helper[1 : 1+end]
		}
	}
	var b strings.Builder
	for i := 0; i < len(helper); i++ {
		c := helper[i]
		if c == '\\' && i+1 < len(helper) {
			i++
			b.WriteByte(helper[i])
			continue
		}
		if c == ' ' || c == '\t' {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

// findHelper looks a helper up the way git does: in git's own program
// directory, where helpers such as store and cache live, then on PATH.
func findHelper(name string) (string, error) {
	if dir, err := output(5*time.Second, "", "git", "--exec-path"); err == nil {
		path := filepath.Join(dir, name)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return exec.LookPath(name)
}

// starts reports whether program can be started at all. It passes an
// operation name that git never sends; the credential protocol has helpers
// ignore operations they don't know, and how it exits doesn't matter.
func starts(program string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "ocx-doctor-probe")
	cmd.Stdin = strings.NewReader("")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func checkRepo(opts Options, repo string) []check.Result {
	prefix := filepath.Base(repo) + ": "
	top, err := output(10*time.Second, repo, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return []check.Result{result(prefix+"repository", []string{repo + " is not a git repository"})}
	}
	return []check.Result{
		checkHook(opts.Root, top, prefix),
		checkAgentsFile(top, prefix),
		checkProtection(opts, top, prefix),
	}
}

func checkHook(root, repo, prefix string) check.Result {
	name := prefix + "pre-push review gate"
	dir, err := output(10*time.Second, repo, "git", "rev-parse", "--git-path", "hooks")
	if err != nil {
		return result(name, []string{err.Error()})
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, dir)
	}
	installed, err := os.ReadFile(filepath.Join(dir, "pre-push"))
	if err != nil || !bytes.Contains(installed, []byte(hook.Marker)) {
		return result(name, []string{"not installed; run: make hook-install REPO=" + repo})
	}
	if fi, err := os.Stat(filepath.Join(dir, "pre-push")); err != nil || fi.Mode()&0o111 == 0 {
		return result(name, []string{"installed but not executable; run: make hook-install REPO=" + repo})
	}
	current, err := os.ReadFile(filepath.Join(root, config.HookFile))
	if err == nil && !bytes.Equal(installed, current) {
		return warn(name, "out of date; run: make hook-install REPO="+repo)
	}
	return check.Result{Name: name, Status: check.Pass}
}

func checkAgentsFile(repo, prefix string) check.Result {
	name := prefix + "AGENTS.md"
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); err != nil {
		return warn(name, "missing; agents won't know the build, test, and lint commands. Start from templates/AGENTS.md")
	}
	return check.Result{Name: name, Status: check.Pass}
}

var githubRemote = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?/?$`)

// GitHubRepo extracts owner and name from a GitHub remote URL.
func GitHubRepo(url string) (owner, name string, ok bool) {
	m := githubRemote.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// requiredRules are the ruleset rules that keep agents (and everyone else)
// off the default branch: no deletion, no force pushes, pull requests only.
var requiredRules = []string{"deletion", "non_fast_forward", "pull_request"}

func checkProtection(opts Options, repo, prefix string) check.Result {
	name := prefix + "GitHub branch protection"
	url, err := output(10*time.Second, repo, "git", "remote", "get-url", "origin")
	if err != nil {
		return check.Result{Name: name, Status: check.Skip, Details: []string{"no origin remote"}}
	}
	owner, repoName, ok := GitHubRepo(url)
	if !ok {
		return check.Result{Name: name, Status: check.Skip, Details: []string{"origin is not on GitHub"}}
	}
	if opts.Offline {
		return check.Result{Name: name, Status: check.Skip, Details: []string{"offline"}}
	}
	slug := owner + "/" + repoName
	branch, err := output(20*time.Second, "", "gh", "api", "repos/"+slug, "--jq", ".default_branch")
	if err != nil {
		return warn(name, "can't read "+slug+" with gh: "+err.Error())
	}
	raw, err := output(20*time.Second, "", "gh", "api", "repos/"+slug+"/rules/branches/"+branch)
	if err != nil {
		return warn(name, "can't read the rules for "+branch+": "+err.Error())
	}
	var rules []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return warn(name, "unexpected rules response: "+err.Error())
	}
	have := map[string]bool{}
	for _, r := range rules {
		have[r.Type] = true
	}
	var missing []string
	for _, r := range requiredRules {
		if !have[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return result(name, []string{fmt.Sprintf("%s on %s is missing rules: %s", branch, slug, strings.Join(missing, ", ")),
			"add a ruleset for the default branch that blocks deletion and force pushes and requires a pull request"})
	}
	return check.Result{Name: name, Status: check.Pass, Details: []string{branch + " on " + slug}}
}

func run(timeout time.Duration, dir string, name string, args ...string) error {
	_, err := output(timeout, dir, name, args...)
	return err
}

func output(timeout time.Duration, dir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = "timed out"
		}
		return "", errors.New(lastLine(msg))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
