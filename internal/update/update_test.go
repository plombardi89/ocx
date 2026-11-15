package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	oldBinary = "#!/bin/sh\necho old-version\n"
	newBinary = "#!/bin/sh\necho new-version\necho more detail\n"
)

func hexSum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// release serves files by name and counts the requests for each.
type release struct {
	t     *testing.T
	mu    sync.Mutex
	files map[string]http.HandlerFunc
	hits  map[string]int
	srv   *httptest.Server
}

func newRelease(t *testing.T) *release {
	r := &release{t: t, files: map[string]http.HandlerFunc{}, hits: map[string]int{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		r.mu.Lock()
		r.hits[name]++
		h, ok := r.files[name]
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		h(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *release) serve(name, body string) {
	r.files[name] = func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }
}

func (r *release) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[name]
}

// standard serves newBinary as this machine's asset, with a matching
// SHA256SUMS, and returns options that would replace a copy of oldBinary.
func standard(t *testing.T) (*release, Options) {
	t.Helper()
	r := newRelease(t)
	r.serve("ocx-linux-amd64", newBinary)
	r.serve("SHA256SUMS", hexSum(newBinary)+"  ocx-linux-amd64\n"+hexSum("arm")+"  ocx-linux-arm64\n")
	dir := t.TempDir()
	exe := filepath.Join(dir, "ocx")
	if err := os.WriteFile(exe, []byte(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	return r, Options{Location: r.srv.URL, Executable: exe, GOOS: "linux", GOARCH: "amd64"}
}

// unchanged fails the test unless the binary still holds oldBinary and no
// temporary file is left next to it.
func unchanged(t *testing.T, exe string) {
	t.Helper()
	if got, err := os.ReadFile(exe); err != nil || string(got) != oldBinary {
		t.Errorf("the old binary changed: %q, %v", got, err)
	}
	noTemp(t, filepath.Dir(exe))
}

func noTemp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ocx-update-") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want one containing %q", err, want)
	}
}

func TestReplacesTheBinaryWithTheReleaseBuild(t *testing.T) {
	_, opts := standard(t)
	res, err := Run(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.UpToDate || res.NewVersion != "new-version" || res.Path != opts.Executable {
		t.Errorf("result = %+v", res)
	}
	got, _ := os.ReadFile(opts.Executable)
	if string(got) != newBinary {
		t.Errorf("binary = %q", got)
	}
	if fi, _ := os.Stat(opts.Executable); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	noTemp(t, filepath.Dir(opts.Executable))
}

func TestAnUpToDateBinaryDownloadsNothingElse(t *testing.T) {
	r, opts := standard(t)
	r.serve("SHA256SUMS", hexSum(oldBinary)+"  ocx-linux-amd64\n")
	for _, check := range []bool{false, true} {
		res, err := Run(opts, check)
		if err != nil || !res.UpToDate {
			t.Fatalf("check=%v: %+v, %v", check, res, err)
		}
	}
	if n := r.count("ocx-linux-amd64"); n != 0 {
		t.Errorf("the binary was downloaded %d times", n)
	}
	unchanged(t, opts.Executable)
}

func TestCheckReportsAnUpdateWithoutDownloadingIt(t *testing.T) {
	r, opts := standard(t)
	res, err := Run(opts, true)
	if err != nil || res.UpToDate {
		t.Fatalf("%+v, %v", res, err)
	}
	if n := r.count("ocx-linux-amd64"); n != 0 {
		t.Errorf("the binary was downloaded %d times", n)
	}
	unchanged(t, opts.Executable)
}

func TestFailedDownloadsLeaveTheBinaryAlone(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *release, opts *Options)
		want  string
	}{
		{"an HTTP 404", func(r *release, _ *Options) { delete(r.files, "ocx-linux-amd64") }, "HTTP 404"},
		{"a download cut off part way", func(r *release, _ *Options) {
			r.files["ocx-linux-amd64"] = func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprint(len(newBinary)+100))
				fmt.Fprint(w, newBinary)
			}
		}, "unexpected EOF"},
		{"a download over the limit", func(r *release, opts *Options) {
			big := strings.Repeat("x", 300)
			r.serve("ocx-linux-amd64", big)
			r.serve("SHA256SUMS", hexSum(big)+"  ocx-linux-amd64\n")
			opts.MaxDownload = 200
		}, "larger than"},
		{"a timeout", func(r *release, opts *Options) {
			r.files["ocx-linux-amd64"] = func(w http.ResponseWriter, req *http.Request) {
				select {
				case <-time.After(5 * time.Second):
				case <-req.Context().Done():
				}
			}
			opts.Client = &http.Client{Timeout: 200 * time.Millisecond}
		}, "Timeout"},
		{"a checksum mismatch", func(r *release, _ *Options) { r.serve("ocx-linux-amd64", newBinary+"tampered") }, "doesn't match"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, opts := standard(t)
			c.setup(r, &opts)
			_, err := Run(opts, false)
			wantErr(t, err, c.want)
			unchanged(t, opts.Executable)
		})
	}
}

func TestSHA256SUMSNeedsExactlyOneValidLineForTheBinary(t *testing.T) {
	cases := map[string]string{
		"no line":            hexSum("arm") + "  ocx-linux-arm64\n",
		"two lines":          hexSum(newBinary) + "  ocx-linux-amd64\n" + hexSum("x") + "  ocx-linux-amd64\n",
		"an invalid sum":     "not-a-checksum  ocx-linux-amd64\n",
		"one space":          hexSum(newBinary) + " ocx-linux-amd64\n",
		"a short sum":        hexSum(newBinary)[:63] + "  ocx-linux-amd64\n",
		"binary-mode marker": hexSum(newBinary) + " *ocx-linux-amd64\n",
	}
	for name, sums := range cases {
		t.Run(name, func(t *testing.T) {
			r, opts := standard(t)
			r.serve("SHA256SUMS", sums)
			for _, check := range []bool{true, false} {
				if _, err := Run(opts, check); err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
					t.Errorf("check=%v: err = %v", check, err)
				}
			}
			if n := r.count("ocx-linux-amd64"); n != 0 {
				t.Errorf("the binary was downloaded %d times", n)
			}
			unchanged(t, opts.Executable)
		})
	}
}

func TestANewBinaryWhoseVersionDoesntWorkIsRejected(t *testing.T) {
	cases := []struct {
		name, binary, want string
	}{
		{"fails", "#!/bin/sh\necho broken >&2\nexit 3\n", "failed"},
		{"hangs", "#!/bin/sh\nexec sleep 5\n", "took longer than"},
		{"prints nothing", "#!/bin/sh\nexit 0\n", "printed nothing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, opts := standard(t)
			r.serve("ocx-linux-amd64", c.binary)
			r.serve("SHA256SUMS", hexSum(c.binary)+"  ocx-linux-amd64\n")
			opts.VersionTimeout = 300 * time.Millisecond
			start := time.Now()
			_, err := Run(opts, false)
			wantErr(t, err, c.want)
			if time.Since(start) > 3*time.Second {
				t.Errorf("took %v", time.Since(start))
			}
			unchanged(t, opts.Executable)
		})
	}
}

func TestADirectoryThatCantBeWrittenToIsNamed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	_, opts := standard(t)
	dir := filepath.Dir(opts.Executable)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err := Run(opts, false)
	wantErr(t, err, dir)
	wantErr(t, err, "~/.local/bin")
	unchanged(t, opts.Executable)
}

func TestFailedWritesAndRenamesLeaveTheBinaryAlone(t *testing.T) {
	t.Run("writing the new binary", func(t *testing.T) {
		_, opts := standard(t)
		orig := writeAll
		writeAll = func(f *os.File, data []byte) error {
			_, _ = f.Write(data[:len(data)/2])
			return errors.New("no space left on device")
		}
		defer func() { writeAll = orig }()
		_, err := Run(opts, false)
		wantErr(t, err, "write the new ocx")
		unchanged(t, opts.Executable)
	})
	t.Run("renaming it into place", func(t *testing.T) {
		_, opts := standard(t)
		orig := rename
		rename = func(string, string) error { return errors.New("device busy") }
		defer func() { rename = orig }()
		_, err := Run(opts, false)
		wantErr(t, err, "replace "+opts.Executable)
		unchanged(t, opts.Executable)
	})
}

func TestAnUnsupportedSystemDownloadsNothing(t *testing.T) {
	r, opts := standard(t)
	opts.GOOS, opts.GOARCH = "darwin", "arm64"
	_, err := Run(opts, false)
	wantErr(t, err, "darwin/arm64")
	if n := r.count("SHA256SUMS"); n != 0 {
		t.Errorf("SHA256SUMS was downloaded %d times", n)
	}
	unchanged(t, opts.Executable)
}

func TestReplacesTheFileALinkPointsTo(t *testing.T) {
	_, opts := standard(t)
	real := opts.Executable
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "ocx")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(linkDir, "other"), filepath.Join(filepath.Dir(real), "other")} {
		if err := os.WriteFile(f, []byte("other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts.Executable = link
	res, err := Run(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != real {
		t.Errorf("replaced %s, want %s", res.Path, real)
	}
	if target, err := os.Readlink(link); err != nil || target != real {
		t.Errorf("the link now points to %q, %v", target, err)
	}
	if got, _ := os.ReadFile(real); string(got) != newBinary {
		t.Errorf("the linked file = %q", got)
	}
	for _, dir := range []string{linkDir, filepath.Dir(real)} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 2 {
			t.Errorf("%s holds %d entries, want ocx and other", dir, len(entries))
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "other")); string(got) != "other\n" {
			t.Errorf("%s/other changed", dir)
		}
	}
}

func TestLocation(t *testing.T) {
	if got := Location(""); got != "https://github.com/plombardi89/ocx/releases/latest/download" {
		t.Errorf("default = %s", got)
	}
	if got := Location("http://mirror.example/ocx/"); got != "http://mirror.example/ocx" {
		t.Errorf("trailing slash: %s", got)
	}
}

func TestATrailingSlashInTheLocationStillFindsTheFiles(t *testing.T) {
	r, opts := standard(t)
	opts.Location = Location(r.srv.URL + "/")
	if _, err := Run(opts, false); err != nil {
		t.Fatal(err)
	}
	if r.count("SHA256SUMS") != 1 || r.count("ocx-linux-amd64") != 1 {
		t.Errorf("requests: %v", r.hits)
	}
}
