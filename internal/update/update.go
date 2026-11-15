// Package update replaces the running ocx binary with the one published at a
// release location, after checking it against the release's SHA256SUMS.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultLocation is where the release workflow publishes the latest build.
const DefaultLocation = "https://github.com/plombardi89/ocx/releases/latest/download"

// Limits that apply unless Options sets others.
const (
	DefaultMaxDownload    = 100 << 20
	DefaultVersionTimeout = 10 * time.Second
	DefaultHTTPTimeout    = 2 * time.Minute
)

// Replaced in tests to make writing the new binary or renaming it fail.
var (
	writeAll = func(f *os.File, data []byte) error {
		_, err := f.Write(data)
		return err
	}
	rename = os.Rename
)

// Options says where to look for a release and which binary to replace.
type Options struct {
	// Location holds SHA256SUMS and ocx-linux-<arch>, without a trailing
	// slash; see Location.
	Location string
	// Executable is the running binary's path. Links are followed, so the
	// file they point to is the one replaced.
	Executable string
	// GOOS and GOARCH pick the release binary.
	GOOS, GOARCH string
	// Client fetches the files; nil means a client with DefaultHTTPTimeout.
	Client *http.Client
	// MaxDownload caps each file's size; 0 means DefaultMaxDownload.
	MaxDownload int64
	// VersionTimeout limits how long the new binary's version may take; 0
	// means DefaultVersionTimeout.
	VersionTimeout time.Duration
}

// Result describes what Run found or did.
type Result struct {
	// UpToDate is true when the running binary already matches the release.
	UpToDate bool
	// Path is the file that was, or would be, replaced, with links followed.
	Path string
	// NewVersion is the first line the new binary's version printed, set
	// only after a replacement.
	NewVersion string
}

// Location returns the release location: env when it's set, otherwise
// DefaultLocation, without a trailing slash.
func Location(env string) string {
	if env == "" {
		return DefaultLocation
	}
	return strings.TrimRight(env, "/")
}

// Asset returns the name of the release binary for goos and goarch, or an
// error for a system the release doesn't build for.
func Asset(goos, goarch string) (string, error) {
	if goos == "linux" && (goarch == "amd64" || goarch == "arm64") {
		return "ocx-linux-" + goarch, nil
	}
	return "", fmt.Errorf("there's no ocx release for %s/%s, only linux/amd64 and linux/arm64", goos, goarch)
}

// Run compares the running binary with the release. With check it stops
// there and changes nothing. Otherwise, when they differ, it downloads the
// release binary, checks it against SHA256SUMS, makes sure its version
// command works, and renames it over the running binary. On any error the
// running binary is left as it was, and no temporary file is left behind.
func Run(opts Options, check bool) (Result, error) {
	asset, err := Asset(opts.GOOS, opts.GOARCH)
	if err != nil {
		return Result{}, err
	}
	path, err := filepath.EvalSymlinks(opts.Executable)
	if err != nil {
		return Result{}, fmt.Errorf("find the running ocx: %w", err)
	}
	res := Result{Path: path}
	sums, err := opts.download("SHA256SUMS")
	if err != nil {
		return res, err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return res, err
	}
	running, err := os.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("read the running ocx: %w", err)
	}
	if sum(running) == want {
		res.UpToDate = true
		return res, nil
	}
	if check {
		return res, nil
	}
	data, err := opts.download(asset)
	if err != nil {
		return res, err
	}
	if sum(data) != want {
		return res, fmt.Errorf("the downloaded %s doesn't match its line in SHA256SUMS", asset)
	}
	res.NewVersion, err = opts.replace(path, data)
	return res, err
}

func (o Options) download(name string) ([]byte, error) {
	url := o.Location + "/" + name
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	limit := o.MaxDownload
	if limit == 0 {
		limit = DefaultMaxDownload
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d MiB", url, limit>>20)
	}
	return data, nil
}

var sumLine = regexp.MustCompile(`^([0-9a-fA-F]{64})  (\S+)$`)

// checksumFor returns the checksum on SHA256SUMS's one line for asset, in
// sha256sum's format: 64 hexadecimal digits, two spaces, and the name.
func checksumFor(sums []byte, asset string) (string, error) {
	var found []string
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.TrimPrefix(fields[len(fields)-1], "*") != asset {
			continue
		}
		m := sumLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil || m[2] != asset {
			return "", fmt.Errorf("SHA256SUMS has an invalid line for %s: %q", asset, line)
		}
		found = append(found, strings.ToLower(m[1]))
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("SHA256SUMS has no line for %s", asset)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("SHA256SUMS has %d lines for %s, not one", len(found), asset)
	}
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// replace writes data next to path, checks that it runs, and renames it
// over path. It returns the first line of the new binary's version.
func (o Options) replace(path string, data []byte) (version string, err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ocx-update-*")
	if err != nil {
		return "", fmt.Errorf("can't write to %s, where ocx is: %w; move ocx to a directory you can write to, such as ~/.local/bin, and run it from there", dir, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	err = writeAll(tmp, data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("write the new ocx to %s: %w", dir, err)
	}
	if err = os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", fmt.Errorf("make the new ocx executable: %w", err)
	}
	if version, err = o.version(tmp.Name()); err != nil {
		return "", err
	}
	if err = rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("replace %s: %w", path, err)
	}
	return version, nil
}

// version runs bin with "version" and returns the first line it prints. The
// binary must exit with status 0 within the time limit and print something.
func (o Options) version(bin string) (string, error) {
	limit := o.VersionTimeout
	if limit == 0 {
		limit = DefaultVersionTimeout
	}
	// The output goes to a file, not a pipe, so only the command itself
	// has to finish in time: a child of it that keeps the output open
	// doesn't hold up the update.
	out, err := os.CreateTemp("", "ocx-version-*")
	if err != nil {
		return "", fmt.Errorf("run the new ocx's version command: %w", err)
	}
	defer func() {
		_ = out.Close()
		_ = os.Remove(out.Name())
	}()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.Stdout = out
	err = cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("the new ocx's version command took longer than %s", limit)
	}
	if err != nil {
		return "", fmt.Errorf("the new ocx's version command failed: %w", err)
	}
	printed, err := os.ReadFile(out.Name())
	if err != nil {
		return "", fmt.Errorf("read the new ocx's version: %w", err)
	}
	first, _, _ := strings.Cut(string(printed), "\n")
	if first = strings.TrimSpace(first); first == "" {
		return "", errors.New("the new ocx's version command printed nothing")
	}
	return first, nil
}
