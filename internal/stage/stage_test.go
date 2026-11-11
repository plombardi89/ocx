package stage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/gitx"
)

func TestStage(t *testing.T) {
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := gitx.Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := Stage(root, dir, Options{NonInteractive: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		".opencode/opencode.jsonc",
		".opencode/prompts/reviewer.md",
		".opencode/skills/code-review/SKILL.md",
		".opencode/skills/code-review/references/go.md",
		".opencode/commands/review.md",
		InstructionsName,
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}

	var project struct {
		Instructions []string `json:"instructions"`
		Agents       map[string]struct {
			Permissions []map[string]string `json:"permissions"`
		} `json:"agents"`
	}
	data, err := os.ReadFile(filepath.Join(dir, ".opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	if len(project.Instructions) != 1 || project.Instructions[0] != InstructionsName {
		t.Errorf("instructions = %v", project.Instructions)
	}
	if rules := project.Agents["build"].Permissions; len(rules) != 1 || rules[0]["action"] != "question" || rules[0]["effect"] != "deny" {
		t.Errorf("non-interactive build rules = %v", rules)
	}

	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); err == nil {
		t.Error("staging wrote opencode.json outside .opencode")
	}
	if status, _ := gitx.Run(dir, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Errorf("staged files show up in git status: %q", status)
	}

	if err := Stage(root, dir, Options{}); err == nil {
		t.Error("staging twice should fail rather than overwrite")
	}
}

// Linked worktrees share .git/info/exclude, so a change there would follow
// the author back into their own checkout.
func TestStagingLeavesTheSharedExcludeFileAlone(t *testing.T) {
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := gitx.Init(dir); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(dir, ".git", "info", "exclude")
	before, _ := os.ReadFile(excludePath)
	if err := Stage(root, dir, Options{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(excludePath)
	if string(after) != string(before) {
		t.Fatalf("exclude file changed:\n%s", strings.TrimPrefix(string(after), string(before)))
	}
}
