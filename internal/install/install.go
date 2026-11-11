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

func writeManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ManifestName), append(data, '\n'), 0o644)
}

// Action kinds.
const (
	Keep   = "keep"   // already linked correctly
	Link   = "link"   // create a link
	Relink = "relink" // replace a link that points somewhere else
	Backup = "backup" // move an existing file aside, then link
	Remove = "remove" // remove a link this install no longer needs
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
	if fi, err := os.Lstat(filepath.Join(dir, other)); err == nil {
		a := Action{Kind: Backup, Path: other, Note: "OpenCode would also load it"}
		if fi.Mode()&os.ModeSymlink == 0 && !trivialConfig(filepath.Join(dir, other)) && !force {
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

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Apply carries out a plan that has no conflicts, then records it in the
// manifest.
func Apply(p *Plan, now time.Time) error {
	if len(p.Conflicts) > 0 {
		return errors.New("plan has conflicts")
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
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
	stamp := now.UTC().Format("20060102T150405Z")
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
				if err := os.Remove(path); err != nil {
					return err
				}
			}
			if err := link(a.Target, path); err != nil {
				return err
			}
			links = append(links, a.Path)
		case Backup:
			rel := filepath.Join(BackupDir, stamp, a.Path)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(p.Dir, rel)), 0o755); err != nil {
				return err
			}
			if err := os.Rename(path, filepath.Join(p.Dir, rel)); err != nil {
				return err
			}
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
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	m.Root = p.Root
	m.Installed = now.UTC().Format(time.RFC3339)
	m.Links = links
	return writeManifest(p.Dir, m)
}

func link(target, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
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
	if apply {
		if err := os.Remove(filepath.Join(dir, ManifestName)); err != nil {
			return steps, err
		}
		_ = removeEmptyDirs(filepath.Join(dir, BackupDir))
		for _, sub := range []string{"skills", "commands"} {
			_ = os.Remove(filepath.Join(dir, sub)) // only succeeds if empty
		}
	}
	return steps, nil
}

// removeEmptyDirs removes dir and its subdirectories if they hold no files.
func removeEmptyDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			_ = removeEmptyDirs(filepath.Join(dir, e.Name()))
		}
	}
	return os.Remove(dir)
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
