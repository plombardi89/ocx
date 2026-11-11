package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plombardi89/ocx/internal/config"
)

var now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func plan(t *testing.T, root, dir string, force bool) *Plan {
	t.Helper()
	p, err := PlanInstall(root, dir, force)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func apply(t *testing.T, p *Plan) {
	t.Helper()
	if err := Apply(p, now); err != nil {
		t.Fatalf("apply: %v\n%s", err, p.Describe())
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kinds(p *Plan) map[string]string {
	out := map[string]string{}
	for _, a := range p.Actions {
		out[a.Path] = a.Kind
	}
	return out
}

func TestDryRunChangesNothing(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	p := plan(t, root, dir, false)
	if len(p.Conflicts) > 0 || !p.Changes() {
		t.Fatalf("unexpected plan:\n%s", p.Describe())
	}
	k := kinds(p)
	for _, path := range []string{"opencode.jsonc", "prompts", "AGENTS.md", "skills/code-review", "commands/review.md"} {
		if k[path] != Link {
			t.Errorf("%s: %q, want link", path, k[path])
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("planning wrote %d entries", len(entries))
	}
}

func TestApplyLinksEverythingAndIsIdempotent(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	apply(t, plan(t, root, dir, false))

	want := map[string]string{
		"opencode.jsonc":     filepath.Join(root, "config/opencode.jsonc"),
		"prompts":            filepath.Join(root, "config/prompts"),
		"AGENTS.md":          filepath.Join(root, "config/global-AGENTS.md"),
		"skills/code-review": filepath.Join(root, "skills/code-review"),
		"commands/plan.md":   filepath.Join(root, "commands/plan.md"),
	}
	for path, target := range want {
		got, err := os.Readlink(filepath.Join(dir, path))
		if err != nil || got != target {
			t.Errorf("%s -> %q (%v), want %s", path, got, err, target)
		}
	}
	m, err := ReadManifest(dir)
	if err != nil || m == nil || m.Root != root || len(m.Links) < len(want) {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
	if again := plan(t, root, dir, false); again.Changes() {
		t.Fatalf("second plan wants changes:\n%s", again.Describe())
	}
}

func TestBacksUpConfigThatOnlySetsSchema(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	original := "{\n  \"$schema\": \"https://opencode.ai/config.json\"\n}\n"
	write(t, filepath.Join(dir, "opencode.jsonc"), original)
	p := plan(t, root, dir, false)
	if len(p.Conflicts) > 0 || kinds(p)["opencode.jsonc"] != Backup {
		t.Fatalf("unexpected plan:\n%s", p.Describe())
	}
	apply(t, p)
	backup := filepath.Join(dir, BackupDir, "20261111T120000Z", "opencode.jsonc")
	if data, err := os.ReadFile(backup); err != nil || string(data) != original {
		t.Fatalf("backup = %q, %v", data, err)
	}
}

func TestRealSettingsAreAConflictUnlessForced(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	write(t, filepath.Join(dir, "opencode.jsonc"), `{"model": "some/model", "share": "disabled"}`)
	write(t, filepath.Join(dir, "skills/code-review/SKILL.md"), "my own skill\n")
	p := plan(t, root, dir, false)
	if len(p.Conflicts) != 2 {
		t.Fatalf("conflicts = %v", p.Conflicts)
	}
	if err := Apply(p, now); err == nil {
		t.Fatal("applied a plan with conflicts")
	}
	forced := plan(t, root, dir, true)
	if len(forced.Conflicts) > 0 {
		t.Fatalf("forced plan has conflicts: %v", forced.Conflicts)
	}
	apply(t, forced)
	if _, err := os.Stat(filepath.Join(dir, BackupDir, "20261111T120000Z", "skills/code-review/SKILL.md")); err != nil {
		t.Fatalf("user skill not backed up: %v", err)
	}
}

func TestOtherConfigFileName(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	write(t, filepath.Join(dir, "opencode.json"), `{"$schema": "https://opencode.ai/config.json"}`)
	if p := plan(t, root, dir, false); len(p.Conflicts) > 0 || kinds(p)["opencode.json"] != Backup {
		t.Fatalf("trivial opencode.json:\n%s", p.Describe())
	}
	write(t, filepath.Join(dir, "opencode.json"), `{"model": "x/y"}`)
	if p := plan(t, root, dir, false); len(p.Conflicts) != 1 {
		t.Fatalf("opencode.json with settings: conflicts = %v", p.Conflicts)
	}
}

func TestForeignLinkIsAConflict(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	if err := os.Symlink("/somewhere/else", filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if p := plan(t, root, dir, false); len(p.Conflicts) != 1 || !strings.Contains(p.Conflicts[0], "AGENTS.md") {
		t.Fatalf("conflicts = %v", p.Conflicts)
	}
}

func TestRelinksAndRemovesOwnLinks(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	apply(t, plan(t, root, dir, false))

	// A link ocx created that now points elsewhere is replaced.
	agents := filepath.Join(dir, "AGENTS.md")
	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "README.md"), agents); err != nil {
		t.Fatal(err)
	}
	// A skill that is no longer in the repository is removed.
	stale := filepath.Join(dir, "skills", "retired-skill")
	if err := os.Symlink(filepath.Join(root, "skills", "code-review"), stale); err != nil {
		t.Fatal(err)
	}
	m, _ := ReadManifest(dir)
	m.Links = append(m.Links, "skills/retired-skill")
	if err := writeManifest(dir, m); err != nil {
		t.Fatal(err)
	}

	p := plan(t, root, dir, false)
	k := kinds(p)
	if k["AGENTS.md"] != Relink || k["skills/retired-skill"] != Remove || len(p.Conflicts) > 0 {
		t.Fatalf("unexpected plan:\n%s", p.Describe())
	}
	apply(t, p)
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale link still present: %v", err)
	}
	if again := plan(t, root, dir, false); again.Changes() {
		t.Fatalf("still wants changes:\n%s", again.Describe())
	}
}

func TestUninstallRestoresThePreviousState(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	original := `{"$schema": "https://opencode.ai/config.json"}`
	write(t, filepath.Join(dir, "opencode.jsonc"), original)
	write(t, filepath.Join(dir, "cli.json"), `{}`)
	apply(t, plan(t, root, dir, false))

	steps, err := Uninstall(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(dir, "opencode.jsonc")); err != nil {
		t.Fatalf("dry run removed a link: %v\n%s", err, strings.Join(steps, "\n"))
	}

	if _, err := Uninstall(dir, true); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "opencode.jsonc")); err != nil || string(data) != original {
		t.Fatalf("opencode.jsonc = %q, %v", data, err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, ",") != "cli.json,opencode.jsonc" {
		t.Fatalf("left behind: %v", left)
	}
}

func TestUninstallWithoutInstall(t *testing.T) {
	if _, err := Uninstall(t.TempDir(), true); err == nil {
		t.Fatal("uninstall without a manifest should fail")
	}
}
