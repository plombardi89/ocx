package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plombardi89/ocx/internal/check"
	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/install"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := config.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGitHubRepo(t *testing.T) {
	cases := map[string]string{
		"https://github.com/plombardi89/ocx.git": "plombardi89/ocx",
		"https://github.com/plombardi89/ocx":     "plombardi89/ocx",
		"git@github.com:plombardi89/ocx.git":     "plombardi89/ocx",
		"ssh://git@github.com/a/b.c.git":         "a/b.c",
		"https://gitlab.com/a/b.git":             "",
		"/tmp/origin.git":                        "",
	}
	for url, want := range cases {
		owner, name, ok := GitHubRepo(url)
		got := ""
		if ok {
			got = owner + "/" + name
		}
		if got != want {
			t.Errorf("GitHubRepo(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestCheckInstall(t *testing.T) {
	root, dir := repoRoot(t), t.TempDir()
	if r := checkInstall(root, dir); r.Status != check.Fail || !strings.Contains(strings.Join(r.Details, " "), "not installed") {
		t.Fatalf("before install: %+v", r)
	}
	p, err := install.PlanInstall(root, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := install.Apply(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r := checkInstall(root, dir); r.Status != check.Pass {
		t.Fatalf("after install: %+v", r)
	}
	if err := os.Remove(filepath.Join(dir, "prompts")); err != nil {
		t.Fatal(err)
	}
	if r := checkInstall(root, dir); r.Status != check.Fail || !strings.Contains(strings.Join(r.Details, " "), "prompts") {
		t.Fatalf("missing prompts link: %+v", r)
	}
}

func TestOfflineDoctorDoesNotContactGitHub(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "gh-calls")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + calls + "\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	results := Run(Options{Root: repoRoot(t), ConfigDir: t.TempDir(), Offline: true})
	if data, _ := os.ReadFile(calls); len(data) != 0 {
		t.Fatalf("offline doctor ran gh:\n%s", data)
	}
	for _, r := range results {
		if r.Name == "gh" && r.Status != check.Skip {
			t.Errorf("gh result offline: %+v", r)
		}
	}
}

func TestDoctorFailsWhenAnotherFileSetsPolicies(t *testing.T) {
	if os.Getenv("OCX_TEST_OPENCODE") != "1" {
		t.Skip("runs opencode; set OCX_TEST_OPENCODE=1, as make check does")
	}
	root, dir := repoRoot(t), filepath.Join(t.TempDir(), "opencode")
	p, err := install.PlanInstall(root, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := install.Apply(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	opts := Options{Root: root, ConfigDir: dir}
	if r := checkLoaded(opts); r.Status != check.Pass {
		t.Fatalf("plain install: %+v", r)
	}

	extra := `{"policies": [{"action": "tool.use", "resource": "shell:go test *", "effect": "deny"}]}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	r := checkLoaded(opts)
	if r.Status != check.Fail || !strings.Contains(strings.Join(r.Details, "\n"), filepath.Join(dir, "opencode.json")) {
		t.Fatalf("install with another file's policies: %+v", r)
	}
}

func TestMissingGitHubProtectionIsOnlyAWarning(t *testing.T) {
	if r := protectionResult("protection", "main", "a/b", []string{"deletion"}); r.Status != check.Warn {
		t.Fatalf("missing rules: %+v", r)
	}
	if r := protectionResult("protection", "main", "a/b", []string{"deletion", "non_fast_forward", "pull_request", "required_status_checks"}); r.Status != check.Pass {
		t.Fatalf("all rules: %+v", r)
	}
}
