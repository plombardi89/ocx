package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests run real ocx binaries as separate processes, with
// OCX_RELEASE_URL pointing at a local server that plays the release.

const (
	updOlder = "older-build"
	updNewer = "newer-build"
)

// updMachineAsset returns the release file name for this machine, or skips
// the test when no release is built for it.
func updMachineAsset(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skipf("no ocx release is built for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return "ocx-linux-" + runtime.GOARCH
}

func updHex(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// updSums is a SHA256SUMS with a line for this machine's binary and an
// unrelated line for the other processor's.
func updSums(asset string, binary []byte) string {
	other := "ocx-linux-arm64"
	if asset == other {
		other = "ocx-linux-amd64"
	}
	return updHex([]byte("unrelated")) + "  " + other + "\n" + updHex(binary) + "  " + asset + "\n"
}

// updBuild builds ocx so that its version command prints stamp, and returns
// the binary's path. env adds build environment variables.
func updBuild(t *testing.T, stamp string, env ...string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ocx")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-ldflags", "-X main.commit="+stamp, "-o", bin, ".")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func updReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// updInstall copies the binary at src to ocx in a new directory.
func updInstall(t *testing.T, src string, mode os.FileMode) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "ocx")
	if err := os.WriteFile(dst, updReadFile(t, src), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dst, mode); err != nil {
		t.Fatal(err)
	}
	return dst
}

// updScript is a stand-in binary: a shell script.
func updScript(body string) []byte { return []byte("#!/bin/sh\n" + body) }

// updVersionScript is a stand-in binary whose version command prints first
// and then a second line.
func updVersionScript(first string) []byte {
	return updScript("case \"$1\" in\nversion) echo " + first + "; echo more detail ;;\nesac\n")
}

type updResult struct {
	code           int
	stdout, stderr string
	elapsed        time.Duration
}

// updRun runs the ocx binary at bin with a clean environment: a temporary
// HOME, OCX_RELEASE_URL when releaseURL isn't empty, and extraEnv.
func updRun(t *testing.T, bin, releaseURL string, extraEnv []string, args ...string) updResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	if releaseURL != "" {
		cmd.Env = append(cmd.Env, "OCX_RELEASE_URL="+releaseURL)
	}
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	res := updResult{stdout: stdout.String(), stderr: stderr.String(), elapsed: time.Since(start)}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.code = exit.ExitCode()
	default:
		t.Fatalf("running %s %v: %v", bin, args, err)
	}
	return res
}

func (r updResult) String() string {
	return fmt.Sprintf("exit status %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
}

// updServer plays a release location below prefix and records the path of
// every request.
type updServer struct {
	srv      *httptest.Server
	prefix   string
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	paths    []string
}

func newUpdServer(t *testing.T, prefix string) *updServer {
	t.Helper()
	s := &updServer{prefix: prefix, handlers: map[string]http.HandlerFunc{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		h := s.handlers[strings.TrimPrefix(r.URL.Path, s.prefix+"/")]
		s.mu.Unlock()
		if h == nil || !strings.HasPrefix(r.URL.Path, s.prefix+"/") {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// url is the release location, without a trailing slash.
func (s *updServer) url() string { return s.srv.URL + s.prefix }

func (s *updServer) handle(name string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[name] = h
}

func (s *updServer) serve(name string, body []byte) {
	s.handle(name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
}

func (s *updServer) serveStatus(name string, status int) {
	s.handle(name, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("no\n"))
	})
}

// serveCutOff promises more bytes than it sends and then drops the
// connection.
func (s *updServer) serveCutOff(name string, body []byte) {
	s.handle(name, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)+100))
		_, _ = w.Write(body)
	})
}

// release serves binary as this machine's release file, with a matching
// SHA256SUMS.
func (s *updServer) release(asset string, binary []byte) {
	s.serve("SHA256SUMS", []byte(updSums(asset, binary)))
	s.serve(asset, binary)
}

func (s *updServer) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// updSnapshot describes everything under dir: regular files with their mode
// and content, directories, and links with their targets.
func updSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			state[rel] = "link to " + target
		case d.IsDir():
			state[rel] = "directory"
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			state[rel] = fmt.Sprintf("%v %x", info.Mode().Perm(), sha256.Sum256(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func updNotChanged(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if reflect.DeepEqual(before, after) {
		return
	}
	var diffs []string
	for k, v := range before {
		if got, ok := after[k]; !ok {
			diffs = append(diffs, "removed "+k)
		} else if got != v {
			diffs = append(diffs, "changed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			diffs = append(diffs, "added "+k)
		}
	}
	sort.Strings(diffs)
	t.Errorf("%s changed: %s", what, strings.Join(diffs, ", "))
}

// updTree maps the relative path of every regular file under dir to its
// content.
func updTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return files
		}
		t.Fatal(err)
	}
	return files
}

func updFirstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(first)
}

func TestUpdateOfAnUpToDateBinaryReportsItAndFetchesOnlySHA256SUMS(t *testing.T) {
	asset := updMachineAsset(t)
	older := updBuild(t, updOlder)
	for _, args := range [][]string{{"update"}, {"update", "--check"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			installed := updInstall(t, older, 0o755)
			s := newUpdServer(t, "")
			s.serve("SHA256SUMS", []byte(updSums(asset, updReadFile(t, installed))))
			s.serve(asset, updVersionScript("must-not-be-installed"))
			before := updSnapshot(t, filepath.Dir(installed))

			r := updRun(t, installed, s.url(), nil, args...)

			if r.code != 0 {
				t.Fatalf("exited %d, want 0\n%s", r.code, r)
			}
			if !strings.Contains(r.stdout, "Already up to date: "+updOlder) {
				t.Errorf("stdout = %q, want it to say \"Already up to date: %s\"", r.stdout, updOlder)
			}
			if got := s.requested(); !reflect.DeepEqual(got, []string{"/SHA256SUMS"}) {
				t.Errorf("requests = %v, want only /SHA256SUMS", got)
			}
			updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
		})
	}
}

func TestUpdateComparesTheFileALinkPointsTo(t *testing.T) {
	asset := updMachineAsset(t)
	realPath := updInstall(t, updBuild(t, updOlder), 0o755)
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "ocx")
	if err := os.Symlink(realPath, link); err != nil {
		t.Fatal(err)
	}
	s := newUpdServer(t, "")
	s.serve("SHA256SUMS", []byte(updSums(asset, updReadFile(t, realPath))))
	s.serve(asset, updVersionScript("must-not-be-installed"))
	beforeReal, beforeLink := updSnapshot(t, filepath.Dir(realPath)), updSnapshot(t, linkDir)

	r := updRun(t, link, s.url(), nil, "update")

	if r.code != 0 || !strings.Contains(r.stdout, "Already up to date: "+updOlder) {
		t.Errorf("running through a link: %s", r)
	}
	if got := s.requested(); !reflect.DeepEqual(got, []string{"/SHA256SUMS"}) {
		t.Errorf("requests = %v, want only /SHA256SUMS", got)
	}
	updNotChanged(t, "the real binary's directory", beforeReal, updSnapshot(t, filepath.Dir(realPath)))
	updNotChanged(t, "the link's directory", beforeLink, updSnapshot(t, linkDir))
}

func TestUpdateReplacesTheBinaryWithTheReleaseAndSaysSo(t *testing.T) {
	asset := updMachineAsset(t)
	older := updBuild(t, updOlder)
	installed := updInstall(t, older, 0o700)
	dir := filepath.Dir(installed)
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "record")
	standIn := updScript("if [ \"$1\" = version ]; then\n" +
		"  echo \"${0%/*}\" > '" + record + "'\n" +
		"  stat -c %a \"$0\" >> '" + record + "'\n" +
		"  echo stand-in-2\n  echo more detail\nfi\n")
	s := newUpdServer(t, "/releases/latest")
	s.release(asset, standIn)
	beforeKeep := updSnapshot(t, dir)["keep.txt"]

	// A trailing slash on the location makes no difference.
	r := updRun(t, installed, s.url()+"/", nil, "update")

	if r.code != 0 {
		t.Fatalf("exited %d, want 0\n%s", r.code, r)
	}
	if !strings.Contains(r.stdout, "Updated ocx from "+updOlder+" to stand-in-2.") {
		t.Errorf("stdout = %q, want \"Updated ocx from %s to stand-in-2.\"", r.stdout, updOlder)
	}
	if got := string(updReadFile(t, installed)); got != string(standIn) {
		t.Errorf("the binary is now %q, want the release binary", got)
	}
	if fi, err := os.Stat(installed); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("the new binary's mode = %v, %v; want 0755", fi.Mode().Perm(), err)
	}
	wantRequests := []string{"/releases/latest/SHA256SUMS", "/releases/latest/" + asset}
	if got := s.requested(); !reflect.DeepEqual(got, wantRequests) {
		t.Errorf("requests = %v, want %v", got, wantRequests)
	}
	after := updSnapshot(t, dir)
	if len(after) != 3 || after["keep.txt"] != beforeKeep {
		t.Errorf("the directory should hold only ., ocx, and keep.txt unchanged: %v", after)
	}

	lines := strings.Split(strings.TrimSpace(string(updReadFile(t, record))), "\n")
	if len(lines) != 2 {
		t.Fatalf("the new binary's version command recorded %q", lines)
	}
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lines[0] != wantDir {
		t.Errorf("the new binary ran its version command from %q, want a file in %q", lines[0], wantDir)
	}
	if lines[1] != "755" {
		t.Errorf("the new binary's mode when its version ran = %s, want 755", lines[1])
	}
}

func TestUpdateRunThroughALinkReplacesTheLinkedFileAndKeepsTheLink(t *testing.T) {
	asset := updMachineAsset(t)
	realPath := updInstall(t, updBuild(t, updOlder), 0o755)
	realDir := filepath.Dir(realPath)
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "ocx")
	if err := os.Symlink(realPath, link); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{realDir, linkDir} {
		if err := os.WriteFile(filepath.Join(dir, "other"), []byte("other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	standIn := updVersionScript("stand-in-2")
	s := newUpdServer(t, "")
	s.release(asset, standIn)

	r := updRun(t, link, s.url(), nil, "update")

	if r.code != 0 || !strings.Contains(r.stdout, "Updated ocx from "+updOlder+" to stand-in-2.") {
		t.Fatalf("update through a link: %s", r)
	}
	if got := string(updReadFile(t, realPath)); got != string(standIn) {
		t.Errorf("the linked file holds %q, want the release binary", got)
	}
	if got := string(updReadFile(t, link)); got != string(standIn) {
		t.Errorf("reading through the link gives %q, want the release binary", got)
	}
	if target, err := os.Readlink(link); err != nil || target != realPath {
		t.Errorf("the link points to %q, %v; want %q", target, err, realPath)
	}
	for _, dir := range []string{realDir, linkDir} {
		snap := updSnapshot(t, dir)
		if len(snap) != 3 {
			t.Errorf("%s holds %v, want ., ocx, and other", dir, snap)
		}
		if got := string(updReadFile(t, filepath.Join(dir, "other"))); got != "other\n" {
			t.Errorf("%s/other changed to %q", dir, got)
		}
	}
}

type updFailure struct {
	name        string
	unreachable bool
	setup       func(s *updServer, asset string, good []byte)
	want        func(s *updServer) []string
}

func updGoodSum(good []byte) string { return updHex(good) }

func TestUpdateFailuresExitWithStatus1AndLeaveTheBinaryAlone(t *testing.T) {
	asset := updMachineAsset(t)
	older := updBuild(t, updOlder)
	sumsCase := func(name string, sums func(good []byte) string) updFailure {
		return updFailure{name: name, setup: func(s *updServer, asset string, good []byte) {
			s.serve("SHA256SUMS", []byte(sums(good)))
			s.serve(asset, good)
		}}
	}
	cases := []updFailure{
		{name: "the release can't be reached", unreachable: true},
		{name: "SHA256SUMS is cut off part way", setup: func(s *updServer, asset string, good []byte) {
			s.release(asset, good)
			s.serveCutOff("SHA256SUMS", []byte(updSums(asset, good)))
		}},
		{name: "the binary is cut off part way", setup: func(s *updServer, asset string, good []byte) {
			s.release(asset, good)
			s.serveCutOff(asset, good)
		}},
		{name: "SHA256SUMS is not found", setup: func(s *updServer, asset string, good []byte) {
			s.serve(asset, good)
		}, want: func(s *updServer) []string { return []string{s.url(), "404"} }},
		{name: "SHA256SUMS gives a server error", setup: func(s *updServer, asset string, good []byte) {
			s.release(asset, good)
			s.serveStatus("SHA256SUMS", 500)
		}, want: func(s *updServer) []string { return []string{s.url(), "500"} }},
		{name: "the binary is not found", setup: func(s *updServer, asset string, good []byte) {
			s.serve("SHA256SUMS", []byte(updSums(asset, good)))
		}, want: func(s *updServer) []string { return []string{s.url(), "404"} }},
		{name: "the binary is forbidden", setup: func(s *updServer, asset string, good []byte) {
			s.release(asset, good)
			s.serveStatus(asset, 403)
		}, want: func(s *updServer) []string { return []string{s.url(), "403"} }},
		{name: "the binary gives a server error", setup: func(s *updServer, asset string, good []byte) {
			s.release(asset, good)
			s.serveStatus(asset, 503)
		}, want: func(s *updServer) []string { return []string{s.url(), "503"} }},
		{name: "the binary doesn't match SHA256SUMS", setup: func(s *updServer, asset string, good []byte) {
			s.serve("SHA256SUMS", []byte(updSums(asset, good)))
			s.serve(asset, append(append([]byte(nil), good...), "# tampered\n"...))
		}},
		sumsCase("SHA256SUMS has no line for the binary", func(_ []byte) string {
			return updHex([]byte("unrelated")) + "  ocx-linux-other\n"
		}),
		sumsCase("SHA256SUMS has two lines for the binary", func(good []byte) string {
			return updGoodSum(good) + "  " + asset + "\n" + updGoodSum(good) + "  " + asset + "\n"
		}),
		sumsCase("SHA256SUMS has a checksum of 63 digits", func(good []byte) string {
			return updGoodSum(good)[:63] + "  " + asset + "\n"
		}),
		sumsCase("SHA256SUMS has a checksum of 65 digits", func(good []byte) string {
			return updGoodSum(good) + "0  " + asset + "\n"
		}),
		sumsCase("SHA256SUMS has a checksum with a character that isn't a digit", func(good []byte) string {
			return "g" + updGoodSum(good)[1:] + "  " + asset + "\n"
		}),
		sumsCase("SHA256SUMS separates the checksum with one space", func(good []byte) string {
			return updGoodSum(good) + " " + asset + "\n"
		}),
		{name: "the new binary's version fails", setup: func(s *updServer, asset string, _ []byte) {
			s.release(asset, updScript("exit 3\n"))
		}},
		{name: "the new binary's version prints and then fails", setup: func(s *updServer, asset string, _ []byte) {
			s.release(asset, updScript("echo looks-fine\nexit 1\n"))
		}},
		{name: "the new binary's version prints nothing", setup: func(s *updServer, asset string, _ []byte) {
			s.release(asset, updScript("exit 0\n"))
		}},
		{name: "the new binary can't be run", setup: func(s *updServer, asset string, _ []byte) {
			s.release(asset, []byte("this is not a program\n"))
		}},
	}
	good := updVersionScript("stand-in-2")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			installed := updInstall(t, older, 0o755)
			dir := filepath.Dir(installed)
			s := newUpdServer(t, "")
			if c.setup != nil {
				c.setup(s, asset, good)
			}
			url := s.url()
			if c.unreachable {
				s.srv.Close()
			}
			before := updSnapshot(t, dir)

			r := updRun(t, installed, url, nil, "update")

			if r.code != 1 {
				t.Errorf("exited %d, want 1\n%s", r.code, r)
			}
			if strings.TrimSpace(r.stderr) == "" {
				t.Error("printed nothing about what failed")
			}
			if c.want != nil {
				for _, w := range c.want(s) {
					if !strings.Contains(r.stderr, w) {
						t.Errorf("stderr = %q, want it to contain %q", r.stderr, w)
					}
				}
			}
			if strings.Contains(r.stdout, "Updated ocx") || strings.Contains(r.stdout, "Already up to date") {
				t.Errorf("stdout claims success: %q", r.stdout)
			}
			updNotChanged(t, "the binary's directory", before, updSnapshot(t, dir))
		})
	}
}

func TestUpdateInADirectoryItCantWriteToNamesItAndSuggestsAnotherOne(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	asset := updMachineAsset(t)
	installed := updInstall(t, updBuild(t, updOlder), 0o755)
	dir := filepath.Dir(installed)
	s := newUpdServer(t, "")
	s.release(asset, updVersionScript("stand-in-2"))
	before := updSnapshot(t, dir)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	r := updRun(t, installed, s.url(), nil, "update")

	if r.code != 1 {
		t.Errorf("exited %d, want 1\n%s", r.code, r)
	}
	for _, want := range []string{dir, "~/.local/bin"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", r.stderr, want)
		}
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, dir))
}

// updInstalledWithServer installs the older build and starts a release
// server, and returns a snapshot of the installed binary's directory.
func updInstalledWithServer(t *testing.T) (installed string, s *updServer, before map[string]string) {
	t.Helper()
	installed = updInstall(t, updBuild(t, updOlder), 0o755)
	s = newUpdServer(t, "")
	return installed, s, updSnapshot(t, filepath.Dir(installed))
}

// updPadded returns script followed by padding, size bytes in all. The
// script ends in exit, so a shell never reads the padding.
func updPadded(script string, size int) []byte {
	out := bytes.Repeat([]byte("#"), size)
	copy(out, script)
	return out
}

func TestUpdateAcceptsABinaryOfExactlyOneHundredMiB(t *testing.T) {
	asset := updMachineAsset(t)
	installed, s, _ := updInstalledWithServer(t)
	binary := updPadded("#!/bin/sh\ncase \"$1\" in\nversion) echo stand-in-big ;;\nesac\nexit 0\n", 100<<20)
	s.release(asset, binary)

	r := updRun(t, installed, s.url(), nil, "update")

	if r.code != 0 || !strings.Contains(r.stdout, "Updated ocx from "+updOlder+" to stand-in-big.") {
		t.Fatalf("a download of exactly 100 MiB: %s", r)
	}
	if got := updReadFile(t, installed); !bytes.Equal(got, binary) {
		t.Errorf("the installed binary is %d bytes, not the 100 MiB release binary", len(got))
	}
}

func TestUpdateRejectsABinaryOverOneHundredMiB(t *testing.T) {
	asset := updMachineAsset(t)
	installed, s, before := updInstalledWithServer(t)
	s.release(asset, updPadded("#!/bin/sh\necho stand-in-big\nexit 0\n", 100<<20+1))

	r := updRun(t, installed, s.url(), nil, "update")

	if r.code != 1 || strings.TrimSpace(r.stderr) == "" {
		t.Errorf("a download of 100 MiB and one byte: %s", r)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
}

func TestUpdateRejectsASHA256SUMSOverOneHundredMiB(t *testing.T) {
	asset := updMachineAsset(t)
	installed, s, before := updInstalledWithServer(t)
	good := updVersionScript("stand-in-2")
	sums := []byte(updSums(asset, good))
	s.serve("SHA256SUMS", append(sums, bytes.Repeat([]byte("\n"), 100<<20+1-len(sums))...))
	s.serve(asset, good)

	r := updRun(t, installed, s.url(), nil, "update")

	if r.code != 1 || strings.TrimSpace(r.stderr) == "" {
		t.Errorf("a SHA256SUMS of 100 MiB and one byte: %s", r)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
}

func TestUpdateAcceptsANewBinaryWhoseVersionTakesSeconds(t *testing.T) {
	t.Parallel()
	asset := updMachineAsset(t)
	installed := updInstall(t, updBuild(t, updOlder), 0o755)
	s := newUpdServer(t, "")
	s.release(asset, updScript("case \"$1\" in\nversion) sleep 5; echo slow-start ;;\nesac\n"))

	r := updRun(t, installed, s.url(), nil, "update")

	if r.code != 0 || !strings.Contains(r.stdout, "Updated ocx from "+updOlder+" to slow-start.") {
		t.Errorf("a version command that takes 5 seconds: %s", r)
	}
}

func TestUpdateGivesUpOnANewBinaryWhoseVersionNeverFinishes(t *testing.T) {
	t.Parallel()
	asset := updMachineAsset(t)
	installed := updInstall(t, updBuild(t, updOlder), 0o755)
	s := newUpdServer(t, "")
	s.release(asset, updScript("case \"$1\" in\nversion) exec sleep 120 ;;\nesac\n"))
	before := updSnapshot(t, filepath.Dir(installed))

	r := updRun(t, installed, s.url(), nil, "update")

	if r.code != 1 || strings.TrimSpace(r.stderr) == "" {
		t.Errorf("a version command that never finishes: %s", r)
	}
	if r.elapsed > 20*time.Second {
		t.Errorf("took %v to give up, want about 10 seconds", r.elapsed)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
}

func TestUpdateCheckReportsAnAvailableUpdateAndChangesNothing(t *testing.T) {
	asset := updMachineAsset(t)
	installed := updInstall(t, updBuild(t, updOlder), 0o755)
	dir := filepath.Dir(installed)
	s := newUpdServer(t, "")
	s.release(asset, updVersionScript("stand-in-2"))
	before := updSnapshot(t, dir)

	r := updRun(t, installed, s.url(), nil, "update", "--check")

	if r.code != 0 {
		t.Fatalf("exited %d, want 0\n%s", r.code, r)
	}
	if !strings.Contains(r.stdout, "An update is available.") {
		t.Errorf("stdout = %q, want \"An update is available.\"", r.stdout)
	}
	if strings.Contains(r.stdout, "Updated ocx") {
		t.Errorf("stdout says the binary was updated: %q", r.stdout)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, dir))
}

func TestUpdateCheckExitsWith1OnlyWhenTheCheckFails(t *testing.T) {
	asset := updMachineAsset(t)
	older := updBuild(t, updOlder)
	cases := []struct {
		name        string
		unreachable bool
		setup       func(s *updServer)
	}{
		{name: "the release can't be reached", unreachable: true},
		{name: "SHA256SUMS is not found", setup: func(_ *updServer) {}},
		{name: "SHA256SUMS has no line for the binary", setup: func(s *updServer) {
			s.serve("SHA256SUMS", []byte(updHex([]byte("x"))+"  ocx-linux-other\n"))
		}},
		{name: "SHA256SUMS has an invalid line for the binary", setup: func(s *updServer) {
			s.serve("SHA256SUMS", []byte("not-a-checksum  "+asset+"\n"))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			installed := updInstall(t, older, 0o755)
			s := newUpdServer(t, "")
			if c.setup != nil {
				c.setup(s)
			}
			url := s.url()
			if c.unreachable {
				s.srv.Close()
			}
			before := updSnapshot(t, filepath.Dir(installed))

			r := updRun(t, installed, url, nil, "update", "--check")

			if r.code != 1 || strings.TrimSpace(r.stderr) == "" {
				t.Errorf("a failed check: %s", r)
			}
			updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
		})
	}
}

func TestUpdateUsageErrorsExitWith2AndChangeNothing(t *testing.T) {
	asset := updMachineAsset(t)
	installed := updInstall(t, updBuild(t, updOlder), 0o755)
	dir := filepath.Dir(installed)
	cfg := filepath.Join(t.TempDir(), "opencode")
	s := newUpdServer(t, "")
	s.release(asset, updVersionScript("stand-in-2"))
	cases := [][]string{
		{"update", "--check", "--apply"},
		{"update", "--apply", "--check"},
		{"update", "--check", "--apply", "--config-dir", cfg},
		{"update", "--bogus"},
		{"update", "-x"},
		{"update", "--apply", "--bogus"},
		{"update", "extra"},
		{"update", "--apply", "extra"},
		{"update", "--check", "extra"},
		{"update", "--config-dir", cfg},
		{"update", "--check", "--config-dir", cfg},
	}
	before := updSnapshot(t, dir)
	for _, args := range cases {
		r := updRun(t, installed, s.url(), nil, args...)
		if r.code != 2 {
			t.Errorf("ocx %v exited %d, want 2\n%s", args, r.code, r)
		}
	}
	if got := s.requested(); len(got) != 0 {
		t.Errorf("usage errors made requests: %v", got)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("a usage error created the configuration directory")
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, dir))
}

func TestUpdateOnASystemWithoutAReleaseExitsWith1NamingItAndDownloadsNothing(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("this test runs a linux/386 build, which needs a linux/amd64 machine")
	}
	bin := updBuild(t, updOlder, "GOOS=linux", "GOARCH=386", "CGO_ENABLED=0")
	if out, err := exec.CommandContext(t.Context(), bin, "version").CombinedOutput(); err != nil {
		t.Skipf("this machine can't run a linux/386 binary: %v %s", err, out)
	}
	installed := updInstall(t, bin, 0o755)
	s := newUpdServer(t, "")
	s.release("ocx-linux-amd64", updVersionScript("stand-in-2"))
	s.release("ocx-linux-arm64", updVersionScript("stand-in-2"))
	before := updSnapshot(t, filepath.Dir(installed))
	for _, args := range [][]string{{"update"}, {"update", "--check"}} {
		r := updRun(t, installed, s.url(), nil, args...)
		if r.code != 1 {
			t.Errorf("ocx %v exited %d, want 1\n%s", args, r.code, r)
		}
		if !strings.Contains(r.stderr, "386") {
			t.Errorf("ocx %v: stderr = %q, want it to name the system, linux/386", args, r.stderr)
		}
	}
	if got := s.requested(); len(got) != 0 {
		t.Errorf("downloaded %v, want nothing", got)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
}

func TestUpdateWithoutAReleaseURLContactsGitHub(t *testing.T) {
	updMachineAsset(t)
	var mu sync.Mutex
	var targets []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		targets = append(targets, r.Method+" "+r.Host)
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(proxy.Close)
	installed := updInstall(t, updBuild(t, updOlder), 0o755)
	env := []string{
		"HTTPS_PROXY=" + proxy.URL, "https_proxy=" + proxy.URL,
		"HTTP_PROXY=" + proxy.URL, "http_proxy=" + proxy.URL,
		"NO_PROXY=", "no_proxy=",
	}

	r := updRun(t, installed, "", env, "update", "--check")

	if r.code != 1 {
		t.Errorf("exited %d, want 1 because the proxy refuses\n%s", r.code, r)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, target := range targets {
		if target == "CONNECT github.com:443" {
			found = true
		}
	}
	if !found {
		t.Errorf("the proxy saw %v, want a connection to github.com:443", targets)
	}
}

func TestUpdateApplyRunsTheNewBinarysApplyAndPassesItsResultThrough(t *testing.T) {
	asset := updMachineAsset(t)
	older := updBuild(t, updOlder)
	cfg := filepath.Join(t.TempDir(), "opencode")
	for _, c := range []struct {
		name string
		args []string
		want [][]string
	}{
		{
			"with a configuration directory",
			[]string{"update", "--apply", "--config-dir", cfg},
			[][]string{{"apply", "--config-dir", cfg}, {"apply", "--config-dir=" + cfg}},
		},
		{"without one", []string{"update", "--apply"}, [][]string{{"apply"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			installed := updInstall(t, older, 0o755)
			record := filepath.Join(t.TempDir(), "record")
			standIn := updScript("case \"$1\" in\n" +
				"version) echo stand-in-2 ;;\n" +
				"apply) printf '%s\\n' \"$@\" > '" + record + "'; echo line on stdout; echo line on stderr >&2; exit 7 ;;\n" +
				"esac\n")
			s := newUpdServer(t, "")
			s.release(asset, standIn)

			r := updRun(t, installed, s.url(), nil, c.args...)

			if r.code != 7 {
				t.Errorf("exited %d, want the new binary's apply status 7\n%s", r.code, r)
			}
			if !strings.Contains(r.stdout, "line on stdout\n") {
				t.Errorf("stdout = %q, want apply's line", r.stdout)
			}
			if !strings.Contains(r.stderr, "line on stderr\n") {
				t.Errorf("stderr = %q, want apply's line", r.stderr)
			}
			got := strings.Split(strings.TrimSpace(string(updReadFile(t, record))), "\n")
			matched := false
			for _, want := range c.want {
				matched = matched || reflect.DeepEqual(got, want)
			}
			if !matched {
				t.Errorf("the new binary was run with %q, want one of %q", got, c.want)
			}
			if got := string(updReadFile(t, installed)); got != string(standIn) {
				t.Error("the binary wasn't replaced before apply ran")
			}
		})
	}
}

func TestUpdateApplyInstallsTheNewBinarysConfigurationAndAgainWhenUpToDate(t *testing.T) {
	asset := updMachineAsset(t)
	older, newer := updBuild(t, updOlder), updBuild(t, updNewer)
	newerData := updReadFile(t, newer)
	s := newUpdServer(t, "")
	s.release(asset, newerData)
	installed := updInstall(t, older, 0o755)

	wantCfg := filepath.Join(t.TempDir(), "reference")
	if r := updRun(t, newer, "", nil, "apply", "--config-dir", wantCfg); r.code != 0 {
		t.Fatalf("setup: the newer binary's apply exited %d\n%s", r.code, r)
	}
	want := updTree(t, wantCfg)
	if len(want) == 0 {
		t.Fatal("setup: the newer binary's apply wrote nothing")
	}

	cfg := filepath.Join(t.TempDir(), "opencode")
	r := updRun(t, installed, s.url(), nil, "update", "--apply", "--config-dir", cfg)
	if r.code != 0 {
		t.Fatalf("exited %d, want 0\n%s", r.code, r)
	}
	if !strings.Contains(r.stdout, "Updated ocx from "+updOlder+" to "+updNewer+".") {
		t.Errorf("stdout = %q, want the update message", r.stdout)
	}
	// Only the new binary can print its own version here.
	if !strings.Contains(r.stdout, "Applied "+updNewer) {
		t.Errorf("stdout = %q, want the new binary's \"Applied %s\"", r.stdout, updNewer)
	}
	if v := updRun(t, installed, "", nil, "version"); updFirstLine(v.stdout) != updNewer {
		t.Errorf("the installed binary reports %q, want %q", v.stdout, updNewer)
	}
	if got := updTree(t, cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("the configuration applied holds %d files, want the new binary's %d", len(got), len(want))
	}

	cfg2 := filepath.Join(t.TempDir(), "opencode")
	r = updRun(t, installed, s.url(), nil, "update", "--apply", "--config-dir", cfg2)
	if r.code != 0 {
		t.Fatalf("a second update --apply exited %d, want 0\n%s", r.code, r)
	}
	if !strings.Contains(r.stdout, "Already up to date: "+updNewer) || !strings.Contains(r.stdout, "Applied "+updNewer) {
		t.Errorf("a second update --apply: %s", r)
	}
	if got := updTree(t, cfg2); !reflect.DeepEqual(got, want) {
		t.Errorf("the second apply wrote %d files, want %d", len(got), len(want))
	}
}

func TestUpdateApplyWhenUpToDateRunsTheCurrentBinarysApply(t *testing.T) {
	asset := updMachineAsset(t)
	older := updBuild(t, updOlder)
	installed := updInstall(t, older, 0o755)
	s := newUpdServer(t, "")
	s.serve("SHA256SUMS", []byte(updSums(asset, updReadFile(t, installed))))
	s.serve(asset, updVersionScript("must-not-be-installed"))
	wantCfg := filepath.Join(t.TempDir(), "reference")
	if r := updRun(t, installed, "", nil, "apply", "--config-dir", wantCfg); r.code != 0 {
		t.Fatalf("setup: apply exited %d\n%s", r.code, r)
	}
	want := updTree(t, wantCfg)
	before := updSnapshot(t, filepath.Dir(installed))

	cfg := filepath.Join(t.TempDir(), "opencode")
	r := updRun(t, installed, s.url(), nil, "update", "--apply", "--config-dir", cfg)

	if r.code != 0 {
		t.Fatalf("exited %d, want 0\n%s", r.code, r)
	}
	if !strings.Contains(r.stdout, "Already up to date: "+updOlder) || !strings.Contains(r.stdout, "Applied "+updOlder) {
		t.Errorf("stdout = %q, want the up-to-date message and the current binary's apply", r.stdout)
	}
	if got := updTree(t, cfg); !reflect.DeepEqual(got, want) || len(got) == 0 {
		t.Errorf("the configuration applied holds %d files, want the current binary's %d", len(got), len(want))
	}
	if got := s.requested(); !reflect.DeepEqual(got, []string{"/SHA256SUMS"}) {
		t.Errorf("requests = %v, want only /SHA256SUMS", got)
	}
	updNotChanged(t, "the binary's directory", before, updSnapshot(t, filepath.Dir(installed)))
}

func TestUpdateApplyThatFailsKeepsTheNewBinaryAndApplyCanBeRunAgain(t *testing.T) {
	asset := updMachineAsset(t)
	older, newer := updBuild(t, updOlder), updBuild(t, updNewer)
	newerData := updReadFile(t, newer)
	s := newUpdServer(t, "")
	s.release(asset, newerData)
	installed := updInstall(t, older, 0o755)

	notADirectory := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADirectory, []byte("a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	direct := updRun(t, newer, "", nil, "apply", "--config-dir", notADirectory)
	if direct.code == 0 || strings.TrimSpace(direct.stderr) == "" {
		t.Fatalf("setup: apply was expected to fail on a file: %s", direct)
	}

	r := updRun(t, installed, s.url(), nil, "update", "--apply", "--config-dir", notADirectory)

	if r.code != direct.code {
		t.Errorf("exited %d, want apply's own status %d\n%s", r.code, direct.code, r)
	}
	if !strings.Contains(r.stderr, strings.TrimSpace(direct.stderr)) {
		t.Errorf("stderr = %q, want apply's message %q", r.stderr, strings.TrimSpace(direct.stderr))
	}
	if got := updReadFile(t, installed); !bytes.Equal(got, newerData) {
		t.Error("the binary wasn't left replaced after apply failed")
	}
	if v := updRun(t, installed, "", nil, "version"); updFirstLine(v.stdout) != updNewer {
		t.Errorf("the installed binary reports %q, want %q", v.stdout, updNewer)
	}

	cfg := filepath.Join(t.TempDir(), "opencode")
	if again := updRun(t, installed, "", nil, "apply", "--config-dir", cfg); again.code != 0 {
		t.Fatalf("ocx apply after the failed update exited %d\n%s", again.code, again)
	}
	if got := updTree(t, cfg); len(got) == 0 {
		t.Error("ocx apply after the failed update wrote nothing")
	}
}
