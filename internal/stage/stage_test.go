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
	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
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

	if status, _ := gitx.Run(dir, "status", "--porcelain"); status != "" {
		t.Errorf("staged files are not excluded from git: %q", status)
	}
	exclude, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if strings.Count(string(exclude), ".opencode/") != 1 {
		t.Errorf("exclude file: %q", exclude)
	}

	if err := Stage(root, dir, Options{}); err == nil {
		t.Error("staging twice should fail rather than overwrite")
	}
}
