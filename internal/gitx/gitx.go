// Package gitx runs git commands for ocx tooling with the user's global
// and system git configuration ignored, so results do not depend on the
// machine's settings.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Env returns the environment for isolated git commands.
func Env() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
}

// Run runs git in dir and returns trimmed stdout. On failure the error
// includes stderr.
func Run(dir string, args ...string) (string, error) {
	return RunInput(dir, "", args...)
}

// RunInput is Run with stdin.
func RunInput(dir, input string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = Env()
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Init creates a repository on branch main with a local identity, signing
// disabled, and hooks pinned to the repository's own hooks directory.
func Init(dir string) error {
	steps := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "ocx eval"},
		{"config", "user.email", "eval@ocx.invalid"},
		{"config", "commit.gpgsign", "false"},
		{"config", "tag.gpgsign", "false"},
	}
	for _, s := range steps {
		if _, err := Run(dir, s...); err != nil {
			return err
		}
	}
	hooks, err := Run(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	_, err = Run(dir, "config", "core.hooksPath", hooks+"/hooks")
	return err
}

// CommitAll stages everything and commits with message.
func CommitAll(dir, message string) (string, error) {
	if _, err := Run(dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := Run(dir, "commit", "-q", "-m", message); err != nil {
		return "", err
	}
	return Run(dir, "rev-parse", "HEAD")
}
