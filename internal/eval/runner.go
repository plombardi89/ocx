package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/gitx"
	"github.com/plombardi89/ocx/internal/stage"
)

// Options select and configure an eval run.
type Options struct {
	Root     string
	Names    []string // scenario name globs; empty means all
	Tags     []string // scenarios must carry every tag
	Models   []string // bakeoff model overrides
	Reps     int
	Parallel int
	Keep     bool
	OutDir   string
	WorkDir  string
	Log      func(format string, args ...any)
}

// Check is one assertion outcome.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Result is the outcome of one scenario run.
type Result struct {
	Scenario    string  `json:"scenario"`
	Model       string  `json:"model"`
	Rep         int     `json:"rep"`
	Status      string  `json:"status"` // pass, fail, error
	Error       string  `json:"error,omitempty"`
	Checks      []Check `json:"checks"`
	Verdict     string  `json:"verdict,omitempty"`
	Summary     string  `json:"summary,omitempty"`
	DurationSec float64 `json:"duration_sec"`
	Cost        float64 `json:"cost"`
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	SessionID   string  `json:"session_id,omitempty"`
	Workspace   string  `json:"workspace,omitempty"`
	OutDir      string  `json:"out_dir"`
}

type job struct {
	scenario Scenario
	override string
	rep      int
}

// Select returns the scenarios matching the name globs and tags.
func Select(all []Scenario, names, tags []string) ([]Scenario, error) {
	var out []Scenario
	for _, s := range all {
		if len(names) > 0 {
			matched := false
			for _, n := range names {
				if ok, err := filepath.Match(n, s.Name); err != nil {
					return nil, err
				} else if ok {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		keep := true
		for _, t := range tags {
			if !s.HasTag(t) {
				keep = false
			}
		}
		if keep {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no scenarios match names %v and tags %v", names, tags)
	}
	return out, nil
}

// Run executes the selected scenarios and writes results to opts.OutDir.
func Run(opts Options) ([]Result, error) {
	cfg, err := config.Load(filepath.Join(opts.Root, config.ConfigFile))
	if err != nil {
		return nil, err
	}
	all, err := LoadScenarios(opts.Root)
	if err != nil {
		return nil, err
	}
	scenarios, err := Select(all, opts.Names, opts.Tags)
	if err != nil {
		return nil, err
	}
	for _, s := range scenarios {
		if err := s.Validate(opts.Root); err != nil {
			return nil, err
		}
	}
	if opts.Reps < 1 {
		opts.Reps = 1
	}
	if opts.Parallel < 1 {
		opts.Parallel = 1
	}
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	for _, d := range []string{opts.OutDir, opts.WorkDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	var jobs []job
	for _, s := range scenarios {
		overrides := []string{""}
		if s.Bakeoff && len(opts.Models) > 0 {
			overrides = opts.Models
		}
		for _, m := range overrides {
			for rep := 1; rep <= opts.Reps; rep++ {
				jobs = append(jobs, job{scenario: s, override: m, rep: rep})
			}
		}
	}

	results := make([]Result, len(jobs))
	sem := make(chan struct{}, opts.Parallel)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			model := expectedModel(cfg, j.scenario.Agent, j.override)
			opts.Log("start  %s  %s  #%d", j.scenario.Name, model, j.rep)
			r := runOne(opts, cfg, j)
			opts.Log("%-6s %s  %s  #%d  %s  %s", r.Status, r.Scenario, r.Model, r.Rep, time.Duration(r.DurationSec*float64(time.Second)).Round(time.Second), failedChecks(r))
			results[i] = r
		}()
	}
	wg.Wait()

	if err := writeSummary(opts.OutDir, results); err != nil {
		return results, err
	}
	return results, nil
}

func expectedModel(cfg *config.Config, agent, override string) string {
	if override != "" {
		return override
	}
	if a, ok := cfg.Agents[agent]; ok && a.Model != "" {
		return a.Model
	}
	return cfg.Model
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func runOne(opts Options, cfg *config.Config, j job) Result {
	s := j.scenario
	model := expectedModel(cfg, s.Agent, j.override)
	id := fmt.Sprintf("%s__%s__%d", s.Name, unsafeChars.ReplaceAllString(model, "_"), j.rep)
	r := Result{Scenario: s.Name, Model: model, Rep: j.rep, OutDir: filepath.Join(opts.OutDir, id)}
	_ = os.MkdirAll(r.OutDir, 0o755)
	defer func() { writeJSON(filepath.Join(r.OutDir, "result.json"), r) }()

	fail := func(err error) Result {
		r.Status, r.Error = "error", err.Error()
		return r
	}

	ws, err := Prepare(opts.Root, s, filepath.Join(opts.WorkDir, id), stage.Options{NonInteractive: true})
	if err != nil {
		return fail(fmt.Errorf("prepare workspace: %w", err))
	}
	r.Workspace = ws.Dir

	prompt, err := renderPrompt(s.Prompt, ws)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.TimeoutDuration())
	defer cancel()
	tr, runErr := RunOpencode(ctx, ws.Work, s.Agent, j.override, prompt,
		filepath.Join(r.OutDir, "events.jsonl"), filepath.Join(r.OutDir, "stderr.log"))
	r.DurationSec = tr.Duration.Seconds()
	r.SessionID = tr.SessionID
	final := tr.FinalMessage()
	_ = os.WriteFile(filepath.Join(r.OutDir, "final.md"), []byte(final), 0o644)
	if runErr != nil {
		r.Status, r.Error = "error", runErr.Error()
		r.Workspace = ws.Dir // keep for debugging
		return r
	}

	exportCtx, cancelExport := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelExport()
	exp, err := ExportSession(exportCtx, ws.Work, tr.SessionID, filepath.Join(r.OutDir, "export.json"))
	if err != nil {
		r.Checks = append(r.Checks, Check{Name: "session export", Detail: err.Error()})
	} else {
		r.Cost = exp.Info.Cost
		r.TokensIn, r.TokensOut = exp.Info.Tokens.Input, exp.Info.Tokens.Output
		r.Checks = append(r.Checks, checkModel(exp, s.Agent, model))
	}

	a := asserter{ws: ws, tr: tr, final: final, cfg: cfg, outDir: r.OutDir, ctx: exportCtx}
	r.Checks = append(r.Checks, a.run(s.Expect)...)
	r.Verdict, r.Summary = a.verdict, a.summary

	r.Status = "pass"
	for _, c := range r.Checks {
		if !c.OK {
			r.Status = "fail"
		}
	}
	if r.Status == "pass" && !opts.Keep {
		_ = ws.Remove()
		r.Workspace = ""
	}
	return r
}

func renderPrompt(text string, ws *Workspace) (string, error) {
	return renderTemplate("prompt", text, ws.templateData())
}

func modelMatches(got ModelRef, want string) bool {
	w, err := config.ParseModel(want)
	if err != nil || got.ProviderID != w.Provider || got.ID != w.ID {
		return false
	}
	return w.Variant == "" || got.Variant == w.Variant
}

func checkModel(exp *Export, agent, want string) Check {
	c := Check{Name: "ran on " + want}
	n := 0
	for _, m := range exp.Messages {
		if m.Type != "assistant" || m.Agent != agent || m.Model == nil {
			continue
		}
		n++
		if !modelMatches(*m.Model, want) {
			c.Detail = fmt.Sprintf("%s message used %s", agent, m.Model)
			return c
		}
	}
	if n == 0 {
		c.Detail = fmt.Sprintf("no assistant messages from %s (saw %v)", agent, exp.AssistantModels())
		return c
	}
	c.OK = true
	return c
}

func failedChecks(r Result) string {
	if r.Error != "" {
		return r.Error
	}
	var names []string
	for _, c := range r.Checks {
		if !c.OK {
			names = append(names, c.Name)
		}
	}
	return strings.Join(names, "; ")
}

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, append(data, '\n'), 0o644)
	}
}

func writeSummary(dir string, results []Result) error {
	writeJSON(filepath.Join(dir, "results.json"), results)
	var b strings.Builder
	b.WriteString("# Eval results\n\n")
	b.WriteString("| Scenario | Model | Rep | Status | Verdict | Findings B/M/W/m/n | Duration | Failed checks |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, r := range results {
		findings := ""
		if s, ok := parseSummary(r.Summary); ok {
			findings = s.Counts()
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | %s | %s |\n", r.Scenario, r.Model, r.Rep, r.Status, r.Verdict, findings,
			time.Duration(r.DurationSec*float64(time.Second)).Round(time.Second), strings.ReplaceAll(failedChecks(r), "|", "\\|"))
	}

	type agg struct {
		runs, passed int
		seconds      float64
	}
	bump := func(a *agg, r Result) {
		a.runs++
		a.seconds += r.DurationSec
		if r.Status == "pass" {
			a.passed++
		}
	}
	byModel := map[string]*agg{}
	byPair := map[[2]string]*agg{}
	var scenarios []string
	for _, r := range results {
		if byModel[r.Model] == nil {
			byModel[r.Model] = &agg{}
		}
		pair := [2]string{r.Model, r.Scenario}
		if byPair[pair] == nil {
			byPair[pair] = &agg{}
		}
		bump(byModel[r.Model], r)
		bump(byPair[pair], r)
		if !slices.Contains(scenarios, r.Scenario) {
			scenarios = append(scenarios, r.Scenario)
		}
	}
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)
	sort.Strings(scenarios)

	b.WriteString("\n## By model\n\n| Model | Passed | Mean duration |")
	for _, s := range scenarios {
		b.WriteString(" " + s + " |")
	}
	b.WriteString("\n| --- | --- | --- |" + strings.Repeat(" --- |", len(scenarios)) + "\n")
	for _, m := range models {
		a := byModel[m]
		mean := time.Duration(a.seconds / float64(a.runs) * float64(time.Second)).Round(time.Second)
		fmt.Fprintf(&b, "| %s | %d/%d | %s |", m, a.passed, a.runs, mean)
		for _, s := range scenarios {
			if p := byPair[[2]string{m, s}]; p != nil {
				fmt.Fprintf(&b, " %d/%d |", p.passed, p.runs)
			} else {
				b.WriteString(" - |")
			}
		}
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(b.String()), 0o644)
}

// gitOut runs an isolated git command, returning "" on error.
func gitOut(dir string, args ...string) string {
	out, err := gitx.Run(dir, args...)
	if err != nil {
		return ""
	}
	return out
}
