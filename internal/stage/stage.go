// Package stage copies the ocagents configuration into a project's .opencode
// directory, so a session started there runs with these agents and skills
// without touching the global OpenCode configuration.
package stage

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ocagents/internal/config"
)

// InstructionsName is the staged copy of the global AGENTS.md. It is loaded
// through the "instructions" setting of the generated project opencode.json.
const InstructionsName = ".opencode/ocagents-instructions.md"

// Options adjust the staged configuration.
type Options struct {
	// NonInteractive denies the question tool for build, so headless runs
	// cannot block waiting for an answer.
	NonInteractive bool
}

// Stage copies the configuration from the ocagents root into target.
func Stage(root, target string, opts Options) error {
	dotdir := filepath.Join(target, ".opencode")
	if _, err := os.Stat(dotdir); err == nil {
		return fmt.Errorf("%s already exists; stage into a fresh copy of the project", dotdir)
	}
	projectConfig := filepath.Join(target, "opencode.json")
	if _, err := os.Stat(projectConfig); err == nil {
		return fmt.Errorf("%s already exists; stage into a fresh copy of the project", projectConfig)
	}

	copies := []struct{ src, dst string }{
		{config.ConfigFile, ".opencode/opencode.jsonc"},
		{config.PromptsDir, ".opencode/prompts"},
		{config.SkillsDir, ".opencode/skills"},
		{config.CommandsDir, ".opencode/commands"},
		{config.InstructionsFile, InstructionsName},
	}
	for _, c := range copies {
		if err := copyPath(filepath.Join(root, c.src), filepath.Join(target, c.dst)); err != nil {
			return err
		}
	}

	project := map[string]any{
		"$schema":      "https://opencode.ai/config.json",
		"instructions": []string{InstructionsName},
	}
	if opts.NonInteractive {
		project["agents"] = map[string]any{
			"build": map[string]any{
				"permissions": []map[string]string{
					{"action": "question", "resource": "*", "effect": "deny"},
				},
			},
		}
	}
	data, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(projectConfig, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return excludeFromGit(target, ".opencode/", "/opencode.json")
}

// CopyTree copies a directory tree or a single file.
func CopyTree(src, dst string) error { return copyPath(src, dst) }

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, out, fi.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode.Perm())
}

// excludeFromGit adds patterns to .git/info/exclude so staged files never
// show up as untracked changes in reviews.
func excludeFromGit(dir string, patterns ...string) error {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return nil // not a git repository
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	existing, _ := os.ReadFile(path)
	var add []string
	for _, p := range patterns {
		if !strings.Contains("\n"+string(existing)+"\n", "\n"+p+"\n") {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + "# staged by ocagents\n" + strings.Join(add, "\n") + "\n")
	return err
}
