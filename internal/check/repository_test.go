package check

import (
	"os"
	"strings"
	"testing"

	"github.com/plombardi89/ocx/internal/config"
)

// TestThisRepositoryPassesItsChecks runs the checks on the configuration,
// skills, commands, scenarios, and writing in this repository. With
// OCX_TEST_OPENCODE=1, which make check sets, it also resolves every
// agent's permissions with OpenCode and runs the permission probes.
func TestThisRepositoryPassesItsChecks(t *testing.T) {
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Run(root, Options{Offline: os.Getenv("OCX_TEST_OPENCODE") != "1"}) {
		switch r.Status {
		case Fail:
			t.Errorf("%s:\n%s", r.Name, strings.Join(r.Details, "\n"))
		case Warn, Skip:
			t.Logf("%s %s: %s", r.Status, r.Name, strings.Join(r.Details, "; "))
		}
	}
}
