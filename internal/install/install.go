// Package install links this repository's agents, skills, and commands into
// OpenCode's global configuration directory, and reverses it.
//
// Everything is a symlink into the repository, so pulling new commits
// updates the installed agents. Files the install would replace are moved
// to a hidden backup directory, which OpenCode doesn't scan, and a manifest
// records every link and backup so uninstall restores the previous state.
package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tailscale/hujson"

	"github.com/plombardi89/ocx/internal/config"
)

// File names inside the configuration directory.
const (
	ManifestName = ".ocx-install.json"
	BackupDir    = ".ocx-backups"
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

// Item is one link: Path inside the configuration directory, pointing at
// Target inside the repository.
type Item struct {
	Path   string // relative to the configuration directory
	Target string // absolute
}

// Desired lists the links an install of root creates.
func Desired(root string) ([]Item, error) {
	items := []Item{
		{"opencode.jsonc", filepath.Join(root, config.ConfigFile)},
		{"prompts", filepath.Join(root, config.PromptsDir)},
		{"AGENTS.md", filepath.Join(root, config.InstructionsFile)},
	}
	skills, err := config.LoadSkills(root)
	if err != nil {
		return nil, err
	}
	for _, s := range skills {
		items = append(items, Item{filepath.Join("skills", s.ID), s.Dir})
	}
	commands, err := config.LoadCommands(root)
	if err != nil {
		return nil, err
	}
	for _, c := range commands {
		items = append(items, Item{filepath.Join("commands", c.Name+".md"), c.Path})
	}
	for _, it := range items {
		if _, err := os.Stat(it.Target); err != nil {
			return nil, fmt.Errorf("missing install source: %w", err)
		}
	}
	return items, nil
}

// Manifest records what an install created, for uninstall and doctor.
type Manifest struct {
	Root      string            `json:"root"`
	Installed string            `json:"installed"`
	Links     []string          `json:"links"`
	Backups   map[string]string `json:"backups"` // original path -> backup path, both relative
	Dirs      []string          `json:"dirs"`    // directories install created, relative
}

// ReadManifest returns the manifest in dir, or nil if there is none.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	return &m, nil
}

// writeManifest replaces the manifest in one step, so a failed write leaves
// the previous one intact.
func writeManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ManifestName+".tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, ManifestName)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Action kinds.
const (
	Keep   = "keep"   // already linked correctly
	Link   = "link"   // create a link
	Relink = "relink" // replace a link that points somewhere else
	Backup = "backup" // move an existing file aside, then link
	Remove = "remove" // remove a link this install no longer needs
	Record = "record" // write the manifest that uninstall and doctor rely on
)

// Action is one step of a plan.
type Action struct {
	Kind   string
	Path   string // relative to the configuration directory
	Target string
	Note   string
}

// Plan is what Install would do.
type Plan struct {
	Dir       string
	Root      string
	Actions   []Action
	Conflicts []string
}

// Changes reports whether applying the plan would change anything.
func (p *Plan) Changes() bool {
	for _, a := range p.Actions {
		if a.Kind != Keep {
			return true
		}
	}
	return false
}

// PlanInstall works out how to install root into dir. Existing files that
// aren't ours are conflicts unless they are safe to replace (a global config
// holding only "$schema") or force is set; either way they are backed up.
func PlanInstall(root, dir string, force bool) (*Plan, error) {
	items, err := Desired(root)
	if err != nil {
		return nil, err
	}
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	ours := map[string]bool{}
	if m != nil {
		for _, l := range m.Links {
			ours[l] = true
		}
	}

	p := &Plan{Dir: dir, Root: root}
	wanted := map[string]bool{}
	for _, it := range items {
		wanted[it.Path] = true
		p.Actions = append(p.Actions, planItem(p, it, ours[it.Path], force))
	}

	// The other global config file name would also be loaded alongside ours.
	other := "opencode.json"
	if _, err := os.Lstat(filepath.Join(dir, other)); err == nil {
		a := Action{Kind: Backup, Path: other, Note: "OpenCode would also load it"}
		if !trivialConfig(filepath.Join(dir, other)) && !force {
			p.Conflicts = append(p.Conflicts, fmt.Sprintf("%s has settings of its own; merge what you need into the ocx configuration, then rerun with --force to back it up", other))
		}
		p.Actions = append(p.Actions, a)
	}

	// Links from an earlier install that this version no longer creates.
	if m != nil {
		for _, l := range m.Links {
			if wanted[l] {
				continue
			}
			if target, err := os.Readlink(filepath.Join(dir, l)); err == nil && within(target, m.Root) {
				p.Actions = append(p.Actions, Action{Kind: Remove, Path: l, Target: target, Note: "no longer part of ocx"})
			}
		}
	}

	// Links that are already right still need a manifest that lists them.
	switch {
	case m == nil:
		p.Actions = append(p.Actions, Action{Kind: Record, Path: ManifestName, Note: "none yet"})
	case m.Root != root:
		p.Actions = append(p.Actions, Action{Kind: Record, Path: ManifestName, Note: "was for " + m.Root})
	case !sameSet(m.Links, wanted):
		p.Actions = append(p.Actions, Action{Kind: Record, Path: ManifestName})
	}
	sort.SliceStable(p.Actions, func(i, j int) bool { return p.Actions[i].Path < p.Actions[j].Path })
	return p, nil
}

func planItem(p *Plan, it Item, ours, force bool) Action {
	path := filepath.Join(p.Dir, it.Path)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Action{Kind: Link, Path: it.Path, Target: it.Target}
	}
	if err != nil {
		p.Conflicts = append(p.Conflicts, fmt.Sprintf("%s: %v", it.Path, err))
		return Action{Kind: Keep, Path: it.Path}
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(path)
		switch {
		case target == it.Target:
			return Action{Kind: Keep, Path: it.Path, Target: it.Target}
		case ours:
			return Action{Kind: Relink, Path: it.Path, Target: it.Target, Note: "was " + target}
		case force:
			return Action{Kind: Backup, Path: it.Path, Target: it.Target, Note: "link to " + target}
		default:
			p.Conflicts = append(p.Conflicts, fmt.Sprintf("%s is a link to %s, which ocx didn't create; rerun with --force to back it up", it.Path, target))
			return Action{Kind: Keep, Path: it.Path}
		}
	}
	if it.Path == "opencode.jsonc" && trivialConfig(path) {
		return Action{Kind: Backup, Path: it.Path, Target: it.Target, Note: "holds only $schema"}
	}
	if force {
		return Action{Kind: Backup, Path: it.Path, Target: it.Target}
	}
	reason := "exists and isn't from ocx"
	if it.Path == "opencode.jsonc" {
		reason = "has settings of its own; merge what you need into config/opencode.jsonc"
	}
	p.Conflicts = append(p.Conflicts, fmt.Sprintf("%s %s; rerun with --force to back it up", it.Path, reason))
	return Action{Kind: Keep, Path: it.Path}
}

// trivialConfig reports whether a config file sets nothing but "$schema".
func trivialConfig(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	std, err := hujson.Standardize(data)
	if err != nil {
		return false
	}
	var v map[string]any
	if json.Unmarshal(std, &v) != nil {
		return false
	}
	for k := range v {
		if k != "$schema" {
			return false
		}
	}
	return true
}

func sameSet(list []string, set map[string]bool) bool {
	seen := map[string]bool{}
	for _, x := range list {
		if !set[x] {
			return false
		}
		seen[x] = true
	}
	return len(seen) == len(set)
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Apply carries out a plan that has no conflicts, then records it in the
// manifest. If any step fails, including writing the manifest, it undoes the
// steps already taken, so the directory is left as it was.
func Apply(p *Plan, now time.Time) (err error) {
	if len(p.Conflicts) > 0 {
		return errors.New("plan has conflicts")
	}
	m, err := ReadManifest(p.Dir)
	if err != nil {
		return err
	}
	if m == nil {
		m = &Manifest{}
	}
	if m.Backups == nil {
		m.Backups = map[string]string{}
	}

	var undo []func() error
	var created []string
	defer func() {
		if err == nil {
			return
		}
		var failed []string
		for i := len(undo) - 1; i >= 0; i-- {
			if uerr := undo[i](); uerr != nil {
				failed = append(failed, uerr.Error())
			}
		}
		if len(failed) > 0 {
			err = fmt.Errorf("%w; undoing the partial install also failed, so check %s by hand: %s", err, p.Dir, strings.Join(failed, "; "))
		}
	}()
	mkdirs := func(dir string) error {
		var missing []string
		for d := dir; ; d = filepath.Dir(d) {
			if _, err := os.Lstat(d); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			missing = append(missing, d)
			if filepath.Dir(d) == d {
				break
			}
		}
		for i := len(missing) - 1; i >= 0; i-- {
			d := missing[i]
			if err := os.Mkdir(d, 0o755); err != nil {
				return err
			}
			undo = append(undo, func() error { return os.Remove(d) })
			if rel, err := filepath.Rel(p.Dir, d); err == nil && rel != "." && within(d, p.Dir) {
				created = append(created, rel)
			}
		}
		return nil
	}
	link := func(target, path string) error {
		if err := mkdirs(filepath.Dir(path)); err != nil {
			return err
		}
		if err := os.Symlink(target, path); err != nil {
			return err
		}
		undo = append(undo, func() error { return os.Remove(path) })
		return nil
	}
	remove := func(path string) error {
		old, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		undo = append(undo, func() error { return os.Symlink(old, path) })
		return nil
	}
	// Each install backs up into a directory of its own that it creates, so
	// a backup can never replace an earlier one.
	backupRoot := ""
	newBackupRoot := func() (string, error) {
		if err := mkdirs(filepath.Join(p.Dir, BackupDir)); err != nil {
			return "", err
		}
		stamp := now.UTC().Format("20060102T150405Z")
		for i := 1; i <= 100; i++ {
			rel := filepath.Join(BackupDir, stamp)
			if i > 1 {
				rel += fmt.Sprintf("-%d", i)
			}
			dir := filepath.Join(p.Dir, rel)
			err := os.Mkdir(dir, 0o755)
			if err == nil {
				undo = append(undo, func() error { return os.Remove(dir) })
				created = append(created, rel)
				return rel, nil
			}
			if !errors.Is(err, os.ErrExist) {
				return "", err
			}
		}
		return "", fmt.Errorf("too many backups named %s in %s", stamp, filepath.Join(p.Dir, BackupDir))
	}

	if err := mkdirs(p.Dir); err != nil {
		return err
	}
	var links []string
	for _, a := range p.Actions {
		path := filepath.Join(p.Dir, a.Path)
		switch a.Kind {
		case Keep:
			if a.Target != "" {
				links = append(links, a.Path)
			}
		case Link, Relink:
			if a.Kind == Relink {
				if err := remove(path); err != nil {
					return err
				}
			}
			if err := link(a.Target, path); err != nil {
				return err
			}
			links = append(links, a.Path)
		case Backup:
			if backupRoot == "" {
				if backupRoot, err = newBackupRoot(); err != nil {
					return err
				}
			}
			rel := filepath.Join(backupRoot, a.Path)
			dst := filepath.Join(p.Dir, rel)
			if err := mkdirs(filepath.Dir(dst)); err != nil {
				return err
			}
			if err := os.Rename(path, dst); err != nil {
				return err
			}
			undo = append(undo, func() error { return os.Rename(dst, path) })
			if _, done := m.Backups[a.Path]; !done {
				m.Backups[a.Path] = rel
			}
			if a.Target != "" {
				if err := link(a.Target, path); err != nil {
					return err
				}
				links = append(links, a.Path)
			}
		case Remove:
			if err := remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	m.Root = p.Root
	m.Installed = now.UTC().Format(time.RFC3339)
	m.Links = links
	for _, d := range created {
		if !slices.Contains(m.Dirs, d) {
			m.Dirs = append(m.Dirs, d)
		}
	}
	sort.Strings(m.Dirs)
	return writeManifest(p.Dir, m)
}

// Uninstall removes every link the manifest in dir records, as long as it
// still points into the installed repository, and restores the backups.
// With apply false it only describes what it would do.
func Uninstall(dir string, apply bool) ([]string, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("no ocx install found in %s", dir)
	}
	var steps []string
	removed := map[string]bool{}
	for _, l := range m.Links {
		path := filepath.Join(dir, l)
		target, err := os.Readlink(path)
		if err != nil || !within(target, m.Root) {
			steps = append(steps, "skip "+l+" (no longer an ocx link)")
			continue
		}
		steps = append(steps, "remove link "+l)
		removed[l] = true
		if apply {
			if err := os.Remove(path); err != nil {
				return steps, err
			}
		}
	}
	originals := make([]string, 0, len(m.Backups))
	for orig := range m.Backups {
		originals = append(originals, orig)
	}
	sort.Strings(originals)
	for _, orig := range originals {
		backup := m.Backups[orig]
		_, err := os.Lstat(filepath.Join(dir, orig))
		free := removed[orig] || errors.Is(err, os.ErrNotExist)
		if !free {
			steps = append(steps, "keep backup "+backup+" ("+orig+" is in use)")
			continue
		}
		steps = append(steps, "restore "+orig+" from "+backup)
		if apply {
			if err := os.Rename(filepath.Join(dir, backup), filepath.Join(dir, orig)); err != nil {
				return steps, err
			}
		}
	}
	steps = append(steps, "remove "+ManifestName)
	if len(m.Dirs) > 0 {
		steps = append(steps, "remove the directories install created, if they are empty")
	}
	if apply {
		if err := os.Remove(filepath.Join(dir, ManifestName)); err != nil {
			return steps, err
		}
		// Deepest first, so a parent is empty by the time it is tried.
		dirs := slices.Clone(m.Dirs)
		sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
		for _, d := range dirs {
			removeEmptyDir(filepath.Join(dir, d))
		}
	}
	return steps, nil
}

// removeEmptyDir removes path if it is an empty directory. A link to a
// directory is left alone even though os.Remove would delete the link.
func removeEmptyDir(path string) {
	if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
		_ = os.Remove(path) // fails, as it should, unless empty
	}
}

// Describe formats a plan for people.
func (p *Plan) Describe() string {
	var b bytes.Buffer
	for _, a := range p.Actions {
		line := fmt.Sprintf("%-7s %s", a.Kind, a.Path)
		if a.Target != "" && a.Kind != Remove {
			line += " -> " + a.Target
		}
		if a.Note != "" {
			line += "  (" + a.Note + ")"
		}
		b.WriteString(line + "\n")
	}
	for _, c := range p.Conflicts {
		b.WriteString("CONFLICT " + c + "\n")
	}
	return b.String()
}
