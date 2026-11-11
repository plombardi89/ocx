// Package hook installs the ocagents pre-push review gate into a repository.
package hook

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Marker identifies a hook managed by ocagents.
const Marker = "ocagents-managed-hook"

// Install copies the hook script at src into repo's hooks directory as
// pre-push. An existing pre-push hook that ocagents does not manage is
// preserved as pre-push.local; the managed hook runs it after its own checks.
// It returns the installed path.
func Install(src, repo string) (string, error) {
	script, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if !bytes.Contains(script, []byte(Marker)) {
		return "", fmt.Errorf("%s is not the ocagents hook (missing %q marker)", src, Marker)
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not a git repository: %w", repo, err)
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "pre-push")
	if existing, err := os.ReadFile(dst); err == nil && !bytes.Contains(existing, []byte(Marker)) {
		local := filepath.Join(dir, "pre-push.local")
		if _, err := os.Stat(local); err == nil {
			return "", fmt.Errorf("both %s and %s exist; merge them by hand first", dst, local)
		}
		if err := os.Rename(dst, local); err != nil {
			return "", err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(dst, script, 0o755); err != nil {
		return "", err
	}
	return dst, os.Chmod(dst, 0o755)
}
