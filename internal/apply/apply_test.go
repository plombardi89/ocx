package apply

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var v1 = map[string][]byte{
	"opencode.jsonc":           []byte(`{"agents": {}}`),
	"AGENTS.md":                []byte("# rules\n"),
	"prompts/reviewer.md":      []byte("review\n"),
	"skills/old/SKILL.md":      []byte("old skill\n"),
	"skills/new/SKILL.md":      []byte("new skill\n"),
	"skills/new/refs/notes.md": []byte("notes\n"),
	"commands/plan.md":         []byte("plan\n"),
}

func without(files map[string][]byte, drop ...string) map[string][]byte {
	out := map[string][]byte{}
	maps.Copy(out, files)
	for _, d := range drop {
		delete(out, d)
	}
	return out
}

// tree returns every file under dir with its content, and every directory,
// so a test can tell that nothing changed.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link to " + target
		case d.IsDir():
			out[rel+"/"] = ""
		default:
			data, _ := os.ReadFile(p)
			out[rel] = string(data)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return out
}

func list(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ListFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustApply(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if _, err := Apply(dir, files, false); err != nil {
		t.Fatal(err)
	}
}

func TestFirstApplyWritesEveryFileAndListsThem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "opencode")
	changes, err := Apply(dir, v1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != len(v1) {
		t.Fatalf("changes = %v", changes)
	}
	for p, want := range v1 {
		got, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s = %q, %v", p, got, err)
		}
		fi, _ := os.Lstat(filepath.Join(dir, p))
		if !fi.Mode().IsRegular() {
			t.Errorf("%s is not a regular file", p)
		}
	}
	var paths []string
	for p := range v1 {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if got := list(t, dir); got != strings.Join(paths, "\n")+"\n" {
		t.Errorf("list = %q", got)
	}
}

func TestApplyOverwritesFilesAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte(`{"$schema": "x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mustApply(t, dir, v1)
	if got, _ := os.ReadFile(filepath.Join(dir, "opencode.jsonc")); string(got) != string(v1["opencode.jsonc"]) {
		t.Fatalf("opencode.jsonc = %q", got)
	}
}

func TestApplyDeletesFilesTheNewVersionDrops(t *testing.T) {
	dir := t.TempDir()
	mustApply(t, dir, v1)
	// A file of the user's in a shared directory keeps that directory.
	if err := os.WriteFile(filepath.Join(dir, "commands", "mine.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := Apply(dir, without(v1, "skills/old/SKILL.md", "commands/plan.md"), false)
	if err != nil {
		t.Fatal(err)
	}
	var deleted []string
	for _, c := range changes {
		if c.Op == "delete" {
			deleted = append(deleted, c.Path)
		}
	}
	if strings.Join(deleted, ",") != "skills/old/SKILL.md,commands/plan.md" && strings.Join(deleted, ",") != "commands/plan.md,skills/old/SKILL.md" {
		t.Fatalf("deleted = %v", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "old")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the emptied skills/old directory is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "commands", "mine.md")); err != nil {
		t.Errorf("the user's own command was removed: %v", err)
	}
	if strings.Contains(list(t, dir), "commands/plan.md") {
		t.Error("the list still names a dropped file")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	dir := t.TempDir()
	mustApply(t, dir, v1)
	before := tree(t, dir)
	changes, err := Apply(dir, without(v1, "skills/old/SKILL.md"), true)
	if err != nil || len(changes) != len(v1) {
		t.Fatalf("changes = %v, %v", changes, err)
	}
	if changes, err := Remove(dir, true); err != nil || len(changes) != len(v1) {
		t.Fatalf("remove changes = %v, %v", changes, err)
	}
	after := tree(t, dir)
	if len(before) != len(after) {
		t.Fatalf("dry runs changed the directory: %v -> %v", before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed", k)
		}
	}
}

func TestRemoveDeletesWhatApplyWroteAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tui.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustApply(t, dir, v1)
	if _, err := Remove(dir, false); err != nil {
		t.Fatal(err)
	}
	got := tree(t, dir)
	if len(got) != 2 || got["tui.jsonc"] != "{}" {
		t.Fatalf("left behind: %v", got)
	}
	if _, err := Remove(dir, false); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestRetriesSkipListedFilesThatAreAlreadyGone(t *testing.T) {
	dir := t.TempDir()
	mustApply(t, dir, v1)
	if err := os.Remove(filepath.Join(dir, "skills", "old", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	mustApply(t, dir, without(v1, "skills/old/SKILL.md"))
	if err := os.Remove(filepath.Join(dir, "commands", "plan.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(dir, false); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, dir); len(got) != 1 {
		t.Fatalf("left behind: %v", got)
	}
}

func TestRefusesWhatsInTheWayBeforeChangingAnything(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir, outside string)
		want  string
	}{
		{"a link at a file it writes", func(t *testing.T, dir, outside string) {
			t.Helper()
			if err := os.Symlink(filepath.Join(outside, "keep.md"), filepath.Join(dir, "AGENTS.md")); err != nil {
				t.Fatal(err)
			}
		}, "AGENTS.md is a link"},
		{"a link at a directory it writes into", func(t *testing.T, dir, outside string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "skills", "new")); err != nil {
				t.Fatal(err)
			}
		}, "skills/new is a link"},
		{"a directory where a file goes", func(t *testing.T, dir, _ string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(dir, "opencode.jsonc"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "opencode.jsonc is a directory"},
		{"a file where a directory goes", func(t *testing.T, dir, _ string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(dir, "prompts"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "prompts is a file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "keep.md"), []byte("keep\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			c.setup(t, dir, outside)
			before, beforeOutside := tree(t, dir), tree(t, outside)
			_, err := Apply(dir, v1, false)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if after := tree(t, dir); len(after) != len(before) {
				t.Errorf("the directory changed: %v", after)
			}
			if after := tree(t, outside); after["keep.md"] != beforeOutside["keep.md"] || len(after) != len(beforeOutside) {
				t.Errorf("the outside directory changed: %v", after)
			}
		})
	}
}

func TestNeverDeletesThroughALinkedDirectory(t *testing.T) {
	for _, op := range []string{"apply", "remove"} {
		t.Run(op, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			mustApply(t, dir, v1)
			// The listed skills/old becomes a link to a directory elsewhere
			// that happens to hold a SKILL.md of its own.
			if err := os.RemoveAll(filepath.Join(dir, "skills", "old")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("someone else's\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "skills", "old")); err != nil {
				t.Fatal(err)
			}
			var err error
			if op == "apply" {
				_, err = Apply(dir, without(v1, "skills/old/SKILL.md"), false)
			} else {
				_, err = Remove(dir, false)
			}
			if err == nil || !strings.Contains(err.Error(), "skills/old is a link") {
				t.Fatalf("err = %v", err)
			}
			if got, _ := os.ReadFile(filepath.Join(outside, "SKILL.md")); string(got) != "someone else's\n" {
				t.Fatalf("the file behind the link changed: %q", got)
			}
		})
	}
}

func TestRefusesListEntriesOutsideTheDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "opencode")
	outside := filepath.Join(parent, "keep.md")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"../keep.md", outside, "skills/../../keep.md", "."} {
		t.Run(entry, func(t *testing.T) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ListFile), []byte(entry+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Apply(dir, v1, false); err == nil || !strings.Contains(err.Error(), "isn't a path inside") {
				t.Errorf("apply: %v", err)
			}
			if _, err := Remove(dir, false); err == nil || !strings.Contains(err.Error(), "isn't a path inside") {
				t.Errorf("remove: %v", err)
			}
			if got, _ := os.ReadFile(outside); string(got) != "keep\n" {
				t.Fatal("the file outside the directory changed")
			}
			if _, err := os.Stat(filepath.Join(dir, "opencode.jsonc")); !errors.Is(err, fs.ErrNotExist) {
				t.Error("apply wrote files despite the invalid list")
			}
		})
	}
}

func TestAFailedApplyLeavesThePreviousListWhole(t *testing.T) {
	failOn := func(name string) func() {
		orig := writeFile
		writeFile = func(p string, data []byte, perm os.FileMode) error {
			if filepath.Base(p) == name {
				return errors.New("disk full")
			}
			return orig(p, data, perm)
		}
		return func() { writeFile = orig }
	}
	cases := []struct {
		name string
		fail func() func()
		want string
	}{
		{"writing a file", func() func() { return failOn("reviewer.md") }, "write prompts/reviewer.md: disk full"},
		{"writing the list", func() func() { return failOn(ListFile + ".tmp") }, "write " + ListFile + ": disk full"},
		{"renaming the list", func() func() {
			orig := rename
			rename = func(string, string) error { return errors.New("disk full") }
			return func() { rename = orig }
		}, "write " + ListFile + ": disk full"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			mustApply(t, dir, without(v1, "skills/new/SKILL.md"))
			before := list(t, dir)
			restore := c.fail()
			_, err := Apply(dir, v1, false)
			restore()
			if err == nil || err.Error() != c.want {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if got := list(t, dir); got != before {
				t.Fatalf("list = %q, want %q", got, before)
			}
			if _, err := os.Stat(filepath.Join(dir, ListFile+".tmp")); !errors.Is(err, fs.ErrNotExist) {
				t.Error("the temporary list was left behind")
			}
			mustApply(t, dir, v1)
			if !strings.Contains(list(t, dir), "skills/new/SKILL.md") {
				t.Error("a retry didn't finish the job")
			}
		})
	}
}

func TestConfigDirFollowsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if got, _ := ConfigDir(); got != "/x/opencode" {
		t.Fatalf("got %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got, _ := ConfigDir(); got != "/home/u/.config/opencode" {
		t.Fatalf("got %s", got)
	}
}

func TestTheListAndItsTemporaryFileFollowTheSameRules(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir, outside string)
		ops   []string
		want  string
	}{
		{"a link at the list", func(t *testing.T, dir, outside string) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, ListFile)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "keep.md"), filepath.Join(dir, ListFile)); err != nil {
				t.Fatal(err)
			}
		}, []string{"apply", "remove"}, ListFile + " is a link"},
		{"a link at the temporary list", func(t *testing.T, dir, outside string) {
			t.Helper()
			if err := os.Symlink(filepath.Join(outside, "keep.md"), filepath.Join(dir, listTemp)); err != nil {
				t.Fatal(err)
			}
		}, []string{"apply", "remove"}, listTemp + " is a link"},
		{"a directory at the temporary list", func(t *testing.T, dir, _ string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, listTemp), 0o755); err != nil {
				t.Fatal(err)
			}
		}, []string{"apply", "remove"}, listTemp + " is a directory"},
	}
	for _, c := range cases {
		for _, op := range c.ops {
			t.Run(c.name+"/"+op, func(t *testing.T) {
				dir, outside := t.TempDir(), t.TempDir()
				mustApply(t, dir, without(v1, "AGENTS.md"))
				if err := os.WriteFile(filepath.Join(outside, "keep.md"), []byte("keep outside\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				c.setup(t, dir, outside)
				before := tree(t, dir)
				var err error
				if op == "apply" {
					_, err = Apply(dir, v1, false)
				} else {
					_, err = Remove(dir, false)
				}
				if err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("err = %v, want %q", err, c.want)
				}
				if got, _ := os.ReadFile(filepath.Join(outside, "keep.md")); string(got) != "keep outside\n" {
					t.Fatalf("the file outside changed to %q", got)
				}
				after := tree(t, dir)
				if len(after) != len(before) {
					t.Fatalf("the directory changed:\n%v\n%v", before, after)
				}
				for k, v := range before {
					if after[k] != v {
						t.Errorf("%s changed", k)
					}
				}
			})
		}
	}
}

func TestAFailedDirectoryCleanupKeepsTheList(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove directories regardless of permissions")
	}
	for _, op := range []string{"apply", "remove"} {
		t.Run(op, func(t *testing.T) {
			dir := t.TempDir()
			mustApply(t, dir, v1)
			before := list(t, dir)
			// skills/old can still lose its file, but skills can't lose the
			// emptied skills/old.
			skills := filepath.Join(dir, "skills")
			if err := os.Chmod(skills, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })
			var err error
			if op == "apply" {
				_, err = Apply(dir, without(v1, "skills/old/SKILL.md"), false)
			} else {
				_, err = Remove(dir, false)
			}
			if err == nil || !strings.Contains(err.Error(), "delete the emptied directory skills/") {
				t.Fatalf("err = %v, want a failure to delete an emptied skill directory", err)
			}
			if got := list(t, dir); got != before {
				t.Fatalf("the list changed to %q", got)
			}
			if err := os.Chmod(skills, 0o755); err != nil {
				t.Fatal(err)
			}
			if op == "apply" {
				mustApply(t, dir, without(v1, "skills/old/SKILL.md"))
			} else if _, err := Remove(dir, false); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(skills, "old")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a rerun left skills/old behind: %v", err)
			}
		})
	}
}

func TestARetryFinishesACleanupThatFailedPartWay(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove directories regardless of permissions")
	}
	files := without(v1)
	files["skills/old/references/nested/deep/notes.md"] = []byte("deep\n")
	for _, op := range []string{"apply", "remove"} {
		t.Run(op, func(t *testing.T) {
			dir := t.TempDir()
			mustApply(t, dir, files)
			run := func() error {
				var err error
				if op == "apply" {
					_, err = Apply(dir, without(files, "skills/old/SKILL.md", "skills/old/references/nested/deep/notes.md"), false)
				} else {
					_, err = Remove(dir, false)
				}
				return err
			}
			// The first run deletes the note and deep/, then can't delete
			// the emptied nested/.
			references := filepath.Join(dir, "skills", "old", "references")
			if err := os.Chmod(references, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(references, 0o755) })
			if err := run(); err == nil || !strings.Contains(err.Error(), "skills/old/references/nested") {
				t.Fatalf("first run: %v", err)
			}
			if err := os.Chmod(references, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := run(); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "skills", "old")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the retry left emptied directories behind under skills/old: %v", tree(t, filepath.Join(dir, "skills", "old")))
			}
		})
	}
}
