package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestApplyWritesEveryShippedFileAsARegularFile(t *testing.T) {
	shipped := shippedFiles(t)
	for _, want := range []string{"opencode.jsonc", "AGENTS.md"} {
		if _, ok := shipped[want]; !ok {
			t.Fatalf("the repository has no %s to ship", want)
		}
	}
	for _, prefix := range []string{"prompts/", "skills/", "commands/"} {
		shippedPathWithPrefix(t, shipped, prefix)
	}

	s := newSandbox(t)
	cfg := newConfigDir(t)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	for rel, want := range shipped {
		path := filepath.Join(cfg, filepath.FromSlash(rel))
		fi, err := os.Lstat(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !fi.Mode().IsRegular() {
			t.Errorf("%s is not a regular file (mode %v)", rel, fi.Mode())
			continue
		}
		if got := readText(t, path); got != want {
			t.Errorf("%s differs from the file in the repository", rel)
		}
	}
	for rel := range treeState(t, cfg) {
		if strings.HasSuffix(rel, "/") || rel == ".ocx-files" {
			continue
		}
		if _, ok := shipped[rel]; !ok {
			t.Errorf("apply wrote %s, which isn't a shipped file", rel)
		}
	}
}

func TestApplyUsesXDGConfigHomeWhenItIsSet(t *testing.T) {
	xdg := t.TempDir()
	s := newSandbox(t, "XDG_CONFIG_HOME="+xdg)
	if r := s.run("apply"); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	for _, rel := range []string{"opencode.jsonc", "AGENTS.md", ".ocx-files"} {
		if !pathExists(filepath.Join(xdg, "opencode", rel)) {
			t.Errorf("%s was not written under $XDG_CONFIG_HOME/opencode", rel)
		}
	}
	if pathExists(filepath.Join(s.home, ".config")) {
		t.Error("apply wrote under the home directory although XDG_CONFIG_HOME was set")
	}
	if r := s.run("remove"); r.Code != 0 {
		t.Fatalf("remove exited %d: %s", r.Code, r.shown())
	}
	if pathExists(filepath.Join(xdg, "opencode", "opencode.jsonc")) {
		t.Error("remove didn't delete the files from $XDG_CONFIG_HOME/opencode")
	}
}

func TestApplyFallsBackToDotConfigInTheHomeDirectory(t *testing.T) {
	s := newSandbox(t)
	if r := s.run("apply"); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	for _, rel := range []string{"opencode.jsonc", "AGENTS.md", ".ocx-files"} {
		if !pathExists(filepath.Join(s.home, ".config", "opencode", rel)) {
			t.Errorf("%s was not written under ~/.config/opencode", rel)
		}
	}
	if r := s.run("remove"); r.Code != 0 {
		t.Fatalf("remove exited %d: %s", r.Code, r.shown())
	}
	if pathExists(filepath.Join(s.home, ".config", "opencode", "opencode.jsonc")) {
		t.Error("remove didn't delete the files from ~/.config/opencode")
	}
}

func TestConfigDirFlagChoosesTheDirectory(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	if !pathExists(filepath.Join(cfg, "opencode.jsonc")) {
		t.Error("apply didn't write into --config-dir")
	}
	if pathExists(filepath.Join(s.home, ".config", "opencode")) {
		t.Error("apply also wrote into the default directory")
	}
}

func TestApplyOverwritesFilesAlreadyThere(t *testing.T) {
	shipped := shippedFiles(t)
	cfg := newConfigDir(t)
	existing := []string{
		"opencode.jsonc",
		"AGENTS.md",
		shippedPathWithPrefix(t, shipped, "prompts/"),
		shippedPathWithPrefix(t, shipped, "skills/"),
		shippedPathWithPrefix(t, shipped, "commands/"),
	}
	for _, rel := range existing {
		writeText(t, filepath.Join(cfg, filepath.FromSlash(rel)), "content from before\n")
	}
	s := newSandbox(t)
	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	for _, rel := range existing {
		if got := readText(t, filepath.Join(cfg, filepath.FromSlash(rel))); got != shipped[rel] {
			t.Errorf("%s was not overwritten with the shipped content", rel)
		}
	}
}

func TestApplyTwiceLeavesTheSameFiles(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	first := treeState(t, cfg)
	s.install(cfg)
	requireUnchanged(t, "the configuration directory after a second apply", first, treeState(t, cfg))
}

func TestApplyAcceptsAConfigDirThatIsALinkAndKeepsTheLink(t *testing.T) {
	s := newSandbox(t)
	base := t.TempDir()
	resolved := filepath.Join(base, "resolved")
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	makeLink(t, resolved, link)

	if r := s.apply(link); r.Code != 0 {
		t.Fatalf("apply through a linked configuration directory exited %d: %s", r.Code, r.shown())
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the configuration directory is no longer a link: %v", err)
	}
	for rel := range shippedFiles(t) {
		fi, err := os.Lstat(filepath.Join(resolved, filepath.FromSlash(rel)))
		if err != nil || !fi.Mode().IsRegular() {
			t.Errorf("%s was not written as a regular file in the linked directory: %v", rel, err)
		}
	}
	if r := s.remove(link); r.Code != 0 {
		t.Fatalf("remove through a linked configuration directory exited %d: %s", r.Code, r.shown())
	}
	if pathExists(filepath.Join(resolved, "opencode.jsonc")) || pathExists(filepath.Join(resolved, ".ocx-files")) {
		t.Error("remove left files in the linked directory")
	}
}

func TestApplyListsEveryFileItWroteInOCXFiles(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)

	data := readText(t, filepath.Join(cfg, ".ocx-files"))
	lines := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	listed := map[string]bool{}
	for _, line := range lines {
		if line == "" {
			t.Errorf(".ocx-files has an empty line: %q", data)
		}
		if listed[line] {
			t.Errorf(".ocx-files lists %s twice", line)
		}
		listed[line] = true
	}
	shipped := shippedFiles(t)
	for rel := range shipped {
		if !listed[rel] {
			t.Errorf(".ocx-files doesn't list %s", rel)
		}
	}
	for rel := range listed {
		if _, ok := shipped[rel]; !ok {
			t.Errorf(".ocx-files lists %s, which apply didn't write", rel)
		}
	}
}

func TestApplyDeletesListedFilesItNoLongerShipsAndTheEmptyDirectoriesTheyLeave(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)

	stale := []string{
		"skills/retired/SKILL.md",
		"skills/retired/references/notes.md",
		"commands/retired.md",
		"skills/shared/SKILL.md",
	}
	for _, rel := range stale {
		writeText(t, filepath.Join(cfg, filepath.FromSlash(rel)), "old\n")
	}
	writeText(t, filepath.Join(cfg, "skills", "shared", "mine.txt"), "mine\n")
	writeText(t, filepath.Join(cfg, "commands", "mine.md"), "mine\n")
	writeText(t, filepath.Join(cfg, "skills", "unlisted", "SKILL.md"), "never listed\n")
	setList(t, cfg, append(listedPaths(t, cfg), stale...)...)

	if r := s.apply(cfg); r.Code != 0 {
		t.Fatalf("apply exited %d: %s", r.Code, r.shown())
	}
	for _, rel := range stale {
		if pathExists(filepath.Join(cfg, filepath.FromSlash(rel))) {
			t.Errorf("%s is no longer shipped but was not deleted", rel)
		}
	}
	for _, dir := range []string{"skills/retired", "skills/retired/references"} {
		if pathExists(filepath.Join(cfg, filepath.FromSlash(dir))) {
			t.Errorf("the empty directory %s was not deleted", dir)
		}
	}
	if got := readText(t, filepath.Join(cfg, "skills", "shared", "mine.txt")); got != "mine\n" {
		t.Errorf("a file that was never listed was changed: %q", got)
	}
	if got := readText(t, filepath.Join(cfg, "commands", "mine.md")); got != "mine\n" {
		t.Errorf("a file that was never listed was changed: %q", got)
	}
	if got := readText(t, filepath.Join(cfg, "skills", "unlisted", "SKILL.md")); got != "never listed\n" {
		t.Errorf("a file that was never listed was changed: %q", got)
	}
	shipped := shippedFiles(t)
	for rel, want := range shipped {
		if got := readText(t, filepath.Join(cfg, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s is missing or changed after the second apply", rel)
		}
	}
	for _, line := range listedPaths(t, cfg) {
		for _, rel := range stale {
			if line == rel {
				t.Errorf(".ocx-files still lists the deleted file %s", rel)
			}
		}
	}
	if got, want := len(listedPaths(t, cfg)), len(shipped); got != want {
		t.Errorf(".ocx-files lists %d files, want the %d shipped files", got, want)
	}
}

func TestApplySkipsListedFilesThatAreAlreadyGone(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	setList(t, cfg, append(listedPaths(t, cfg), "skills/ghost/SKILL.md", "commands/ghost.md")...)

	r := s.apply(cfg)
	if r.Code != 0 {
		t.Fatalf("apply exited %d with listed files already gone: %s", r.Code, r.shown())
	}
	for _, line := range listedPaths(t, cfg) {
		if strings.Contains(line, "ghost") {
			t.Errorf(".ocx-files still lists %s", line)
		}
	}
}

func TestApplyNeverDeletesTheContentsOfAListedDirectory(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	writeText(t, filepath.Join(cfg, "skills", "mine", "note.txt"), "mine\n")
	setList(t, cfg, append(listedPaths(t, cfg), "skills")...)
	before := treeState(t, filepath.Join(cfg, "skills"))

	s.apply(cfg)
	requireUnchanged(t, "the skills directory", before, treeState(t, filepath.Join(cfg, "skills")))
}

func TestDryRunApplyOnAFreshDirectoryPrintsEveryWriteAndCreatesNothing(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	r := s.apply(cfg, "--dry-run")
	if r.Code != 0 {
		t.Fatalf("apply --dry-run exited %d: %s", r.Code, r.shown())
	}
	if pathExists(cfg) {
		t.Fatalf("a dry run created %s", cfg)
	}
	for _, rel := range sortedKeys(shippedFiles(t)) {
		line := lineContaining(r.Stdout, rel)
		if line == "" {
			t.Errorf("the dry run doesn't print %s", rel)
		} else if !regexp.MustCompile(`(?i)write`).MatchString(line) {
			t.Errorf("the line for %s doesn't say it would be written: %q", rel, line)
		}
	}
}

func TestDryRunApplyPrintsDeletionsAndChangesNothing(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	writeText(t, filepath.Join(cfg, "commands", "retired.md"), "old\n")
	writeText(t, filepath.Join(cfg, "AGENTS.md"), "edited by hand\n")
	setList(t, cfg, append(listedPaths(t, cfg), "commands/retired.md")...)
	before := treeState(t, cfg)

	r := s.apply(cfg, "--dry-run")
	if r.Code != 0 {
		t.Fatalf("apply --dry-run exited %d: %s", r.Code, r.shown())
	}
	line := lineContaining(r.Stdout, "commands/retired.md")
	if line == "" || !regexp.MustCompile(`(?i)delete`).MatchString(line) {
		t.Errorf("the dry run doesn't say it would delete commands/retired.md: %q", r.Stdout)
	}
	if line := lineContaining(r.Stdout, "AGENTS.md"); !regexp.MustCompile(`(?i)write`).MatchString(line) {
		t.Errorf("the dry run doesn't say it would write AGENTS.md: %q", line)
	}
	requireUnchanged(t, "the configuration directory after a dry run", before, treeState(t, cfg))
}

func lineContaining(out, needle string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func TestRemoveDeletesWhatApplyWroteAndNothingElse(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	writeText(t, filepath.Join(cfg, "tui.jsonc"), "{}\n")
	writeText(t, filepath.Join(cfg, "commands", "mine.md"), "mine\n")
	writeText(t, filepath.Join(cfg, "skills", "mine", "SKILL.md"), "mine\n")
	s.install(cfg)

	if r := s.remove(cfg); r.Code != 0 {
		t.Fatalf("remove exited %d: %s", r.Code, r.shown())
	}
	got := treeState(t, cfg)
	want := map[string]string{
		"tui.jsonc":            "{}\n",
		"commands/":            "",
		"commands/mine.md":     "mine\n",
		"skills/":              "",
		"skills/mine/":         "",
		"skills/mine/SKILL.md": "mine\n",
	}
	requireUnchanged(t, "the directory after remove, compared with the user's own files", want, got)
}

func TestRemoveDeletesTheListAndTheDirectoriesItEmpties(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	if r := s.remove(cfg); r.Code != 0 {
		t.Fatalf("remove exited %d: %s", r.Code, r.shown())
	}
	if pathExists(filepath.Join(cfg, ".ocx-files")) {
		t.Error("remove left .ocx-files behind")
	}
	if got := treeState(t, cfg); len(got) != 0 {
		t.Errorf("remove left %v", sortedKeys(got))
	}
}

func TestRemoveWithoutAListSaysThereIsNothingToRemove(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	writeText(t, filepath.Join(cfg, "tui.jsonc"), "{}\n")
	writeText(t, filepath.Join(cfg, "commands", "mine.md"), "mine\n")
	before := treeState(t, cfg)

	r := s.remove(cfg)
	if !regexp.MustCompile(`(?i)nothing to remove`).MatchString(r.combined()) {
		t.Errorf("remove without .ocx-files said %q, want it to say there's nothing to remove", r.shown())
	}
	requireUnchanged(t, "the directory", before, treeState(t, cfg))

	missing := newConfigDir(t)
	r = s.remove(missing)
	if !regexp.MustCompile(`(?i)nothing to remove`).MatchString(r.combined()) {
		t.Errorf("remove in a directory that doesn't exist said %q", r.shown())
	}
	if pathExists(missing) {
		t.Error("remove created the configuration directory")
	}
}

func TestRemoveSkipsListedFilesThatAreAlreadyGone(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	if err := os.Remove(filepath.Join(cfg, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	removeTree(t, filepath.Join(cfg, "skills"))

	if r := s.remove(cfg); r.Code != 0 {
		t.Fatalf("remove exited %d with listed files already gone: %s", r.Code, r.shown())
	}
	if got := treeState(t, cfg); len(got) != 0 {
		t.Errorf("remove left %v", sortedKeys(got))
	}
}

func TestRemoveDeletesOnlyListedFiles(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	var kept []string
	for _, line := range listedPaths(t, cfg) {
		if line != "AGENTS.md" {
			kept = append(kept, line)
		}
	}
	sort.Strings(kept)
	setList(t, cfg, kept...)

	if r := s.remove(cfg); r.Code != 0 {
		t.Fatalf("remove exited %d: %s", r.Code, r.shown())
	}
	if got := readText(t, filepath.Join(cfg, "AGENTS.md")); got != shippedFiles(t)["AGENTS.md"] {
		t.Error("remove changed or deleted a file that wasn't listed")
	}
	if got := treeState(t, cfg); len(got) != 1 {
		t.Errorf("remove left %v, want only AGENTS.md", sortedKeys(got))
	}
}

func TestRemoveNeverDeletesTheContentsOfAListedDirectory(t *testing.T) {
	s := newSandbox(t)
	cfg := newConfigDir(t)
	s.install(cfg)
	writeText(t, filepath.Join(cfg, "skills", "mine", "note.txt"), "mine\n")
	setList(t, cfg, "skills")
	before := treeState(t, filepath.Join(cfg, "skills"))

	s.remove(cfg)
	requireUnchanged(t, "the skills directory", before, treeState(t, filepath.Join(cfg, "skills")))
}
