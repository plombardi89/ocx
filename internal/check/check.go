// Package check validates the ocx sources: configuration, agents,
// skills, commands, the review gate, eval scenarios, and (with OpenCode
// available) the fully resolved permissions of every agent.
package check

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/eval"
	"github.com/plombardi89/ocx/internal/gitx"
	"github.com/plombardi89/ocx/internal/hook"
	"github.com/plombardi89/ocx/internal/perm"
	"github.com/plombardi89/ocx/internal/stage"
)

// Status values for a Result.
const (
	Pass = "PASS"
	Fail = "FAIL"
	Warn = "WARN"
	Skip = "SKIP"
)

// Result is the outcome of one named check.
type Result struct {
	Name    string
	Status  string
	Details []string
}

// Options control which checks run.
type Options struct {
	// Offline skips checks that run OpenCode (model availability and
	// resolved permissions).
	Offline bool
}

// Run executes every check against the repository at root.
func Run(root string, opts Options) []Result {
	var results []Result
	cfgPath := filepath.Join(root, config.ConfigFile)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return []Result{{Name: "config parses", Status: Fail, Details: []string{err.Error()}}}
	}
	results = append(results, Result{Name: "config parses", Status: Pass})

	skills, skillErr := config.LoadSkills(root)
	commands, cmdErr := config.LoadCommands(root)

	results = append(results,
		checkModels(root, cfg, commands, opts),
		checkAgents(cfgPath, cfg),
		checkPrompts(root, cfgPath, cfg),
		checkBaseline(cfg),
		checkShellAllowlists(cfg),
		checkWriteConfinement(cfg),
		checkSkills(skills, skillErr),
		checkCommands(cfg, commands, cmdErr),
		checkReferences(root, cfg, skills),
		checkHook(root),
		checkScenarios(root),
		checkOwnWriting(root),
	)
	if opts.Offline {
		results = append(results, Result{Name: "resolved permissions", Status: Skip, Details: []string{"offline"}})
	} else {
		results = append(results, checkResolved(root, cfgPath, cfg))
	}
	return results
}

func result(name string, problems []string) Result {
	if len(problems) == 0 {
		return Result{Name: name, Status: Pass}
	}
	return Result{Name: name, Status: Fail, Details: problems}
}

var (
	validVariants = map[string]bool{"none": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	maiModel      = regexp.MustCompile(`(?i)[a-z0-9_-]+/mai-[a-z0-9.-]+`)
)

func checkModels(root string, cfg *config.Config, commands []config.Command, opts Options) Result {
	refs := map[string]string{} // model -> where
	if cfg.Model != "" {
		refs[cfg.Model] = "model"
	}
	for _, id := range cfg.AgentIDs() {
		if m := cfg.Agents[id].Model; m != "" {
			refs[m] = "agents." + id
		}
	}
	for _, c := range commands {
		if m, ok := c.Meta["model"].(string); ok {
			refs[m] = "commands/" + c.Name
		}
	}
	var problems []string
	for ref, where := range refs {
		m, err := config.ParseModel(ref)
		if err != nil {
			problems = append(problems, where+": "+err.Error())
			continue
		}
		if m.Variant != "" && !validVariants[m.Variant] {
			problems = append(problems, fmt.Sprintf("%s: unknown variant %q in %s", where, m.Variant, ref))
		}
	}

	// MAI models are banned everywhere, not just in model fields.
	for _, dir := range []string{"config", config.SkillsDir, config.CommandsDir, config.ScenariosDir, "tests"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil {
				for _, m := range maiModel.FindAllString(string(data), -1) {
					rel, _ := filepath.Rel(root, path)
					problems = append(problems, fmt.Sprintf("%s: MAI model %q is not allowed", rel, m))
				}
			}
			return nil
		})
	}

	if len(problems) == 0 && !opts.Offline {
		available, err := availableModels()
		if err != nil {
			return Result{Name: "models", Status: Warn, Details: []string{"could not list models: " + err.Error()}}
		}
		for ref, where := range refs {
			m, _ := config.ParseModel(ref)
			if !available[m.Provider+"/"+m.ID] {
				problems = append(problems, fmt.Sprintf("%s: %s is not available (opencode models)", where, ref))
			}
		}
	}
	sort.Strings(problems)
	return result("models", problems)
}

func availableModels() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, opencodeBin(), "models").Output()
	if err != nil {
		return nil, err
	}
	models := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			models[line] = true
		}
	}
	return models, nil
}

func opencodeBin() string {
	if bin := os.Getenv("OCX_OPENCODE"); bin != "" {
		return bin
	}
	return "opencode"
}

func isDenyAll(r perm.Rule) bool {
	return r.Action == "*" && r.Resource == "*" && r.Effect == perm.Deny
}

func checkAgents(cfgPath string, cfg *config.Config) Result {
	var problems []string
	for _, id := range cfg.AgentIDs() {
		a := cfg.Agents[id]
		for _, r := range a.Permissions {
			if r.Effect != perm.Allow && r.Effect != perm.Ask && r.Effect != perm.Deny {
				problems = append(problems, fmt.Sprintf("agents.%s: invalid effect %q", id, r.Effect))
			}
		}
		if config.BuiltinAgents[id] {
			if a.System != "" {
				problems = append(problems, fmt.Sprintf("agents.%s: do not replace a built-in agent's system prompt; override permissions or model only", id))
			}
			continue
		}
		if a.Description == "" {
			problems = append(problems, fmt.Sprintf("agents.%s: missing description", id))
		}
		switch a.Mode {
		case "primary", "subagent", "all":
		default:
			problems = append(problems, fmt.Sprintf("agents.%s: mode must be primary, subagent, or all (got %q)", id, a.Mode))
		}
		if a.Model == "" {
			problems = append(problems, fmt.Sprintf("agents.%s: custom agents must pin a model", id))
		}
		file := config.SystemFile(cfgPath, a.System)
		if file == "" {
			problems = append(problems, fmt.Sprintf("agents.%s: system must be a {file:./prompts/<name>.md} reference", id))
		} else if data, err := os.ReadFile(file); err != nil || len(strings.TrimSpace(string(data))) == 0 {
			problems = append(problems, fmt.Sprintf("agents.%s: system prompt file %s is missing or empty", id, file))
		}
		if len(a.Permissions) == 0 || !isDenyAll(a.Permissions[0]) {
			problems = append(problems, fmt.Sprintf("agents.%s: permissions must start with {\"action\": \"*\", \"resource\": \"*\", \"effect\": \"deny\"}", id))
		}
	}
	return result("agents", problems)
}

func checkPrompts(root, cfgPath string, cfg *config.Config) Result {
	used := map[string]bool{}
	for _, a := range cfg.Agents {
		if f := config.SystemFile(cfgPath, a.System); f != "" {
			used[filepath.Clean(f)] = true
		}
	}
	var problems []string
	entries, _ := os.ReadDir(filepath.Join(root, config.PromptsDir))
	for _, e := range entries {
		path := filepath.Clean(filepath.Join(root, config.PromptsDir, e.Name()))
		if !used[path] {
			problems = append(problems, fmt.Sprintf("%s/%s is not used by any agent", config.PromptsDir, e.Name()))
		}
	}
	return result("prompts", problems)
}

func hasRule(rules []perm.Rule, want perm.Rule) bool {
	for _, r := range rules {
		if r == want {
			return true
		}
	}
	return false
}

func checkBaseline(cfg *config.Config) Result {
	required := []perm.Rule{
		{Action: "shell", Resource: "*", Effect: perm.Ask},
		{Action: "shell", Resource: "git push *", Effect: perm.Deny},
		{Action: "shell", Resource: "git commit *", Effect: perm.Deny},
		{Action: "shell", Resource: "git notes *", Effect: perm.Deny},
		{Action: "read", Resource: "*.env", Effect: perm.Deny},
		{Action: "edit", Resource: ".git/*", Effect: perm.Deny},
		{Action: "external_directory", Resource: "*", Effect: perm.Ask},
		{Action: "subagent", Resource: "*", Effect: perm.Deny},
	}
	var problems []string
	for _, r := range required {
		if !hasRule(cfg.Permissions, r) {
			problems = append(problems, fmt.Sprintf("baseline permissions must include %s %q %s", r.Action, r.Resource, r.Effect))
		}
	}
	if len(cfg.Permissions) == 0 || cfg.Permissions[0] != required[0] {
		problems = append(problems, `the first baseline rule must be {"action": "shell", "resource": "*", "effect": "ask"}`)
	}
	return result("baseline permissions", problems)
}

// shellExtras returns the shell allows beyond the read-only baseline that a
// custom agent may hold: the reviewer's verdict note, and for agents with a
// scratch root, managing and running code in scratch worktrees there.
func shellExtras(agent string) map[string]bool {
	extras := map[string]bool{}
	if agent == "reviewer" {
		extras["git notes --ref=ocreview add *"] = true
	}
	if root, ok := config.ScratchRoots[agent]; ok {
		for _, p := range []string{
			"git worktree add --detach " + root + "/* *",
			"git worktree remove --force " + root + "/*",
			"git worktree prune",
			"git worktree list *",
			"bash " + root + "/*",
			"sh " + root + "/*",
			"go run *",
		} {
			extras[p] = true
		}
	}
	return extras
}

func checkShellAllowlists(cfg *config.Config) Result {
	baseline := map[string]bool{}
	for _, r := range cfg.Permissions {
		if r.Action == "shell" && r.Effect == perm.Allow {
			baseline[r.Resource] = true
		}
	}
	var problems []string
	for _, id := range cfg.AgentIDs() {
		if config.BuiltinAgents[id] {
			continue
		}
		extras := shellExtras(id)
		for _, r := range cfg.Agents[id].Permissions {
			if r.Action != "shell" || r.Effect != perm.Allow || baseline[r.Resource] || extras[r.Resource] {
				continue
			}
			problems = append(problems, fmt.Sprintf("agents.%s: shell allow %q is not in the read-only baseline", id, r.Resource))
		}
	}
	probe := "git notes --ref=ocreview add -f -m forged HEAD"
	for _, id := range cfg.AgentIDs() {
		if id == "reviewer" {
			continue
		}
		for _, r := range cfg.Agents[id].Permissions {
			if r.Action == "shell" && r.Effect == perm.Allow && perm.Match(r.Resource, probe) {
				problems = append(problems, fmt.Sprintf("agents.%s: may write review notes (%q); only reviewer may", id, r.Resource))
			}
		}
	}
	return result("custom agent shell allowlists", problems)
}

// checkWriteConfinement ensures custom agents write files, and reach
// directories outside the project, only in their own scratch root (plus the
// read-only external directories the baseline already allows). Agents
// without a scratch root may not write at all.
func checkWriteConfinement(cfg *config.Config) Result {
	external := map[string]bool{}
	for _, r := range cfg.Permissions {
		if r.Action == "external_directory" && r.Effect == perm.Allow {
			external[r.Resource] = true
		}
	}
	var problems []string
	for _, id := range cfg.AgentIDs() {
		if config.BuiltinAgents[id] {
			continue
		}
		scratch := ""
		if root, ok := config.ScratchRoots[id]; ok {
			scratch = root + "/*"
		}
		for _, r := range cfg.Agents[id].Permissions {
			if r.Effect != perm.Allow {
				continue
			}
			switch r.Action {
			case "edit", "write", "patch":
				if scratch == "" {
					problems = append(problems, fmt.Sprintf("agents.%s: %s allow %q, but this agent has no scratch root and must be read-only", id, r.Action, r.Resource))
				} else if r.Resource != scratch {
					problems = append(problems, fmt.Sprintf("agents.%s: %s allow %q is outside %s", id, r.Action, r.Resource, scratch))
				}
			case "external_directory":
				if (scratch == "" || r.Resource != scratch) && !external[r.Resource] {
					problems = append(problems, fmt.Sprintf("agents.%s: external_directory allow %q is neither its scratch root nor in the baseline", id, r.Resource))
				}
			}
		}
	}
	return result("custom agent write confinement", problems)
}

var skillID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var skillFileRef = regexp.MustCompile(`\b((?:references|templates|scripts)/[A-Za-z0-9._/-]+\.[A-Za-z0-9]+)`)

func checkSkills(skills []config.Skill, loadErr error) Result {
	if loadErr != nil {
		return result("skills", []string{loadErr.Error()})
	}
	var problems []string
	for _, s := range skills {
		if !skillID.MatchString(s.ID) || len(s.ID) > 64 {
			problems = append(problems, fmt.Sprintf("skills/%s: ID must be lowercase kebab-case, at most 64 characters", s.ID))
		}
		if s.Name != s.ID {
			problems = append(problems, fmt.Sprintf("skills/%s: frontmatter name %q must equal the directory name", s.ID, s.Name))
		}
		if !strings.HasPrefix(s.Description, "Use when") {
			problems = append(problems, fmt.Sprintf("skills/%s: description must start with \"Use when\" and say only when to use the skill", s.ID))
		}
		if len(s.Description) > 1024 {
			problems = append(problems, fmt.Sprintf("skills/%s: description is %d characters; keep it under 1024", s.ID, len(s.Description)))
		}
		for _, m := range skillFileRef.FindAllStringSubmatch(s.Body, -1) {
			if _, err := os.Stat(filepath.Join(s.Dir, m[1])); err != nil {
				problems = append(problems, fmt.Sprintf("skills/%s: references missing file %s", s.ID, m[1]))
			}
		}
	}
	return result("skills", problems)
}

func checkCommands(cfg *config.Config, commands []config.Command, loadErr error) Result {
	if loadErr != nil {
		return result("commands", []string{loadErr.Error()})
	}
	var problems []string
	for _, c := range commands {
		if d, _ := c.Meta["description"].(string); d == "" {
			problems = append(problems, fmt.Sprintf("commands/%s: missing description", c.Name))
		}
		if _, ok := c.Meta["template"]; ok {
			problems = append(problems, fmt.Sprintf("commands/%s: template comes from the Markdown body, not frontmatter", c.Name))
		}
		if agent, ok := c.Meta["agent"].(string); ok {
			if _, custom := cfg.Agents[agent]; !custom && !config.BuiltinAgents[agent] {
				problems = append(problems, fmt.Sprintf("commands/%s: unknown agent %q", c.Name, agent))
			}
		}
		if strings.TrimSpace(c.Body) == "" {
			problems = append(problems, fmt.Sprintf("commands/%s: empty template", c.Name))
		}
	}
	return result("commands", problems)
}

var (
	skillMention    = regexp.MustCompile("`([a-z0-9]+(?:-[a-z0-9]+)*)` skill")
	subagentMention = regexp.MustCompile("`([a-z0-9]+(?:-[a-z0-9]+)*)` subagent")
)

func checkReferences(root string, cfg *config.Config, skills []config.Skill) Result {
	known := map[string]bool{}
	for _, s := range skills {
		known[s.ID] = true
	}
	agents := map[string]bool{}
	for id := range config.BuiltinAgents {
		agents[id] = true
	}
	for id := range cfg.Agents {
		agents[id] = true
	}
	var problems []string
	var files []string
	for _, dir := range []string{config.PromptsDir, config.SkillsDir, config.CommandsDir} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Ext(path) == ".md" {
				files = append(files, path)
			}
			return nil
		})
	}
	files = append(files, filepath.Join(root, config.InstructionsFile))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range skillMention.FindAllStringSubmatch(string(data), -1) {
			if !known[m[1]] {
				problems = append(problems, fmt.Sprintf("%s: mentions unknown skill %q", rel, m[1]))
			}
		}
		for _, m := range subagentMention.FindAllStringSubmatch(string(data), -1) {
			if !agents[m[1]] {
				problems = append(problems, fmt.Sprintf("%s: mentions unknown subagent %q", rel, m[1]))
			}
		}
	}
	rules := map[string][]perm.Rule{"(baseline)": cfg.Permissions}
	for id, a := range cfg.Agents {
		rules["agents."+id] = a.Permissions
	}
	for where, rs := range rules {
		for _, r := range rs {
			if r.Effect != perm.Allow || strings.ContainsAny(r.Resource, "*?") {
				continue
			}
			if r.Action == "skill" && !known[r.Resource] {
				problems = append(problems, fmt.Sprintf("%s: allows unknown skill %q", where, r.Resource))
			}
			if r.Action == "subagent" && !agents[r.Resource] {
				problems = append(problems, fmt.Sprintf("%s: allows unknown subagent %q", where, r.Resource))
			}
		}
	}
	sort.Strings(problems)
	return result("references", problems)
}

func checkHook(root string) Result {
	path := filepath.Join(root, config.HookFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result("pre-push hook", []string{err.Error()})
	}
	var problems []string
	if !strings.Contains(string(data), hook.Marker) {
		problems = append(problems, "hook is missing the "+hook.Marker+" marker")
	}
	if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
		problems = append(problems, "sh -n: "+strings.TrimSpace(string(out)))
	}
	if len(problems) > 0 {
		return result("pre-push hook", problems)
	}
	if _, err := exec.LookPath("shellcheck"); err != nil {
		return Result{Name: "pre-push hook", Status: Warn, Details: []string{"shellcheck not installed; syntax checked with sh -n only"}}
	}
	if out, err := exec.Command("shellcheck", path).CombinedOutput(); err != nil {
		return result("pre-push hook", []string{"shellcheck:\n" + strings.TrimSpace(string(out))})
	}
	return result("pre-push hook", nil)
}

func checkScenarios(root string) Result {
	scenarios, err := eval.LoadScenarios(root)
	if err != nil {
		return result("eval scenarios", []string{err.Error()})
	}
	var problems []string
	for _, s := range scenarios {
		if err := s.Validate(root); err != nil {
			problems = append(problems, err.Error())
		}
	}
	return result("eval scenarios", problems)
}

var (
	// letterListItem matches list markers made of letters or roman numerals,
	// which the writing rules forbid ("a.", "b)", "iv.").
	letterListItem = regexp.MustCompile(`^\s*(?:[a-zA-Z]|[ivxIVX]{2,})[.)]\s+\S`)
	// coupledText matches comment text that points at plans, reviews, or
	// requests instead of standing on its own.
	coupledText = regexp.MustCompile(`(?i)\b(docs/plans|plan \d|finding \d|review finding|task \d+|as requested|per review)\b`)
)

// coupledGoComments returns the lines of Go comments in src that point at
// plans, reviews, or requests. It reads real comments with the Go parser, so
// string literals that merely look like comments are ignored.
func coupledGoComments(name string, src []byte) ([]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var lines []int
	for _, group := range f.Comments {
		for _, c := range group.List {
			if coupledText.MatchString(c.Text) {
				lines = append(lines, fset.Position(c.Pos()).Line)
			}
		}
	}
	return lines, nil
}

// checkOwnWriting applies the mechanical parts of the writing rules to this
// repository's own docs and code.
func checkOwnWriting(root string) Result {
	var problems []string
	visit := func(dir string, exts map[string]bool, check func(rel string, data []byte)) {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !exts[filepath.Ext(path)] {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil {
				rel, _ := filepath.Rel(root, path)
				check(rel, data)
			}
			return nil
		})
	}

	markdown := func(rel string, data []byte) {
		inFence := false
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if !inFence && letterListItem.MatchString(line) {
				problems = append(problems, fmt.Sprintf("%s:%d: number list items 1, 2, 3; letters and roman numerals aren't allowed", rel, i+1))
			}
		}
	}
	for _, dir := range []string{"config", config.SkillsDir, config.CommandsDir, "templates"} {
		visit(dir, map[string]bool{".md": true}, markdown)
	}
	if data, err := os.ReadFile(filepath.Join(root, "README.md")); err == nil {
		markdown("README.md", data)
	}
	if data, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err == nil {
		markdown("AGENTS.md", data)
	}

	const coupled = "comment refers to a plan, review, or request; state the reason itself"
	goCode := func(rel string, data []byte) {
		lines, err := coupledGoComments(rel, data)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
		}
		for _, line := range lines {
			problems = append(problems, fmt.Sprintf("%s:%d: %s", rel, line, coupled))
		}
	}
	for _, dir := range []string{"cmd", "internal"} {
		visit(dir, map[string]bool{".go": true}, goCode)
	}
	visit("hooks", map[string]bool{"": true}, func(rel string, data []byte) {
		for i, line := range strings.Split(string(data), "\n") {
			if text, ok := strings.CutPrefix(strings.TrimSpace(line), "#"); ok && coupledText.MatchString(text) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s", rel, i+1, coupled))
			}
		}
	})
	return result("writing rules (this repository)", problems)
}

type probeFile struct {
	Probes []struct {
		Agent    string `yaml:"agent"`
		Action   string `yaml:"action"`
		Resource string `yaml:"resource"`
		Expect   string `yaml:"expect"`
	} `yaml:"probes"`
}

// ResolvedAgent is one entry of `opencode debug agents`.
type ResolvedAgent struct {
	ID    string `json:"id"`
	Mode  string `json:"mode"`
	Model *struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
		Variant    string `json:"variant"`
	} `json:"model"`
	System      string      `json:"system"`
	Permissions []perm.Rule `json:"permissions"`
}

// Resolve stages the configuration into a scratch project and asks a private
// OpenCode server for the fully resolved agents.
func Resolve(root string) (map[string]ResolvedAgent, error) {
	dir, err := os.MkdirTemp(ScratchDir(), "ocx-check-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := gitx.Init(dir); err != nil {
		return nil, err
	}
	if err := stage.Stage(root, dir, stage.Options{}); err != nil {
		return nil, err
	}
	return ResolveIn(dir, nil)
}

// ResolveIn asks a private OpenCode server started in dir, with extra
// environment variables env, for the fully resolved agents.
func ResolveIn(dir string, env []string) (map[string]ResolvedAgent, error) {
	var list []ResolvedAgent
	if err := debugJSON(dir, env, "agents", &list); err != nil {
		return nil, err
	}
	agents := map[string]ResolvedAgent{}
	for _, a := range list {
		agents[a.ID] = a
	}
	return agents, nil
}

// ConfigSource is one configuration file or directory OpenCode loaded.
type ConfigSource struct {
	Type string `json:"type"` // "document" or "directory"
	Path string `json:"path"`
	Info struct {
		Policies []perm.Rule `json:"policies"`
	} `json:"info"`
}

// LoadedConfig asks a private OpenCode server started in dir, with extra
// environment variables env, which configuration sources it loads.
func LoadedConfig(dir string, env []string) ([]ConfigSource, error) {
	var sources []ConfigSource
	err := debugJSON(dir, env, "config", &sources)
	return sources, err
}

func debugJSON(dir string, env []string, what string, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, opencodeBin(), "debug", what, "--standalone")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("opencode debug %s: %w", what, err)
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("opencode debug %s: unexpected output: %w", what, err)
	}
	return nil
}

// ScratchDir returns the directory for ocx's temporary projects.
func ScratchDir() string {
	dir := filepath.Join(os.TempDir(), "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return os.TempDir()
	}
	return dir
}

func checkResolved(root, cfgPath string, cfg *config.Config) Result {
	const name = "resolved permissions"
	agents, err := Resolve(root)
	if err != nil {
		return Result{Name: name, Status: Fail, Details: []string{err.Error(), "use --offline to skip checks that need OpenCode"}}
	}
	return CompareResolved(name, root, cfgPath, cfg, agents)
}

// CompareResolved checks resolved agents against the repository: every
// configured agent is present with its model and system prompt, and every
// permission probe gets its expected decision.
func CompareResolved(name, root, cfgPath string, cfg *config.Config, agents map[string]ResolvedAgent) Result {
	var problems []string

	for _, id := range cfg.AgentIDs() {
		a := cfg.Agents[id]
		got, ok := agents[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("agent %s is missing from the resolved configuration", id))
			continue
		}
		if a.Model != "" {
			m, _ := config.ParseModel(a.Model)
			if got.Model == nil || got.Model.ProviderID != m.Provider || got.Model.ID != m.ID || got.Model.Variant != m.Variant {
				problems = append(problems, fmt.Sprintf("agent %s resolved to model %+v, want %s", id, got.Model, a.Model))
			}
		}
		if f := config.SystemFile(cfgPath, a.System); f != "" {
			want, _ := os.ReadFile(f)
			if strings.TrimSpace(got.System) == "" {
				problems = append(problems, fmt.Sprintf("agent %s resolved with an empty system prompt; is the prompts directory missing?", id))
			} else if strings.TrimSpace(got.System) != strings.TrimSpace(string(want)) {
				problems = append(problems, fmt.Sprintf("agent %s: resolved system prompt does not match %s", id, f))
			}
		}
	}

	data, err := os.ReadFile(filepath.Join(root, config.ProbesFile))
	if err != nil {
		return result(name, append(problems, err.Error()))
	}
	var probes probeFile
	if err := yaml.Unmarshal(data, &probes); err != nil {
		return result(name, append(problems, config.ProbesFile+": "+err.Error()))
	}
	for _, p := range probes.Probes {
		a, ok := agents[p.Agent]
		if !ok {
			problems = append(problems, fmt.Sprintf("probe: unknown agent %q", p.Agent))
			continue
		}
		got := perm.Decide(a.Permissions, cfg.Policies, p.Action, p.Resource)
		if got != p.Expect {
			problems = append(problems, fmt.Sprintf("%s %s %q: got %s, want %s", p.Agent, p.Action, p.Resource, got, p.Expect))
		}
	}
	if len(problems) == 0 {
		return Result{Name: name, Status: Pass, Details: []string{fmt.Sprintf("%d probes", len(probes.Probes))}}
	}
	return result(name, problems)
}
