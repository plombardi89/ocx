package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// lockDir makes dir unwritable for the rest of the test, so creating or
// deleting an entry in it fails, and makes it writable again afterwards.
func lockDir(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("directory permissions don't stop the root user")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func unlockDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestApplyStopsWhenAFileCannotBeWrittenAndAFreshRunFinishesTheJob(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	prompts := filepath.Join(cfg, "prompts")
	lockDir(t, prompts)

	r := s.apply(cfg)
	if r.Code != 1 {
		t.Errorf("apply exited %d, want 1", r.Code)
	}
	out := r.combined()
	if !strings.Contains(out, "prompts/") {
		t.Errorf("apply's message doesn't name the path that failed: %q", out)
	}
	if !regexp.MustCompile(`(?i)writ`).MatchString(out) {
		t.Errorf("apply's message doesn't name the operation that failed: %q", out)
	}
	if pathExists(filepath.Join(cfg, ".ocx-files")) {
		t.Error("apply wrote .ocx-files although a file before it failed")
	}

	unlockDir(t, prompts)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("the second apply exited %d: %s", r.Code, r.shown())
	}
	shipped := shippedFiles(t)
	for rel, want := range shipped {
		if got := readText(t, filepath.Join(cfg, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s is missing or wrong after the second apply", rel)
		}
	}
	if got := len(listedPaths(t, cfg)); got != len(shipped) {
		t.Errorf(".ocx-files lists %d files after the second apply, want %d", got, len(shipped))
	}
}

func TestApplyThatFailsOnAFileKeepsThePreviousList(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	writeText(t, filepath.Join(cfg, "AGENTS.md"), "from an older version\n")
	setList(t, cfg, "AGENTS.md")
	before := readText(t, filepath.Join(cfg, ".ocx-files"))
	prompts := filepath.Join(cfg, "prompts")
	lockDir(t, prompts)

	if r := s.apply(cfg); r.Code != 1 {
		t.Errorf("apply exited %d, want 1", r.Code)
	}
	if got := readText(t, filepath.Join(cfg, ".ocx-files")); got != before {
		t.Errorf(".ocx-files = %q after a failed apply, want the previous %q", clip(got), clip(before))
	}

	unlockDir(t, prompts)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("the second apply exited %d: %s", r.Code, r.shown())
	}
	if got := len(listedPaths(t, cfg)); got != len(shippedFiles(t)) {
		t.Errorf(".ocx-files lists %d files after the second apply", got)
	}
}

func TestApplyStopsWhenAFileCannotBeDeletedAndKeepsTheListUntilARetryWorks(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	writeText(t, filepath.Join(cfg, "commands", "retired.md"), "old\n")
	setList(t, cfg, append(listedPaths(t, cfg), "commands/retired.md")...)
	before := readText(t, filepath.Join(cfg, ".ocx-files"))
	commands := filepath.Join(cfg, "commands")
	lockDir(t, commands)

	r := s.apply(cfg)
	if r.Code != 1 {
		t.Errorf("apply exited %d, want 1", r.Code)
	}
	out := r.combined()
	if !strings.Contains(out, "commands/retired.md") {
		t.Errorf("apply's message doesn't name the path that failed: %q", out)
	}
	if !regexp.MustCompile(`(?i)delet`).MatchString(out) {
		t.Errorf("apply's message doesn't name the operation that failed: %q", out)
	}
	if got := readText(t, filepath.Join(cfg, ".ocx-files")); got != before {
		t.Errorf(".ocx-files = %q after a failed apply, want the previous list %q", clip(got), clip(before))
	}

	unlockDir(t, commands)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("the second apply exited %d: %s", r.Code, r.shown())
	}
	if pathExists(filepath.Join(commands, "retired.md")) {
		t.Error("the second apply didn't finish deleting the file")
	}
	for _, line := range listedPaths(t, cfg) {
		if line == "commands/retired.md" {
			t.Error(".ocx-files still lists the deleted file after the second apply")
		}
	}
}

func TestRemoveStopsWhenAFileCannotBeDeletedAndKeepsTheListUntilARetryWorks(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	before := readText(t, filepath.Join(cfg, ".ocx-files"))
	commands := filepath.Join(cfg, "commands")
	lockDir(t, commands)

	r := s.remove(cfg)
	if r.Code != 1 {
		t.Errorf("remove exited %d, want 1", r.Code)
	}
	out := r.combined()
	if !regexp.MustCompile(`commands/[^\s:]+\.md`).MatchString(out) {
		t.Errorf("remove's message doesn't name the path that failed: %q", out)
	}
	if !regexp.MustCompile(`(?i)delet`).MatchString(out) {
		t.Errorf("remove's message doesn't name the operation that failed: %q", out)
	}
	if got := readText(t, filepath.Join(cfg, ".ocx-files")); got != before {
		t.Errorf(".ocx-files = %q after a failed remove, want it left whole", clip(got))
	}

	unlockDir(t, commands)
	if r := s.remove(cfg); r.Code != 0 {
		t.Fatalf("the second remove exited %d: %s", r.Code, r.shown())
	}
	if got := treeState(t, cfg); len(got) != 0 {
		t.Errorf("the second remove left %v", sortedKeys(got))
	}
}
