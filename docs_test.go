package ocx

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// paragraphs splits Markdown into blocks: a block ends at a blank line and
// each list item starts a new block. Whitespace in a block is collapsed to
// single spaces.
func paragraphs(text string) []string {
	item := regexp.MustCompile(`^\s*(?:\d+\.|[-*])\s`)
	var out, current []string
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(strings.Fields(strings.Join(current, " ")), " "))
			current = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case item.MatchString(line):
			flush()
			current = append(current, line)
		default:
			current = append(current, line)
		}
	}
	flush()
	return out
}

func anyParagraph(text string, wants ...*regexp.Regexp) bool {
	for _, p := range paragraphs(text) {
		all := true
		for _, w := range wants {
			if !w.MatchString(p) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func TestREADMEExplainsInstallingWithCurlOrGoInstall(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	curlLatest := regexp.MustCompile(`curl[^\n]*github\.com/plombardi89/ocx/releases[^\n]*latest|curl[^\n]*latest[^\n]*github\.com/plombardi89/ocx/releases`)
	if !curlLatest.MatchString(readme) {
		t.Error("the README has no curl command that downloads from the latest release of github.com/plombardi89/ocx")
	}
	if !strings.Contains(readme, "go install github.com/plombardi89/ocx/cmd/ocx@main") {
		t.Error("the README doesn't give the go install command for github.com/plombardi89/ocx/cmd/ocx@main")
	}
	if !anyParagraph(readme, regexp.MustCompile("`ocx apply`")) {
		t.Error("the README never tells the reader to run ocx apply")
	}
}

func TestREADMEExplainsUpgradingAndRemoving(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	if !anyParagraph(readme, regexp.MustCompile(`(?i)upgrad`), regexp.MustCompile(`(?i)\bbinary\b`), regexp.MustCompile("`ocx apply`"), regexp.MustCompile(`(?i)\bagain\b`)) {
		t.Error("the README doesn't say that upgrading means getting the new binary and running ocx apply again")
	}
	if !anyParagraph(readme, regexp.MustCompile(`(?i)remov`), regexp.MustCompile("`ocx remove`")) {
		t.Error("the README doesn't explain how to remove it with ocx remove")
	}
}

func TestAGENTSHasNoInstructionsThatUseRemovedCommands(t *testing.T) {
	agents := readRepoFile(t, "AGENTS.md")
	removed := regexp.MustCompile(`(?i)(\bocx|cmd/ocx)\s+(install|uninstall|doctor|check|eval)\b|\bmake\s+(install|uninstall|doctor)\b`)
	for i, line := range strings.Split(agents, "\n") {
		if removed.MatchString(line) {
			t.Errorf("AGENTS.md line %d uses a removed command: %q", i+1, line)
		}
	}
}

func TestTheCodeOnlyTheRemovedCommandsUsedIsGone(t *testing.T) {
	for _, dir := range []string{"install", "doctor"} {
		if _, err := os.Stat(filepath.Join("internal", dir)); err == nil {
			t.Errorf("internal/%s still exists", dir)
		}
	}
}

func TestTheOcxBinaryDoesNotLinkTheCheckOrEvalCode(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./cmd/ocx").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		for _, banned := range []string{"internal/check", "internal/eval", "internal/install", "internal/doctor"} {
			if strings.HasSuffix(pkg, banned) {
				t.Errorf("the ocx binary still includes %s", pkg)
			}
		}
	}
}
