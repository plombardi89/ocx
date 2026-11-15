// Package apply installs the configuration ocx carries into OpenCode's
// global configuration directory as plain files, and removes it again. It
// keeps no backups: it overwrites whatever is at the paths it writes.
package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// ListFile, inside the configuration directory, lists the files the last
// apply wrote, one relative path per line. A new list is written to
// listTemp first and renamed into place.
const (
	ListFile = ".ocx-files"
	listTemp = ListFile + ".tmp"
)

// ErrNotInstalled means there is no ListFile, so there's nothing to remove.
var ErrNotInstalled = errors.New("nothing to remove: no " + ListFile + " found")

// Change is one file that apply or remove writes or deletes.
type Change struct {
	Op   string // "write" or "delete"
	Path string // relative to the configuration directory, with slashes
}

// Replaced in tests to make a write or the list's rename fail.
var (
	writeFile = os.WriteFile
	rename    = os.Rename
)

// ConfigDir returns OpenCode's global configuration directory.
func ConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "opencode"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode"), nil
}

// Apply writes files into dir, then deletes the files the previous apply
// listed that are no longer in files, and the directories that leaves
// empty. It refuses before changing anything when a link, or a directory
// where a file belongs, is in the way. With dryRun it only reports the
// changes.
func Apply(dir string, files map[string][]byte, dryRun bool) ([]Change, error) {
	root, err := resolve(dir)
	if err != nil {
		return nil, err
	}
	if err := checkListPaths(root); err != nil {
		return nil, err
	}
	previous, _, err := readList(root)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var changes []Change
	for _, p := range paths {
		changes = append(changes, Change{"write", p})
	}
	for _, p := range previous {
		if _, ok := files[p]; !ok {
			changes = append(changes, Change{"delete", p})
		}
	}
	if err := checkPaths(root, changes); err != nil {
		return nil, err
	}
	if dryRun {
		return changes, nil
	}
	for _, c := range changes {
		full := filepath.Join(root, filepath.FromSlash(c.Path))
		if c.Op == "delete" {
			if err := deleteFile(root, full); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, fmt.Errorf("create the directory for %s: %w", c.Path, err)
		}
		if err := writeFile(full, files[c.Path], 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", c.Path, err)
		}
	}
	if err := writeList(root, paths); err != nil {
		return nil, err
	}
	return changes, nil
}

// Remove deletes the files the last apply listed, the directories that
// leaves empty, and then the list itself. Listed files that are already
// gone are skipped. With dryRun it only reports the changes.
func Remove(dir string, dryRun bool) ([]Change, error) {
	root, err := resolve(dir)
	if err != nil {
		return nil, err
	}
	if err := checkListPaths(root); err != nil {
		return nil, err
	}
	listed, found, err := readList(root)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotInstalled
	}
	var changes []Change
	for _, p := range listed {
		changes = append(changes, Change{"delete", p})
	}
	if err := checkPaths(root, changes); err != nil {
		return nil, err
	}
	if dryRun {
		return changes, nil
	}
	for _, c := range changes {
		if err := deleteFile(root, filepath.Join(root, filepath.FromSlash(c.Path))); err != nil {
			return nil, err
		}
	}
	if err := os.Remove(filepath.Join(root, ListFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("delete %s: %w", ListFile, err)
	}
	return changes, nil
}

// resolve returns dir as an absolute path with links resolved, so the
// directory itself may be a link but nothing inside it is followed.
func resolve(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return resolved, nil
}

// readList returns the entries of root's ListFile, and whether it exists. An
// entry that could reach outside root makes the whole list invalid.
func readList(root string) ([]string, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, ListFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var entries []string
	for e := range strings.SplitSeq(string(data), "\n") {
		if e == "" {
			continue
		}
		if path.IsAbs(e) || e != path.Clean(e) || e == "." || e == ".." || strings.HasPrefix(e, "../") {
			return nil, true, fmt.Errorf("%s lists %q, which isn't a path inside %s; fix or delete %s", ListFile, e, root, filepath.Join(root, ListFile))
		}
		entries = append(entries, e)
	}
	return entries, true, nil
}

// checkListPaths applies checkPaths's rules to the list and its temporary
// file, which apply and remove read and replace.
func checkListPaths(root string) error {
	return checkPaths(root, []Change{{"write", ListFile}, {"write", listTemp}})
}

// checkPaths refuses a link anywhere between root and a changed path, a
// file where a directory belongs, and a directory where a file belongs.
func checkPaths(root string, changes []Change) error {
	for _, c := range changes {
		parts := strings.Split(c.Path, "/")
		cur := root
		for i, part := range parts {
			cur = filepath.Join(cur, part)
			rel := path.Join(parts[:i+1]...)
			fi, err := os.Lstat(cur)
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				return err
			}
			switch {
			case fi.Mode()&fs.ModeSymlink != 0:
				return fmt.Errorf("%s is a link; ocx won't write or delete through links, so remove it first", rel)
			case i < len(parts)-1 && !fi.IsDir():
				return fmt.Errorf("%s is a file where ocx needs a directory; remove it first", rel)
			case i == len(parts)-1 && fi.IsDir():
				return fmt.Errorf("%s is a directory where ocx expects a file; remove it first", rel)
			}
		}
	}
	return nil
}

// deleteFile deletes full, if it's still there, and then each parent
// directory up to root that the deletion left empty. A directory that still
// holds something ends the cleanup. One that's already gone, because an
// earlier run deleted it before failing higher up, doesn't. Any other
// failure is an error, so the caller keeps the list for a rerun.
func deleteFile(root, full string) error {
	rel := func(p string) string {
		r, _ := filepath.Rel(root, p)
		return filepath.ToSlash(r)
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete %s: %w", rel(full), err)
	}
	for dir := filepath.Dir(full); dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
		err := os.Remove(dir)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
			break
		}
		return fmt.Errorf("delete the emptied directory %s: %w", rel(dir), err)
	}
	return nil
}

// writeList replaces root's ListFile by writing a temporary file and
// renaming it, so a failure leaves the previous list whole.
func writeList(root string, paths []string) error {
	tmp := filepath.Join(root, listTemp)
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write %s: %w", ListFile, err)
	}
	if err := writeFile(tmp, []byte(strings.Join(paths, "\n")+"\n"), 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", ListFile, err)
	}
	if err := rename(tmp, filepath.Join(root, ListFile)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", ListFile, err)
	}
	return nil
}
