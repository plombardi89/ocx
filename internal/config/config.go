// Package config loads the ocx sources: the OpenCode configuration,
// agent prompts, skills, and commands.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
	"gopkg.in/yaml.v3"

	"github.com/plombardi89/ocx/internal/perm"
)

// Paths relative to the repository root.
const (
	ConfigFile       = "config/opencode.jsonc"
	InstructionsFile = "config/global-AGENTS.md"
	PromptsDir       = "config/prompts"
	SkillsDir        = "skills"
	CommandsDir      = "commands"
	HookFile         = "hooks/pre-push"
	ProbesFile       = "tests/permissions.yaml"
	ScenariosDir     = "evals/scenarios"
	FixturesDir      = "evals/fixtures"
)

// ScratchRoots maps each agent that may write files to the only directory it
// may write in. Every other custom agent is read-only.
var ScratchRoots = map[string]string{
	"reviewer": "/tmp/opencode/reviews",
	"debugger": "/tmp/opencode/debug",
}

// BuiltinAgents are the agents OpenCode ships. Overrides of these must not
// replace their system prompts.
var BuiltinAgents = map[string]bool{
	"build": true, "plan": true, "general": true, "explore": true,
	"compaction": true, "title": true, "summary": true,
}

// Config is the subset of opencode.jsonc that ocx reasons about.
type Config struct {
	Schema       string           `json:"$schema"`
	Model        string           `json:"model"`
	Permissions  []perm.Rule      `json:"permissions"`
	Policies     []perm.Rule      `json:"policies"`
	Agents       map[string]Agent `json:"agents"`
	Instructions []string         `json:"instructions"`
}

// Agent is one entry under "agents".
type Agent struct {
	Description string      `json:"description"`
	Mode        string      `json:"mode"`
	Model       string      `json:"model"`
	System      string      `json:"system"`
	Permissions []perm.Rule `json:"permissions"`
	Hidden      bool        `json:"hidden"`
	Steps       int         `json:"steps"`
	Disabled    bool        `json:"disabled"`
}

// FindRoot walks up from dir to the ocx repository root.
func FindRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ConfigFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside the ocx repository (no " + ConfigFile + " found)")
		}
		dir = parent
	}
}

// Load parses the OpenCode configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	std, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid JSONC: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(std))
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// AgentIDs returns the configured agent IDs in sorted order.
func (c *Config) AgentIDs() []string {
	ids := make([]string, 0, len(c.Agents))
	for id := range c.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

var fileRef = regexp.MustCompile(`^\{file:(.+)\}$`)

// SystemFile returns the path referenced by an agent's "{file:...}" system
// prompt, resolved relative to the config file, or "" if it is inline.
func SystemFile(configPath, system string) string {
	m := fileRef.FindStringSubmatch(strings.TrimSpace(system))
	if m == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), m[1])
}

// Model is a parsed "provider/model#variant" reference.
type Model struct {
	Provider, ID, Variant string
}

func (m Model) String() string {
	s := m.Provider + "/" + m.ID
	if m.Variant != "" {
		s += "#" + m.Variant
	}
	return s
}

var modelRef = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)/([A-Za-z0-9][A-Za-z0-9._-]*)(?:#([a-z]+))?$`)

// ParseModel parses a "provider/model#variant" reference.
func ParseModel(s string) (Model, error) {
	m := modelRef.FindStringSubmatch(s)
	if m == nil {
		return Model{}, fmt.Errorf("model %q is not in provider/model#variant form", s)
	}
	return Model{Provider: m[1], ID: m[2], Variant: m[3]}, nil
}

// Frontmatter splits a Markdown document into YAML frontmatter and body.
// Documents without frontmatter return a nil map.
func Frontmatter(data []byte) (map[string]any, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, text, nil
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
		} else {
			return nil, "", errors.New("unterminated frontmatter")
		}
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return nil, "", fmt.Errorf("frontmatter: %w", err)
	}
	body := ""
	if end+len("\n---\n") <= len(rest) {
		body = rest[end+len("\n---\n"):]
	}
	return fm, body, nil
}

// Skill is a directory-form skill: skills/<id>/SKILL.md.
type Skill struct {
	ID          string
	Dir         string
	Name        string
	Description string
	Body        string
	Meta        map[string]any
}

// LoadSkills reads every skills/<id>/SKILL.md under root.
func LoadSkills(root string) ([]Skill, error) {
	entries, err := os.ReadDir(filepath.Join(root, SkillsDir))
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, SkillsDir, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", e.Name(), err)
		}
		fm, body, err := Frontmatter(data)
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", e.Name(), err)
		}
		s := Skill{ID: e.Name(), Dir: dir, Body: body, Meta: fm}
		s.Name, _ = fm["name"].(string)
		s.Description, _ = fm["description"].(string)
		skills = append(skills, s)
	}
	return skills, nil
}

// Command is a Markdown slash command.
type Command struct {
	Name string
	Path string
	Meta map[string]any
	Body string
}

// LoadCommands reads every commands/**/*.md under root.
func LoadCommands(root string) ([]Command, error) {
	base := filepath.Join(root, CommandsDir)
	var cmds []Command
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fm, body, err := Frontmatter(data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		rel, _ := filepath.Rel(base, path)
		cmds = append(cmds, Command{
			Name: strings.TrimSuffix(filepath.ToSlash(rel), ".md"),
			Path: path,
			Meta: fm,
			Body: body,
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return cmds, err
}
