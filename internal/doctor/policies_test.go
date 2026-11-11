package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/check"
	"github.com/plombardi89/ocx/internal/perm"
)

func TestOtherPoliciesSkipsOnlyOcxsOwnFile(t *testing.T) {
	repoConfig := filepath.Join(t.TempDir(), "opencode.jsonc")
	if err := os.WriteFile(repoConfig, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(t.TempDir(), "opencode.jsonc")
	if err := os.Symlink(repoConfig, installed); err != nil {
		t.Fatal(err)
	}
	deny := []perm.Rule{{Action: "tool.use", Resource: "shell:go test *", Effect: perm.Deny}}
	source := func(typ, path string, policies []perm.Rule) check.ConfigSource {
		var s check.ConfigSource
		s.Type, s.Path, s.Info.Policies = typ, path, policies
		return s
	}
	got := otherPolicies([]check.ConfigSource{
		source("document", installed, deny),
		source("document", "/etc/opencode/opencode.json", deny),
		source("document", "/home/u/.config/opencode/opencode.json", nil),
		source("directory", "/home/u/.config/opencode", nil),
	}, repoConfig)
	if len(got) != 1 || !strings.Contains(got[0], "/etc/opencode/opencode.json") {
		t.Fatalf("otherPolicies = %q", got)
	}
}
