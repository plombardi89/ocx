package ocx

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesCarryTheWholeConfigurationAtItsInstalledPaths(t *testing.T) {
	got, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"config/opencode.jsonc":   "opencode.jsonc",
		"config/global-AGENTS.md": "AGENTS.md",
	}
	for _, dir := range []string{"config/prompts", "skills", "commands"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel := filepath.ToSlash(path)
			if dir == "config/prompts" {
				want[rel] = "prompts/" + filepath.ToSlash(path[len(dir)+1:])
			} else {
				want[rel] = rel
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d files embedded, %d in the repository", len(got), len(want))
	}
	for src, dst := range want {
		disk, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if string(got[dst]) != string(disk) {
			t.Errorf("%s: embedded at %s with different content, or missing", src, dst)
		}
	}
	for _, p := range []string{"opencode.jsonc", "AGENTS.md", "prompts/reviewer.md", "skills/code-review/SKILL.md", "commands/plan.md"} {
		if _, ok := got[p]; !ok {
			t.Errorf("missing %s", p)
		}
	}
}
