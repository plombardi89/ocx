package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/plombardi89/ocx/internal/update"
)

func releaseAsset(t *testing.T) string {
	t.Helper()
	name, err := update.Asset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	return name
}

// serveRelease serves binary as this machine's release asset, with a
// matching SHA256SUMS, sets OCX_RELEASE_URL to it, and counts requests.
func serveRelease(t *testing.T, binary []byte) *atomic.Int32 {
	t.Helper()
	asset := releaseAsset(t)
	sum := sha256.Sum256(binary)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "SHA256SUMS":
			_, _ = w.Write([]byte(sums))
		case asset:
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OCX_RELEASE_URL", srv.URL)
	return &hits
}

// fakeExecutable makes runUpdate replace a file holding "old" in a
// temporary directory instead of the test binary, and returns its path.
func fakeExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ocx")
	if err := os.WriteFile(path, []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := executable
	executable = func() (string, error) { return path, nil }
	t.Cleanup(func() { executable = orig })
	return path
}

func TestUpdateUsageErrorsChangeNothing(t *testing.T) {
	hits := serveRelease(t, []byte("#!/bin/sh\necho new\n"))
	exe := fakeExecutable(t)
	for _, args := range [][]string{
		{"update", "--check", "--apply"},
		{"update", "--bogus"},
		{"update", "extra"},
		{"update", "--config-dir", t.TempDir()},
		{"update", "--check", "--config-dir", t.TempDir()},
	} {
		var out, errOut strings.Builder
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%v exited %d, want 2: %s", args, code, errOut.String())
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("usage errors made %d requests", n)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old\n" {
		t.Errorf("the binary changed to %q", got)
	}
}

func TestUpdateCheckChangesNothing(t *testing.T) {
	hits := serveRelease(t, []byte("#!/bin/sh\necho new\n"))
	exe := fakeExecutable(t)
	var out, errOut strings.Builder
	if code := run([]string{"update", "--check"}, &out, &errOut); code != 0 {
		t.Fatalf("exited %d: %s", code, errOut.String())
	}
	if out.String() != "An update is available.\n" {
		t.Errorf("output = %q", out.String())
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("made %d requests, want only SHA256SUMS", n)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old\n" {
		t.Errorf("the binary changed to %q", got)
	}
}

func TestUpdateApplyPassesTheNewBinarysOutputAndStatusThrough(t *testing.T) {
	standIn := "#!/bin/sh\ncase \"$1\" in\nversion) echo stand-in ;;\napply) echo applied out; echo applied err >&2; exit 7 ;;\nesac\n"
	serveRelease(t, []byte(standIn))
	exe := fakeExecutable(t)
	var out, errOut strings.Builder
	code := run([]string{"update", "--apply"}, &out, &errOut)
	if code != 7 {
		t.Fatalf("exited %d, want 7; stderr: %s", code, errOut.String())
	}
	if !strings.HasSuffix(out.String(), "to stand-in.\napplied out\n") {
		t.Errorf("stdout = %q", out.String())
	}
	if errOut.String() != "applied err\n" {
		t.Errorf("stderr = %q", errOut.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != standIn {
		t.Error("the binary wasn't replaced")
	}
}

// buildStamped builds ocx with the given version and returns its path.
// Without version control information, the binary reports the stamp.
func buildStamped(t *testing.T, stamp string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ocx")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-ldflags", "-X main.commit="+stamp, "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func copyTo(t *testing.T, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "ocx")
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return dst
}

type result struct {
	code        int
	out, errOut string
}

func ocxRun(t *testing.T, bin string, args ...string) result {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = t.TempDir()
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, out.String(), errOut.String()}
}

func TestUpdateReplacesARealBinaryAndAppliesTheNewConfiguration(t *testing.T) {
	releaseAsset(t)
	older, newer := "older-build", "newer-build"
	newBin := buildStamped(t, newer)
	data, err := os.ReadFile(newBin)
	if err != nil {
		t.Fatal(err)
	}
	serveRelease(t, data)
	oldBin := buildStamped(t, older)

	t.Run("an update, then apply", func(t *testing.T) {
		installed := copyTo(t, oldBin)
		cfg := filepath.Join(t.TempDir(), "opencode")
		r := ocxRun(t, installed, "update", "--apply", "--config-dir", cfg)
		if r.code != 0 {
			t.Fatalf("exited %d: %s%s", r.code, r.out, r.errOut)
		}
		if !strings.Contains(r.out, "Updated ocx from "+older+" to "+newer+".") {
			t.Errorf("output: %s", r.out)
		}
		// Only the new binary can print its own version here.
		if !strings.Contains(r.out, "Applied "+newer+" to "+cfg) {
			t.Errorf("the new binary didn't apply: %s", r.out)
		}
		if v := ocxRun(t, installed, "version"); strings.TrimSpace(v.out) != newer {
			t.Errorf("the installed binary reports %q", v.out)
		}
		if _, err := os.Stat(filepath.Join(cfg, "opencode.jsonc")); err != nil {
			t.Errorf("the configuration wasn't applied: %v", err)
		}

		cfg2 := filepath.Join(t.TempDir(), "opencode")
		r = ocxRun(t, installed, "update", "--apply", "--config-dir", cfg2)
		if r.code != 0 || !strings.Contains(r.out, "Already up to date: "+newer) || !strings.Contains(r.out, "Applied "+newer) {
			t.Errorf("an already-current update --apply: %d %s%s", r.code, r.out, r.errOut)
		}
	})

	t.Run("apply fails after the replacement", func(t *testing.T) {
		installed := copyTo(t, oldBin)
		cfg := t.TempDir()
		if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere.md"), filepath.Join(cfg, "AGENTS.md")); err != nil {
			t.Fatal(err)
		}
		r := ocxRun(t, installed, "update", "--apply", "--config-dir", cfg)
		if r.code != 1 || !strings.Contains(r.errOut, "AGENTS.md is a link") {
			t.Errorf("exited %d: %s%s", r.code, r.out, r.errOut)
		}
		if v := ocxRun(t, installed, "version"); strings.TrimSpace(v.out) != newer {
			t.Errorf("after a failed apply, the installed binary reports %q", v.out)
		}
	})
}

func TestAnEmptyConfigDirWithoutApplyIsStillAUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--config-dir", ""},
		{"update", "--check", "--config-dir="},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			hits := serveRelease(t, []byte("#!/bin/sh\necho new-version\n"))
			exe := fakeExecutable(t)
			var out, errOut strings.Builder
			if code := run(args, &out, &errOut); code != 2 {
				t.Errorf("exited %d, want 2; stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("made %d requests, want none", n)
			}
			if got, err := os.ReadFile(exe); err != nil || string(got) != "old\n" {
				t.Errorf("the binary is %q, %v; want it unchanged", got, err)
			}
		})
	}
}
