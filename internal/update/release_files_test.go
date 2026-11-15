package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

const relRunning = "#!/bin/sh\necho running-build\n"

func relHex(data string) string {
	s := sha256.Sum256([]byte(data))
	return hex.EncodeToString(s[:])
}

// relLine is one line of SHA256SUMS in sha256sum's format.
func relLine(name, data string) string { return relHex(data) + "  " + name + "\n" }

// relServer serves fixed bodies by URL path and records every request path.
type relServer struct {
	mu    sync.Mutex
	files map[string]string
	paths []string
	srv   *httptest.Server
}

func newRelServer(t *testing.T) *relServer {
	t.Helper()
	s := &relServer{files: map[string]string{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		body, ok := s.files[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *relServer) put(path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = body
}

func (s *relServer) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func (s *relServer) hits(path string) int {
	n := 0
	for _, p := range s.requested() {
		if p == path {
			n++
		}
	}
	return n
}

// relBinaryIn writes a running binary called ocx into a new directory.
func relBinaryIn(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "ocx")
	if err := os.WriteFile(exe, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exe, mode); err != nil {
		t.Fatal(err)
	}
	return exe
}

func relRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func relNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// relUntouched fails the test unless the binary still holds content and is
// the only file in its directory.
func relUntouched(t *testing.T, exe, content string) {
	t.Helper()
	if got := relRead(t, exe); got != content {
		t.Errorf("the running binary changed to %q", got)
	}
	if got := relNames(t, filepath.Dir(exe)); !reflect.DeepEqual(got, []string{"ocx"}) {
		t.Errorf("the binary's directory holds %v, want only ocx", got)
	}
}

func TestRunFetchesTheFilesNamedByTheLocationAndTheMachine(t *testing.T) {
	amd, arm := "#!/bin/sh\necho amd64-build\n", "#!/bin/sh\necho arm64-build\n"
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			s := newRelServer(t)
			s.put("/mirror/ocx/SHA256SUMS", relLine("ocx-linux-arm64", arm)+relLine("ocx-linux-amd64", amd))
			s.put("/mirror/ocx/ocx-linux-amd64", amd)
			s.put("/mirror/ocx/ocx-linux-arm64", arm)
			exe := relBinaryIn(t, relRunning, 0o755)
			opts := Options{Location: Location(s.srv.URL + "/mirror/ocx/"), Executable: exe, GOOS: "linux", GOARCH: arch}
			if _, err := Run(opts, false); err != nil {
				t.Fatal(err)
			}
			want := []string{"/mirror/ocx/SHA256SUMS", "/mirror/ocx/ocx-linux-" + arch}
			if got := s.requested(); !reflect.DeepEqual(got, want) {
				t.Errorf("requests = %v, want %v", got, want)
			}
			wantBinary := amd
			if arch == "arm64" {
				wantBinary = arm
			}
			if got := relRead(t, exe); got != wantBinary {
				t.Errorf("the binary is now %q, want the %s build %q", got, arch, wantBinary)
			}
		})
	}
}

func TestLocationIsTheLatestReleaseUnlessSet(t *testing.T) {
	if got, want := Location(""), "https://github.com/plombardi89/ocx/releases/latest/download"; got != want {
		t.Errorf("default location = %q, want %q", got, want)
	}
	if got, want := Location("https://mirror.example/ocx"), "https://mirror.example/ocx"; got != want {
		t.Errorf("location = %q, want %q", got, want)
	}
}

func TestLocationIgnoresATrailingSlash(t *testing.T) {
	if got, want := Location("https://mirror.example/ocx/"), "https://mirror.example/ocx"; got != want {
		t.Errorf("location = %q, want %q", got, want)
	}
}

func TestSystemsWithoutAReleaseFailNamingTheSystemAndDownloadNothing(t *testing.T) {
	systems := [][2]string{
		{"darwin", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"},
		{"freebsd", "amd64"}, {"linux", "386"}, {"linux", "riscv64"}, {"linux", "arm"},
	}
	for _, sys := range systems {
		for _, check := range []bool{false, true} {
			name := sys[0] + "/" + sys[1]
			if check {
				name += " check"
			}
			t.Run(name, func(t *testing.T) {
				s := newRelServer(t)
				s.put("/SHA256SUMS", relLine("ocx-linux-amd64", "a")+relLine("ocx-linux-arm64", "b"))
				s.put("/ocx-linux-amd64", "a")
				s.put("/ocx-linux-arm64", "b")
				exe := relBinaryIn(t, relRunning, 0o755)
				_, err := Run(Options{Location: s.srv.URL, Executable: exe, GOOS: sys[0], GOARCH: sys[1]}, check)
				if err == nil {
					t.Fatal("no error for a system without a release")
				}
				if !strings.Contains(err.Error(), sys[0]) || !strings.Contains(err.Error(), sys[1]) {
					t.Errorf("the error %q doesn't name %s and %s", err, sys[0], sys[1])
				}
				if got := s.requested(); len(got) != 0 {
					t.Errorf("downloaded %v, want nothing", got)
				}
				relUntouched(t, exe, relRunning)
			})
		}
	}
}

func TestSHA256SUMSNeedsExactlyOneWellFormedLineForTheBinary(t *testing.T) {
	const newBuild = "#!/bin/sh\necho new-build\n"
	good := relHex(newBuild)
	cases := []struct{ name, sums string }{
		{"an empty file", ""},
		{"only the other processor's line", relLine("ocx-linux-arm64", newBuild)},
		{"the same line twice", relLine("ocx-linux-amd64", newBuild) + relLine("ocx-linux-amd64", newBuild)},
		{"two lines with different checksums", relLine("ocx-linux-amd64", newBuild) + relLine("ocx-linux-amd64", "other")},
		{"a checksum of 63 digits", good[:63] + "  ocx-linux-amd64\n"},
		{"a checksum of 65 digits", good + "0  ocx-linux-amd64\n"},
		{"a checksum with a character that isn't a digit", "g" + good[1:] + "  ocx-linux-amd64\n"},
		{"one space", good + " ocx-linux-amd64\n"},
		{"a tab", good + "\tocx-linux-amd64\n"},
		{"three spaces", good + "   ocx-linux-amd64\n"},
		{"a binary-mode marker", good + " *ocx-linux-amd64\n"},
	}
	for _, c := range cases {
		for _, check := range []bool{false, true} {
			name := c.name
			if check {
				name += " (check only)"
			}
			t.Run(name, func(t *testing.T) {
				s := newRelServer(t)
				s.put("/SHA256SUMS", c.sums)
				s.put("/ocx-linux-amd64", newBuild)
				exe := relBinaryIn(t, relRunning, 0o755)
				_, err := Run(Options{Location: s.srv.URL, Executable: exe, GOOS: "linux", GOARCH: "amd64"}, check)
				if err == nil {
					t.Error("no error for a SHA256SUMS without exactly one valid line")
				}
				if n := s.hits("/ocx-linux-amd64"); n != 0 {
					t.Errorf("the binary was downloaded %d times", n)
				}
				relUntouched(t, exe, relRunning)
			})
		}
	}
}

func TestABinaryMatchingSHA256SUMSIsUpToDateEvenWhenReachedThroughALink(t *testing.T) {
	s := newRelServer(t)
	s.put("/SHA256SUMS", relLine("ocx-linux-amd64", relRunning))
	s.put("/ocx-linux-amd64", "#!/bin/sh\necho some-other-build\n")
	real := relBinaryIn(t, relRunning, 0o755)
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "ocx")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, check := range []bool{false, true} {
		res, err := Run(Options{Location: s.srv.URL, Executable: link, GOOS: "linux", GOARCH: "amd64"}, check)
		if err != nil || !res.UpToDate {
			t.Fatalf("check=%v: up to date = %v, err = %v", check, res.UpToDate, err)
		}
	}
	if n := s.hits("/ocx-linux-amd64"); n != 0 {
		t.Errorf("the binary was downloaded %d times", n)
	}
	if target, err := os.Readlink(link); err != nil || target != real {
		t.Errorf("the link points to %q, %v; want %q", target, err, real)
	}
	relUntouched(t, real, relRunning)
	if got := relNames(t, linkDir); !reflect.DeepEqual(got, []string{"ocx"}) {
		t.Errorf("the link's directory holds %v, want only the link", got)
	}
}

func TestReplacingThroughALinkReplacesTheLinkedFileAndKeepsTheLink(t *testing.T) {
	record := filepath.Join(t.TempDir(), "record")
	newBuild := "#!/bin/sh\nif [ \"$1\" = version ]; then\n" +
		"  echo \"${0%/*}\" > '" + record + "'\n" +
		"  stat -c %a \"$0\" >> '" + record + "'\n" +
		"  echo new-build\n  echo more detail\nfi\n"
	s := newRelServer(t)
	s.put("/SHA256SUMS", relLine("ocx-linux-amd64", newBuild))
	s.put("/ocx-linux-amd64", newBuild)

	real := relBinaryIn(t, relRunning, 0o700)
	realDir := filepath.Dir(real)
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "ocx")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{realDir, linkDir} {
		if err := os.WriteFile(filepath.Join(dir, "other"), []byte("other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Run(Options{Location: s.srv.URL, Executable: link, GOOS: "linux", GOARCH: "amd64"}, false); err != nil {
		t.Fatal(err)
	}

	if got := relRead(t, real); got != newBuild {
		t.Errorf("the linked file holds %q, want the release binary", got)
	}
	if fi, err := os.Stat(real); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("the linked file's mode = %v, %v; want 0755", fi.Mode().Perm(), err)
	}
	if target, err := os.Readlink(link); err != nil || target != real {
		t.Errorf("the link points to %q, %v; want %q", target, err, real)
	}
	for _, dir := range []string{realDir, linkDir} {
		if got := relNames(t, dir); !reflect.DeepEqual(got, []string{"ocx", "other"}) {
			t.Errorf("%s holds %v, want ocx and other", dir, got)
		}
		if got := relRead(t, filepath.Join(dir, "other")); got != "other\n" {
			t.Errorf("%s/other changed to %q", dir, got)
		}
	}

	lines := strings.Split(strings.TrimSpace(relRead(t, record)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the new binary's version command recorded %q", lines)
	}
	wantDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if lines[0] != wantDir {
		t.Errorf("the new binary ran from %q, want a temporary file next to the real binary in %q", lines[0], wantDir)
	}
	if lines[1] != "755" {
		t.Errorf("the new binary's mode when it ran = %s, want 755", lines[1])
	}
}
