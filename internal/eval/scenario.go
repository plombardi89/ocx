// Package eval runs scenario evaluations: it builds a throwaway git
// workspace from a fixture, stages the ocagents configuration into it, runs
// an agent headlessly with `opencode run`, and checks the outcome.
package eval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"ocagents/internal/config"
)

// Scenario is one evals/scenarios/<name>/scenario.yaml.
type Scenario struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Fixture     string   `yaml:"fixture"`
	Tags        []string `yaml:"tags"`
	// Agent is passed to `opencode run --agent`.
	Agent string `yaml:"agent"`
	// Bakeoff scenarios run once per --models entry; others ignore it.
	Bakeoff bool `yaml:"bakeoff"`
	// Branch, when set, is created from main and receives the overlay as
	// one commit with CommitMessage, or the Commits in order.
	Branch        string       `yaml:"branch"`
	CommitMessage string       `yaml:"commit_message"`
	Overlay       string       `yaml:"overlay"`
	Commits       []CommitSpec `yaml:"commits"`
	// Prompt is a text/template with .Repo, .Branch, .Base, .Head, and
	// .Commits (the branch's commit SHAs in order), plus the short function.
	Prompt  string `yaml:"prompt"`
	Timeout string `yaml:"timeout"`
	Expect  Expect `yaml:"expect"`

	Dir string `yaml:"-"`
}

// CommitSpec is one commit on the scenario branch.
type CommitSpec struct {
	Message string `yaml:"message"`
	// Overlay and then Overlays are directories, relative to the scenario
	// directory, copied over the worktree in order before committing.
	Overlay  string   `yaml:"overlay"`
	Overlays []string `yaml:"overlays"`
	// Note, if set, is a text/template file relative to the scenario
	// directory, attached to this commit as a review note to simulate an
	// earlier review round. .Head is this commit.
	Note string `yaml:"note"`
}

// Dirs returns the commit's overlay directories in application order.
func (c CommitSpec) Dirs() []string {
	var dirs []string
	if c.Overlay != "" {
		dirs = append(dirs, c.Overlay)
	}
	return append(dirs, c.Overlays...)
}

// Expect lists the assertions for a scenario. Zero values are not checked.
type Expect struct {
	// Verdict is the reviewer verdict in the OCREVIEW summary line.
	Verdict string `yaml:"verdict"`
	// Mentions are regular expressions that must all match the final report.
	Mentions []string `yaml:"mentions"`
	// ExecutedEvidence requires at least one "Evidence (executed)" finding.
	ExecutedEvidence bool `yaml:"executed_evidence"`
	// MinWriting requires at least this many WRITING findings.
	MinWriting int `yaml:"min_writing"`
	// Note requires a review note on Head whose verdict matches the report.
	Note bool `yaml:"note"`
	// Unchanged requires a clean worktree, an unmoved HEAD, and no leftover
	// scratch worktrees afterwards.
	Unchanged bool `yaml:"unchanged"`
	// SkillLoaded requires the agent to load this skill.
	SkillLoaded string `yaml:"skill_loaded"`
	// EditsInScratchOnly requires every completed edit/write/patch call to
	// target a file under the scratch root.
	EditsInScratchOnly bool `yaml:"edits_in_scratch_only"`
	E2E                *E2E `yaml:"e2e"`
}

// E2E holds assertions for end-to-end scenarios where build does the work.
type E2E struct {
	PushedBranchPrefix  string   `yaml:"pushed_branch_prefix"`
	RemoteMainUnchanged bool     `yaml:"remote_main_unchanged"`
	PushedHeadApproved  bool     `yaml:"pushed_head_approved"`
	ReviewerDispatchMin int      `yaml:"reviewer_dispatch_min"`
	DispatchPrefix      string   `yaml:"dispatch_prefix"`
	DispatchForbid      []string `yaml:"dispatch_forbid"`
	// CommentForbid are regular expressions that no added "//" line in the
	// pushed branches may match.
	CommentForbid []string `yaml:"comment_forbid"`
}

// TimeoutDuration returns the scenario timeout, defaulting to 30 minutes.
func (s Scenario) TimeoutDuration() time.Duration {
	if d, err := time.ParseDuration(s.Timeout); err == nil && d > 0 {
		return d
	}
	return 30 * time.Minute
}

// HasTag reports whether the scenario carries tag.
func (s Scenario) HasTag(tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// LoadScenarios reads every scenario under evals/scenarios.
func LoadScenarios(root string) ([]Scenario, error) {
	base := filepath.Join(root, config.ScenariosDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var out []Scenario
	for _, e := range entries {
		// Directories starting with "_" hold material shared by scenarios.
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		dir := filepath.Join(base, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "scenario.yaml"))
		if err != nil {
			return nil, fmt.Errorf("scenario %s: %w", e.Name(), err)
		}
		var s Scenario
		dec := yaml.NewDecoder(bytesReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", e.Name(), err)
		}
		s.Dir = dir
		if s.Branch != "" && len(s.Commits) == 0 {
			if s.Overlay == "" {
				s.Overlay = "overlay"
			}
			s.Commits = []CommitSpec{{Message: s.CommitMessage, Overlay: s.Overlay}}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Validate checks that a scenario is complete and self-consistent.
func (s Scenario) Validate(root string) error {
	prefix := "scenario " + filepath.Base(s.Dir)
	var errs []error
	if s.Name != filepath.Base(s.Dir) {
		errs = append(errs, fmt.Errorf("%s: name %q must equal the directory name", prefix, s.Name))
	}
	if s.Agent == "" || s.Prompt == "" || s.Fixture == "" {
		errs = append(errs, fmt.Errorf("%s: agent, prompt, and fixture are required", prefix))
	}
	if _, err := os.Stat(filepath.Join(root, config.FixturesDir, s.Fixture)); err != nil {
		errs = append(errs, fmt.Errorf("%s: fixture %q not found", prefix, s.Fixture))
	}
	if s.Branch != "" {
		if s.CommitMessage != "" && len(s.Commits) > 1 {
			errs = append(errs, fmt.Errorf("%s: use either commit_message/overlay or commits, not both", prefix))
		}
		for i, c := range s.Commits {
			if c.Message == "" {
				errs = append(errs, fmt.Errorf("%s: commit %d needs a message", prefix, i+1))
			}
			if len(c.Dirs()) == 0 {
				errs = append(errs, fmt.Errorf("%s: commit %d needs an overlay", prefix, i+1))
			}
			for _, d := range c.Dirs() {
				if _, err := os.Stat(filepath.Join(s.Dir, d)); err != nil {
					errs = append(errs, fmt.Errorf("%s: commit %d overlay %q not found", prefix, i+1, d))
				}
			}
			if c.Note != "" {
				if _, err := os.Stat(filepath.Join(s.Dir, c.Note)); err != nil {
					errs = append(errs, fmt.Errorf("%s: commit %d note %q not found", prefix, i+1, c.Note))
				}
			}
		}
	} else if len(s.Commits) > 0 {
		errs = append(errs, fmt.Errorf("%s: commits need a branch", prefix))
	}
	if s.Timeout != "" {
		if _, err := time.ParseDuration(s.Timeout); err != nil {
			errs = append(errs, fmt.Errorf("%s: timeout: %w", prefix, err))
		}
	}
	if v := s.Expect.Verdict; v != "" && v != "APPROVE" && v != "REQUEST_CHANGES" {
		errs = append(errs, fmt.Errorf("%s: verdict must be APPROVE or REQUEST_CHANGES", prefix))
	}
	patterns := append([]string{}, s.Expect.Mentions...)
	if s.Expect.E2E != nil {
		patterns = append(patterns, s.Expect.E2E.DispatchForbid...)
		patterns = append(patterns, s.Expect.E2E.CommentForbid...)
	}
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			errs = append(errs, fmt.Errorf("%s: bad regexp %q: %w", prefix, p, err))
		}
	}
	return errors.Join(errs...)
}
