// Package eval runs scenario evaluations: it builds a throwaway git
// workspace from a fixture, stages the ocx configuration into it, runs
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

	"github.com/plombardi89/ocx/internal/config"
)

// Scenario is one evals/scenarios/<name>/scenario.yaml.
type Scenario struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Fixture     string   `yaml:"fixture"`
	Tags        []string `yaml:"tags"`
	// BaseOverlays, relative to the scenario directory, are copied over the
	// fixture in order before the initial commit, so they become part of
	// main.
	BaseOverlays []string `yaml:"base_overlays"`
	// Agent is passed to `opencode run --agent`.
	Agent string `yaml:"agent"`
	// Bakeoff scenarios run once per --models entry; others ignore it.
	Bakeoff bool `yaml:"bakeoff"`
	// Branch, when set, is created from main and receives the overlay as
	// one commit with CommitMessage, or the Commits in order. Branch "main"
	// puts the commits on main itself, before main is pushed to origin.
	Branch        string       `yaml:"branch"`
	CommitMessage string       `yaml:"commit_message"`
	Overlay       string       `yaml:"overlay"`
	Commits       []CommitSpec `yaml:"commits"`
	// Prompt is a text/template with .Repo, .Branch, .Base, .Head, and
	// .Commits (the branch's commit SHAs in order), plus the short function.
	Prompt  string `yaml:"prompt"`
	Timeout string `yaml:"timeout"`
	Expect  Expect `yaml:"expect"`
	// Uncommitted is an overlay, relative to the scenario directory, copied
	// over the work tree after the commits and left uncommitted.
	Uncommitted string `yaml:"uncommitted"`
	// StubAgents makes each listed agent return fixed reports instead of
	// doing its work: the files, relative to the scenario directory, one per
	// launch in order, with the last one repeating. "{{HEAD}}" in a report
	// becomes the repository's HEAD at the time of the launch.
	StubAgents map[string][]string `yaml:"stub_agents"`

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
	// MinBlockers and MinMajors require at least this many BLOCKER and
	// MAJOR findings.
	MinBlockers int `yaml:"min_blockers"`
	MinMajors   int `yaml:"min_majors"`
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
	// Tests checks a test-writer report.
	Tests *TestsExpect `yaml:"tests"`
	E2E   *E2E         `yaml:"e2e"`
}

// TestsExpect holds assertions on a test-writer report. The report's own
// consistency is always checked; the other fields are checked when set.
type TestsExpect struct {
	// Plan is the repository-relative plan whose acceptance criteria the
	// report must cover, as it is at Head. A plan missing at Head has none.
	Plan   string `yaml:"plan"`
	Status string `yaml:"status"`
	// Exact counts from the OCTESTS line.
	Criteria *int `yaml:"criteria"`
	Passed   *int `yaml:"passed"`
	Failed   *int `yaml:"failed"`
	Gaps     *int `yaml:"gaps"`
	NotRun   *int `yaml:"not_run"`
	// MinFailed is the least number of FAILED criteria.
	MinFailed int `yaml:"min_failed"`
	// Results maps criterion numbers to the result each must have.
	Results map[int]string `yaml:"results"`
	// NameForbid are regular expressions that no test name or comment in
	// the returned files may match.
	NameForbid []string `yaml:"name_forbid"`

	// When the report has test files, the eval runs them, and only them, on
	// Head; they must fail exactly when the report has FAILED criteria.
	// Broken overrides the language's pattern for output that means the
	// test code itself is broken. Fixed are overlays, relative to the
	// scenario directory, with a correct implementation the tests must pass
	// on. Each mutant's overlay breaks the implementation in a way the tests
	// must catch.
	Broken  string   `yaml:"broken"`
	Fixed   []string `yaml:"fixed"`
	Mutants []Mutant `yaml:"mutants"`
}

// Mutant is a deliberately broken implementation.
type Mutant struct {
	Name    string `yaml:"name"`
	Overlay string `yaml:"overlay"`
}

// E2E holds assertions for end-to-end scenarios where build does the work.
type E2E struct {
	PushedBranchPrefix  string `yaml:"pushed_branch_prefix"`
	RemoteMainUnchanged bool   `yaml:"remote_main_unchanged"`
	PushedHeadApproved  bool   `yaml:"pushed_head_approved"`
	// DispatchAgent is the subagent whose launches are checked (default
	// reviewer): at least DispatchMin launches, each prompt starting with
	// DispatchPrefix and matching none of DispatchForbid, on its pinned model.
	DispatchAgent  string   `yaml:"dispatch_agent"`
	DispatchMin    int      `yaml:"dispatch_min"`
	DispatchPrefix string   `yaml:"dispatch_prefix"`
	DispatchForbid []string `yaml:"dispatch_forbid"`
	// CommentForbid are regular expressions that no added "//" line in the
	// pushed branches may match.
	CommentForbid []string `yaml:"comment_forbid"`
	// ChangedOnly are path globs every file changed since Base must match;
	// ChangedMin is the least number of changed files.
	ChangedOnly []string `yaml:"changed_only"`
	ChangedMin  int      `yaml:"changed_min"`
	// PushedFilesInclude are path globs that must each match at least one
	// file changed by the pushed branches.
	PushedFilesInclude []string `yaml:"pushed_files_include"`

	// Dispatches checks the launches of several subagents. The single
	// Dispatch fields above still check one agent, the reviewer by default.
	// DispatchOrder lists agents whose first launches must come in order.
	Dispatches    []Dispatch `yaml:"dispatches"`
	DispatchOrder []string   `yaml:"dispatch_order"`
	// AdoptedExactly requires a pushed commit that adds exactly the test
	// files of the last test-writer report build received, with identical
	// content, and changes nothing else.
	AdoptedExactly bool `yaml:"adopted_exactly"`
	// Buggy are overlays, relative to the scenario directory, that put the
	// original buggy implementation back. On each pushed head, the adopted
	// tests on their own and NormalTest, the project's normal test command,
	// must pass, and with the overlays laid over it they must both fail.
	Buggy      []string `yaml:"buggy"`
	NormalTest []string `yaml:"normal_test"`
	// RegistrationCommit requires a pushed commit that changes only these
	// files.
	RegistrationCommit []string `yaml:"registration_commit"`
	// NoPush requires that no branch besides main reached origin.
	NoPush bool `yaml:"no_push"`
	// ApprovedIfPushed requires every pushed head to carry an approval for
	// that exact commit, and passes when nothing was pushed. The commit and
	// final-state checks above look at the pushed branches, or, when
	// nothing was pushed, at the branch build left checked out.
	ApprovedIfPushed bool `yaml:"approved_if_pushed"`
	// TestProseForbid are regular expressions that no test name or comment
	// in the test files the pushed branches change may match.
	TestProseForbid []string `yaml:"test_prose_forbid"`
	// PushedFileMentions requires each file, as pushed, to match a pattern.
	PushedFileMentions []FileMention `yaml:"pushed_file_mentions"`
}

// Dispatch checks the launches of one subagent: each prompt starts with
// Prefix and matches none of Forbid, on the agent's pinned model, and there
// are at least Min and at most Max of them.
type Dispatch struct {
	Agent  string   `yaml:"agent"`
	Prefix string   `yaml:"prefix"`
	Forbid []string `yaml:"forbid"`
	Min    int      `yaml:"min"`
	Max    *int     `yaml:"max"`
}

// FileMention requires a pushed file to match a regular expression.
type FileMention struct {
	Path    string `yaml:"path"`
	Pattern string `yaml:"pattern"`
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
	switch s.Expect.Verdict {
	case "", "APPROVE", "REQUEST_CHANGES", "REVISE", "ROOT_CAUSE", "STUCK", "HYPOTHESES", "NEED_INFO":
	default:
		errs = append(errs, fmt.Errorf("%s: verdict must be APPROVE, REQUEST_CHANGES, REVISE, ROOT_CAUSE, STUCK, HYPOTHESES, or NEED_INFO", prefix))
	}
	if s.Uncommitted != "" {
		if _, err := os.Stat(filepath.Join(s.Dir, s.Uncommitted)); err != nil {
			errs = append(errs, fmt.Errorf("%s: uncommitted overlay %q not found", prefix, s.Uncommitted))
		}
	}
	for agent, reports := range s.StubAgents {
		if _, ok := config.ScratchRoots[agent]; !ok {
			errs = append(errs, fmt.Errorf("%s: can only stub an agent with a scratch root, not %q", prefix, agent))
		}
		if len(reports) == 0 {
			errs = append(errs, fmt.Errorf("%s: stub for %s needs at least one report", prefix, agent))
		}
		for _, r := range reports {
			if _, err := os.Stat(filepath.Join(s.Dir, r)); err != nil {
				errs = append(errs, fmt.Errorf("%s: stub report %q not found", prefix, r))
			}
		}
	}
	for _, d := range s.BaseOverlays {
		if _, err := os.Stat(filepath.Join(s.Dir, d)); err != nil {
			errs = append(errs, fmt.Errorf("%s: base overlay %q not found", prefix, d))
		}
	}
	patterns := append([]string{}, s.Expect.Mentions...)
	if t := s.Expect.Tests; t != nil {
		patterns = append(patterns, t.NameForbid...)
		if t.Broken != "" {
			patterns = append(patterns, t.Broken)
		}
		overlays := append([]string{}, t.Fixed...)
		for i, m := range t.Mutants {
			if m.Name == "" || m.Overlay == "" {
				errs = append(errs, fmt.Errorf("%s: mutant %d needs a name and an overlay", prefix, i+1))
			}
			overlays = append(overlays, m.Overlay)
		}
		for _, o := range overlays {
			if _, err := os.Stat(filepath.Join(s.Dir, o)); err != nil {
				errs = append(errs, fmt.Errorf("%s: overlay %q not found", prefix, o))
			}
		}
		switch t.Status {
		case "", "COMPLETE", "INCOMPLETE":
		default:
			errs = append(errs, fmt.Errorf("%s: tests status must be COMPLETE or INCOMPLETE", prefix))
		}
		for n, r := range t.Results {
			switch r {
			case "PASSED", "FAILED", "GAP", "NOT_RUN":
			default:
				errs = append(errs, fmt.Errorf("%s: criterion %d result must be PASSED, FAILED, GAP, or NOT_RUN", prefix, n))
			}
		}
	}
	if e := s.Expect.E2E; e != nil {
		patterns = append(patterns, e.DispatchForbid...)
		patterns = append(patterns, e.CommentForbid...)
		patterns = append(patterns, e.TestProseForbid...)
		for _, d := range e.Dispatches {
			if d.Agent == "" {
				errs = append(errs, fmt.Errorf("%s: every dispatch needs an agent", prefix))
			}
			patterns = append(patterns, d.Forbid...)
		}
		for _, m := range e.PushedFileMentions {
			patterns = append(patterns, m.Pattern)
		}
		for _, o := range e.Buggy {
			if _, err := os.Stat(filepath.Join(s.Dir, o)); err != nil {
				errs = append(errs, fmt.Errorf("%s: buggy overlay %q not found", prefix, o))
			}
		}
		if len(e.Buggy) > 0 && len(e.NormalTest) == 0 {
			errs = append(errs, fmt.Errorf("%s: buggy needs normal_test", prefix))
		}
		for _, g := range s.Expect.E2E.ChangedOnly {
			if _, err := filepath.Match(g, ""); err != nil {
				errs = append(errs, fmt.Errorf("%s: bad glob %q: %w", prefix, g, err))
			}
		}
	}
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			errs = append(errs, fmt.Errorf("%s: bad regexp %q: %w", prefix, p, err))
		}
	}
	return errors.Join(errs...)
}
