package ocx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A stand-in go keeps this check of where make lint installs golangci-lint
// fast and independent of module downloads.
func TestLintInstallsItsToolInACheckoutPathWithSpaces(t *testing.T) {
	skipInNestedRun(t)
	requireMake(t)
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "my checkout")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "Makefile"), makefile, 0o644); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	record := filepath.Join(t.TempDir(), "gobin")
	fakeGo := "#!/bin/sh\n" +
		"printf '%s' \"$GOBIN\" > '" + record + "'\n" +
		"mkdir -p \"$GOBIN\"\n" +
		"printf '#!/bin/sh\\nexit 0\\n' > \"$GOBIN/golangci-lint\"\n" +
		"chmod +x \"$GOBIN/golangci-lint\"\n"
	if err := os.WriteFile(filepath.Join(tools, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), "make", "-s", "lint")
	cmd.Dir = checkout
	cmd.Env = append(makeEnv(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make lint in %q: %v\n%s", checkout, err, out)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("go install never ran: %v", err)
	}
	if want := filepath.Join(checkout, "bin"); strings.TrimSpace(string(got)) != want {
		t.Errorf("GOBIN = %q, want %q", got, want)
	}
}
