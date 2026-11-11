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

// snapshot describes every entry under dir: links by target, files by
// content, so two snapshots are equal only if nothing changed.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			out[rel] = "link " + target
			return err
		case d.IsDir():
			out[rel] = "dir"
		default:
			data, err := os.ReadFile(path)
			out[rel] = "file " + string(data)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshot(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s: was %q, now %q", k, v, after[k])
		}
	}
	for k, v := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s: new entry %q", k, v)
		}
	}
}

func TestForcedReinstallKeepsTheOriginalBackup(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	write(t, agents, "original instructions\n")
	apply(t, plan(t, root, dir, true))

	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	write(t, agents, "replacement instructions\n")
	apply(t, plan(t, root, dir, true)) // same timestamp as the first install

	if data, err := os.ReadFile(filepath.Join(dir, BackupDir, "20261111T120000Z-2", "AGENTS.md")); err != nil || string(data) != "replacement instructions\n" {
		t.Errorf("second backup = %q, %v", data, err)
	}
	if _, err := Uninstall(dir, true); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(agents); err != nil || string(data) != "original instructions\n" {
		t.Fatalf("restored %q, %v; the original was overwritten", data, err)
	}
}

func TestFailedInstallLeavesTheDirectoryAsItWas(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root, dir := repoRoot(t), t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "original instructions\n")
	write(t, filepath.Join(dir, "opencode.jsonc"), `{"$schema": "https://opencode.ai/config.json"}`)
	skills := filepath.Join(dir, "skills")
	if err := os.Mkdir(skills, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })
	before := snapshot(t, dir)

	if err := Apply(plan(t, root, dir, true), now); err == nil {
		t.Fatal("install into a read-only skills directory succeeded")
	}
	sameSnapshot(t, before, snapshot(t, dir))
}

func TestFailedManifestWriteLeavesTheDirectoryAsItWas(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "original instructions\n")
	// A directory where the manifest's temporary file goes makes the write fail.
	write(t, filepath.Join(dir, ManifestName+".tmp", "keep"), "x")
	before := snapshot(t, dir)

	if err := Apply(plan(t, root, dir, true), now); err == nil {
		t.Fatal("install succeeded without writing its manifest")
	}
	sameSnapshot(t, before, snapshot(t, dir))
}

func TestFailedReinstallRestoresTheEarlierInstall(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	apply(t, plan(t, root, dir, false))
	agents := filepath.Join(dir, "AGENTS.md")
	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "README.md"), agents); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ManifestName+".tmp", "keep"), "x")
	before := snapshot(t, dir)

	p := plan(t, root, dir, false)
	if kinds(p)["AGENTS.md"] != Relink {
		t.Fatalf("unexpected plan:\n%s", p.Describe())
	}
	if err := Apply(p, now); err == nil {
		t.Fatal("install succeeded without writing its manifest")
	}
	sameSnapshot(t, before, snapshot(t, dir))
}

func TestLinkedOpencodeJSONWithSettingsIsAConflict(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	settings := filepath.Join(t.TempDir(), "settings.json")
	write(t, settings, `{"model": "custom/model"}`)
	if err := os.Symlink(settings, filepath.Join(dir, "opencode.json")); err != nil {
		t.Fatal(err)
	}
	if p := plan(t, root, dir, false); len(p.Conflicts) != 1 || !strings.Contains(p.Conflicts[0], "opencode.json") {
		t.Fatalf("conflicts = %v", p.Conflicts)
	}
	if p := plan(t, root, dir, true); len(p.Conflicts) > 0 || kinds(p)["opencode.json"] != Backup {
		t.Fatalf("forced plan:\n%s", p.Describe())
	}

	write(t, settings, `{"$schema": "https://opencode.ai/config.json"}`)
	if p := plan(t, root, dir, false); len(p.Conflicts) > 0 || kinds(p)["opencode.json"] != Backup {
		t.Fatalf("link to a config that only sets $schema:\n%s", p.Describe())
	}
}

func TestAdoptsCorrectLinksThatHaveNoManifest(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	apply(t, plan(t, root, dir, false))
	if err := os.Remove(filepath.Join(dir, ManifestName)); err != nil {
		t.Fatal(err)
	}

	p := plan(t, root, dir, false)
	if !p.Changes() {
		t.Fatalf("plan for links without a manifest:\n%s", p.Describe())
	}
	apply(t, p)
	if m, err := ReadManifest(dir); err != nil || m == nil || m.Root != root {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
	if again := plan(t, root, dir, false); again.Changes() {
		t.Fatalf("still wants changes:\n%s", again.Describe())
	}
	if _, err := Uninstall(dir, true); err != nil {
		t.Fatal(err)
	}
	// Install didn't create skills/ and commands/ this time, so they stay.
	for path, kind := range snapshot(t, dir) {
		if kind != "dir" {
			t.Errorf("uninstall left %s (%s)", path, kind)
		}
	}
}

func TestUninstallKeepsDirectoriesItDidNotCreate(t *testing.T) {
	for _, sub := range []string{"skills", "commands"} {
		t.Run("linked "+sub, func(t *testing.T) {
			root, dir, library := repoRoot(t), t.TempDir(), t.TempDir()
			write(t, filepath.Join(library, "mine.md"), "keep\n")
			container := filepath.Join(dir, sub)
			if err := os.Symlink(library, container); err != nil {
				t.Fatal(err)
			}
			apply(t, plan(t, root, dir, false))
			if _, err := Uninstall(dir, true); err != nil {
				t.Fatal(err)
			}
			if target, err := os.Readlink(container); err != nil || target != library {
				t.Fatalf("%s link = %q, %v; want %s", sub, target, err, library)
			}
			if entries, _ := os.ReadDir(library); len(entries) != 1 {
				t.Fatalf("%s has %d entries, want only the user's file", library, len(entries))
			}
		})
	}
	t.Run("empty skills directory", func(t *testing.T) {
		root, dir := repoRoot(t), t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		apply(t, plan(t, root, dir, false))
		if _, err := Uninstall(dir, true); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Lstat(filepath.Join(dir, "skills")); err != nil || !fi.IsDir() {
			t.Fatalf("skills directory: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "commands")); !os.IsNotExist(err) {
			t.Fatalf("commands directory install created is still there: %v", err)
		}
	})
}
