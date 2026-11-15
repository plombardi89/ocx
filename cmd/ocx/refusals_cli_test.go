package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// outsideFiles is material outside the configuration directory that no
// command may touch.
type outsideFiles struct {
	root    string // the directory holding everything below
	file    string // a regular file
	dir     string // a directory with a file in it
	missing string // a path that doesn't exist
}

func newOutside(t *testing.T) outsideFiles {
	t.Helper()
	root := t.TempDir()
	o := outsideFiles{
		root:    root,
		file:    filepath.Join(root, "keep.md"),
		dir:     filepath.Join(root, "tree"),
		missing: filepath.Join(root, "never-created.md"),
	}
	writeText(t, o.file, "keep\n")
	writeText(t, filepath.Join(o.dir, "SKILL.md"), "someone else's\n")
	return o
}

// shippedSkillDir returns the installed directory of the first shipped skill,
// such as skills/some-skill.
func shippedSkillDir(t *testing.T) string {
	t.Helper()
	rel := shippedPathWithPrefix(t, shippedFiles(t), "skills/")
	parts := strings.Split(rel, "/")
	return parts[0] + "/" + parts[1]
}

// A setup function prepares the obstacle and returns the path, relative to
// the configuration directory, that the refusal has to name.
type obstacleSetup func(t *testing.T, s *sandbox, cfg string, out outsideFiles) string

func TestApplyRefusesWhatIsInTheWayBeforeChangingAnything(t *testing.T) {
	cases := []struct {
		name  string
		setup obstacleSetup
	}{
		{"a link at a file it writes", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			makeLink(t, out.file, filepath.Join(cfg, "AGENTS.md"))
			return "AGENTS.md"
		}},
		{"a link at a file it writes that points nowhere", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			rel := shippedPathWithPrefix(t, shippedFiles(t), "prompts/")
			makeLink(t, out.missing, filepath.Join(cfg, filepath.FromSlash(rel)))
			return rel
		}},
		{"a link at a directory it writes into", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			makeLink(t, out.dir, filepath.Join(cfg, "skills"))
			return "skills"
		}},
		{"a link at a nested directory it writes into", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			dir := shippedSkillDir(t)
			makeLink(t, out.dir, filepath.Join(cfg, filepath.FromSlash(dir)))
			return dir
		}},
		{"a directory where a file goes", func(t *testing.T, _ *sandbox, cfg string, _ outsideFiles) string {
			t.Helper()
			writeText(t, filepath.Join(cfg, "opencode.jsonc", "inner.txt"), "inside\n")
			return "opencode.jsonc"
		}},
		{"a directory where a nested file goes", func(t *testing.T, _ *sandbox, cfg string, _ outsideFiles) string {
			t.Helper()
			rel := shippedPathWithPrefix(t, shippedFiles(t), "commands/")
			writeText(t, filepath.Join(cfg, filepath.FromSlash(rel), "inner.txt"), "inside\n")
			return rel
		}},
		{"a link at a listed file it would delete", func(t *testing.T, s *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			s.install(cfg)
			writeText(t, filepath.Join(cfg, "AGENTS.md"), "edited by hand\n")
			makeLink(t, out.file, filepath.Join(cfg, "commands", "retired.md"))
			setList(t, cfg, append(listedPaths(t, cfg), "commands/retired.md")...)
			return "commands/retired.md"
		}},
		{"a link at a directory above a listed file it would delete", func(t *testing.T, s *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			s.install(cfg)
			writeText(t, filepath.Join(cfg, "AGENTS.md"), "edited by hand\n")
			makeLink(t, out.dir, filepath.Join(cfg, "skills", "retired"))
			setList(t, cfg, append(listedPaths(t, cfg), "skills/retired/SKILL.md")...)
			return "skills/retired"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			cfg := newConfigDir(t)
			out := newOutside(t)
			want := c.setup(t, s, cfg, out)
			beforeCfg, beforeOut := treeState(t, cfg), treeState(t, out.root)

			r := s.apply(cfg)
			if r.Code != 1 {
				t.Errorf("apply exited %d, want 1", r.Code)
			}
			if !strings.Contains(r.combined(), want) {
				t.Errorf("apply's message doesn't name %s: %q", want, r.shown())
			}
			requireUnchanged(t, "the configuration directory", beforeCfg, treeState(t, cfg))
			requireUnchanged(t, "the files outside the configuration directory", beforeOut, treeState(t, out.root))
		})
	}
}

func TestRemoveRefusesLinksBeforeDeletingAnything(t *testing.T) {
	cases := []struct {
		name  string
		setup obstacleSetup
	}{
		{"a link at a listed file", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			removeTree(t, filepath.Join(cfg, "AGENTS.md"))
			makeLink(t, out.file, filepath.Join(cfg, "AGENTS.md"))
			return "AGENTS.md"
		}},
		{"a link at a nested listed file", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			rel := shippedPathWithPrefix(t, shippedFiles(t), "commands/")
			removeTree(t, filepath.Join(cfg, filepath.FromSlash(rel)))
			makeLink(t, out.file, filepath.Join(cfg, filepath.FromSlash(rel)))
			return rel
		}},
		{"a link at a directory above a listed file", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			dir := shippedSkillDir(t)
			removeTree(t, filepath.Join(cfg, filepath.FromSlash(dir)))
			makeLink(t, out.dir, filepath.Join(cfg, filepath.FromSlash(dir)))
			return dir
		}},
		{"a link at a top-level directory above listed files", func(t *testing.T, _ *sandbox, cfg string, out outsideFiles) string {
			t.Helper()
			removeTree(t, filepath.Join(cfg, "skills"))
			makeLink(t, out.dir, filepath.Join(cfg, "skills"))
			return "skills"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSandbox(t)
			cfg := newConfigDir(t)
			out := newOutside(t)
			s.install(cfg)
			want := c.setup(t, s, cfg, out)
			beforeCfg, beforeOut := treeState(t, cfg), treeState(t, out.root)

			r := s.remove(cfg)
			if r.Code != 1 {
				t.Errorf("remove exited %d, want 1", r.Code)
			}
			if !strings.Contains(r.combined(), want) {
				t.Errorf("remove's message doesn't name %s: %q", want, r.shown())
			}
			requireUnchanged(t, "the configuration directory", beforeCfg, treeState(t, cfg))
			requireUnchanged(t, "the files outside the configuration directory", beforeOut, treeState(t, out.root))
		})
	}
}

func TestInvalidListEntriesAreRefusedBeforeAnythingChanges(t *testing.T) {
	entries := []struct {
		name  string
		entry func(cfg, outsideFile string) string
	}{
		{"parent directory", func(_, _ string) string { return "../keep.md" }},
		{"parent directory in the middle", func(_, _ string) string { return "skills/../../keep.md" }},
		{"parent directory that stays inside", func(_, _ string) string { return "skills/../mine.md" }},
		{"only the parent directory", func(_, _ string) string { return ".." }},
		{"absolute path outside", func(_, outside string) string { return outside }},
		{"absolute path of the configuration directory", func(cfg, _ string) string { return cfg }},
		{"the configuration directory itself", func(_, _ string) string { return "." }},
		{"the configuration directory itself with a slash", func(_, _ string) string { return "./" }},
	}
	for _, op := range []string{"apply", "remove"} {
		for _, e := range entries {
			t.Run(op+"/"+e.name, func(t *testing.T) {
				s := newSandbox(t)
				parent := t.TempDir()
				cfg := filepath.Join(parent, "opencode")
				outside := filepath.Join(parent, "keep.md")
				writeText(t, outside, "keep\n")
				writeText(t, filepath.Join(cfg, "mine.md"), "mine\n")
				setList(t, cfg, "mine.md", e.entry(cfg, outside))
				before := treeState(t, parent)

				var r cliResult
				if op == "apply" {
					r = s.apply(cfg)
				} else {
					r = s.remove(cfg)
				}
				if r.Code == 0 {
					t.Errorf("%s accepted the list: %s", op, r.shown())
				}
				requireUnchanged(t, "the directory and the file next to it", before, treeState(t, parent))
			})
		}
	}
}
