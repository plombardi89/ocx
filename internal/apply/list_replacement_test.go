package apply

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var listTestFiles = map[string][]byte{
	"AGENTS.md":           []byte("# rules\n"),
	"commands/plan.md":    []byte("plan\n"),
	"skills/a/SKILL.md":   []byte("skill a\n"),
	"prompts/reviewer.md": []byte("review\n"),
}

func listTestMore() map[string][]byte {
	out := map[string][]byte{"skills/b/SKILL.md": []byte("skill b\n")}
	for k, v := range listTestFiles {
		out[k] = v
	}
	return out
}

func readListFile(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ListFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// failWrites makes every write of a path accepted by match fail, until the
// returned function restores the real write.
func failWrites(match func(path string) bool) func() {
	orig := writeFile
	writeFile = func(p string, data []byte, perm os.FileMode) error {
		if match(p) {
			return errors.New("disk full")
		}
		return orig(p, data, perm)
	}
	return func() { writeFile = orig }
}

func isListPath(p string) bool { return strings.HasPrefix(filepath.Base(p), ListFile) }

func TestAFailedWriteOfTheListLeavesThePreviousListWhole(t *testing.T) {
	dir := t.TempDir()
	if _, err := Apply(dir, listTestFiles, false); err != nil {
		t.Fatal(err)
	}
	before := readListFile(t, dir)

	restore := failWrites(isListPath)
	_, err := Apply(dir, listTestMore(), false)
	restore()
	if err == nil {
		t.Fatal("apply succeeded although the list couldn't be written")
	}
	if !strings.Contains(err.Error(), ListFile) || !regexp.MustCompile(`(?i)writ`).MatchString(err.Error()) {
		t.Errorf("the error %q doesn't name the operation and the path", err)
	}
	if got := readListFile(t, dir); got != before {
		t.Errorf("list = %q after the failure, want the previous %q", got, before)
	}

	if _, err := Apply(dir, listTestMore(), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readListFile(t, dir), "skills/b/SKILL.md") {
		t.Error("a second apply didn't finish the job")
	}
}

func TestAFailedRenameOfTheListLeavesThePreviousListWhole(t *testing.T) {
	dir := t.TempDir()
	if _, err := Apply(dir, listTestFiles, false); err != nil {
		t.Fatal(err)
	}
	before := readListFile(t, dir)

	orig := rename
	rename = func(from, to string) error {
		if filepath.Base(to) == ListFile {
			return errors.New("disk full")
		}
		return orig(from, to)
	}
	_, err := Apply(dir, listTestMore(), false)
	rename = orig
	if err == nil {
		t.Fatal("apply succeeded although the list couldn't be put in place")
	}
	if !strings.Contains(err.Error(), ListFile) {
		t.Errorf("the error %q doesn't name the path", err)
	}
	if got := readListFile(t, dir); got != before {
		t.Errorf("list = %q after the failure, want the previous %q", got, before)
	}
}

func TestTheListIsReplacedByRenamingATemporaryFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Apply(dir, listTestFiles, false); err != nil {
		t.Fatal(err)
	}

	var written []string
	type move struct{ from, to string }
	var moves []move
	origWrite, origRename := writeFile, rename
	writeFile = func(p string, data []byte, perm os.FileMode) error {
		written = append(written, p)
		return origWrite(p, data, perm)
	}
	rename = func(from, to string) error {
		moves = append(moves, move{from, to})
		return origRename(from, to)
	}
	_, err := Apply(dir, listTestMore(), false)
	writeFile, rename = origWrite, origRename
	if err != nil {
		t.Fatal(err)
	}

	var toList []move
	for _, m := range moves {
		if filepath.Base(m.to) == ListFile {
			toList = append(toList, m)
		}
	}
	if len(toList) != 1 {
		t.Fatalf("the list was moved into place %d times, want once: %v", len(toList), moves)
	}
	temp := toList[0].from
	if filepath.Base(temp) == ListFile {
		t.Errorf("the list was renamed from itself: %v", toList[0])
	}
	wroteTemp := false
	for _, p := range written {
		if filepath.Base(p) == ListFile {
			t.Errorf("the list was written in place at %s instead of through a temporary file", p)
		}
		if p == temp {
			wroteTemp = true
		}
	}
	if !wroteTemp {
		t.Errorf("the file renamed to the list, %s, was never written first", temp)
	}
	if !strings.Contains(readListFile(t, dir), "skills/b/SKILL.md") {
		t.Error("the new list is missing a file")
	}
}

func TestAFailedWriteOfAFileLeavesThePreviousListWhole(t *testing.T) {
	dir := t.TempDir()
	if _, err := Apply(dir, listTestFiles, false); err != nil {
		t.Fatal(err)
	}
	before := readListFile(t, dir)

	restore := failWrites(func(p string) bool {
		return filepath.Base(p) == "SKILL.md" && strings.HasSuffix(filepath.Dir(p), "skills/b")
	})
	_, err := Apply(dir, listTestMore(), false)
	restore()
	if err == nil {
		t.Fatal("apply succeeded although a file couldn't be written")
	}
	if !strings.Contains(err.Error(), "skills/b/SKILL.md") || !regexp.MustCompile(`(?i)writ`).MatchString(err.Error()) {
		t.Errorf("the error %q doesn't name the operation and the path", err)
	}
	if got := readListFile(t, dir); got != before {
		t.Errorf("list = %q after the failure, want the previous %q", got, before)
	}
}
