package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/plombardi89/ocx/internal/config"
)

var (
	summaryLine      = regexp.MustCompile(`OC(REVIEW|PLAN) verdict=(\S+)(?: spec=(\S+) quality=(\S+))? blockers=(\d+) majors=(\d+)(?: writing=(\d+))? minors=(\d+) nits=(\d+)(?: head=([0-9a-f]+))?`)
	statusLine       = regexp.MustCompile(`OC(DEBUG|CONSULT) status=(\S+)`)
	noteLine         = regexp.MustCompile(`^ocreview: verdict=(\S+) .*head=([0-9a-f]+)`)
	executedEvidence = regexp.MustCompile(`(?i)evidence \(executed\)`)
	patchFileHeader  = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$|^\*\*\* Move to: (.+)$`)
)

// reviewSummary is the OCREVIEW line of a code review or the OCPLAN line of
// a plan critique. Plan critiques have no spec, quality, or head fields.
type reviewSummary struct {
	Line, Kind, Verdict, Spec, Quality, Head string
	Blockers, Majors, Writing, Minors, Nits  int
}

// Summary is the verdict, the head it names (OCREVIEW lines only), and the
// whole line from the last OCREVIEW, OCPLAN, OCDEBUG, or OCCONSULT line in a
// report.
type Summary struct {
	Verdict, Head, Line string
}

// ParseSummary returns the summary of text, if it has one.
func ParseSummary(text string) (Summary, bool) {
	s, ok := parseSummary(text)
	return Summary{Verdict: s.Verdict, Head: s.Head, Line: s.Line}, ok
}

// parseSummary returns the last OCREVIEW or OCPLAN summary line in text, or
// failing that the last OCDEBUG or OCCONSULT status line.
func parseSummary(text string) (reviewSummary, bool) {
	all := summaryLine.FindAllStringSubmatch(text, -1)
	if len(all) == 0 {
		status := statusLine.FindAllStringSubmatch(text, -1)
		if len(status) == 0 {
			return reviewSummary{}, false
		}
		m := status[len(status)-1]
		return reviewSummary{Line: m[0], Kind: "OC" + m[1], Verdict: m[2]}, true
	}
	m := all[len(all)-1]
	n := func(s string) int {
		v, _ := strconv.Atoi(s)
		return v
	}
	return reviewSummary{
		Line: m[0], Kind: "OC" + m[1], Verdict: m[2], Spec: m[3], Quality: m[4],
		Blockers: n(m[5]), Majors: n(m[6]), Writing: n(m[7]), Minors: n(m[8]), Nits: n(m[9]),
		Head: m[10],
	}, true
}

// Counts formats the finding counts as blockers/majors/writing/minors/nits.
func (s reviewSummary) Counts() string {
	return fmt.Sprintf("%d/%d/%d/%d/%d", s.Blockers, s.Majors, s.Writing, s.Minors, s.Nits)
}

// inScratch reports whether path is inside any agent's scratch root.
func inScratch(path string) bool {
	for _, root := range config.ScratchRoots {
		if strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// editTargets returns the file paths an edit, write, or patch call targets.
func editTargets(input map[string]any) []string {
	var paths []string
	for _, key := range []string{"path", "filePath", "file_path"} {
		if p, ok := input[key].(string); ok && p != "" {
			paths = append(paths, p)
		}
	}
	if text, ok := input["patchText"].(string); ok {
		for _, m := range patchFileHeader.FindAllStringSubmatch(text, -1) {
			for _, p := range m[1:] {
				if p = strings.TrimSpace(p); p != "" {
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}

type asserter struct {
	ws     *Workspace
	tr     *Transcript
	final  string
	cfg    *config.Config
	outDir string
	ctx    context.Context

	verdict string
	summary string
}

func (a *asserter) run(e Expect) []Check {
	var checks []Check
	add := func(name string, ok bool, detail string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}

	summary, hasSummary := parseSummary(a.final)
	if hasSummary {
		a.summary, a.verdict = summary.Line, summary.Verdict
	}

	if e.Verdict != "" {
		switch {
		case !hasSummary:
			add("verdict "+e.Verdict, false, "no OCREVIEW or OCPLAN summary line in the final message")
		default:
			add("verdict "+e.Verdict, a.verdict == e.Verdict, "got %s", a.verdict)
			if summary.Kind == "OCREVIEW" {
				add("summary names head", summary.Head == a.ws.Head, "summary head=%s, want %s", summary.Head, a.ws.Head)
			}
		}
	}

	if e.MinBlockers > 0 {
		add(fmt.Sprintf("at least %d BLOCKER findings", e.MinBlockers), summary.Blockers >= e.MinBlockers, "summary reports blockers=%d", summary.Blockers)
	}
	if e.MinMajors > 0 {
		add(fmt.Sprintf("at least %d MAJOR findings", e.MinMajors), summary.Majors >= e.MinMajors, "summary reports majors=%d", summary.Majors)
	}

	if e.MinWriting > 0 {
		add(fmt.Sprintf("at least %d WRITING findings", e.MinWriting), summary.Writing >= e.MinWriting, "summary reports writing=%d", summary.Writing)
	}

	for _, p := range e.Mentions {
		re := regexp.MustCompile(p)
		add("report mentions "+p, re.MatchString(a.final), "not found in final report")
	}

	if e.ExecutedEvidence {
		add("executed evidence", executedEvidence.MatchString(a.final), "no finding has \"Evidence (executed)\"")
	}

	if e.Note {
		first := firstLine(gitOut(a.ws.Work, "notes", "--ref=ocreview", "show", a.ws.Head))
		m := noteLine.FindStringSubmatch(first)
		switch {
		case m == nil:
			add("review note on head", false, "no ocreview note on %s (first line %q)", a.ws.Head, first)
		case m[2] != a.ws.Head:
			add("review note on head", false, "note names head=%s", m[2])
		case a.verdict != "" && m[1] != a.verdict:
			add("review note on head", false, "note verdict %s differs from report verdict %s", m[1], a.verdict)
		default:
			add("review note on head", true, "")
		}
	}

	if e.Unchanged {
		status := gitOut(a.ws.Work, "status", "--porcelain")
		head := gitOut(a.ws.Work, "rev-parse", "HEAD")
		branch := gitOut(a.ws.Work, "branch", "--show-current")
		ok := status == "" && head == a.ws.Head && branch == a.ws.Branch
		add("repository unchanged", ok, "status=%q head=%s branch=%s", status, head, branch)

		var extra []string
		for _, line := range strings.Split(gitOut(a.ws.Work, "worktree", "list", "--porcelain"), "\n") {
			if wt, ok := strings.CutPrefix(line, "worktree "); ok && wt != a.ws.Work {
				extra = append(extra, wt)
			}
		}
		add("no leftover worktrees", len(extra) == 0, "still registered: %v", extra)
	}

	if e.SkillLoaded != "" {
		loaded := false
		for _, p := range a.tr.ToolCalls("skill") {
			if p.State != nil && (p.State.Input["id"] == e.SkillLoaded || p.State.Input["name"] == e.SkillLoaded) {
				loaded = true
			}
		}
		add("loaded skill "+e.SkillLoaded, loaded, "skill tool was not called with %q", e.SkillLoaded)
	}

	if e.EditsInScratchOnly {
		var outside []string
		for _, p := range a.tr.ToolCalls("edit", "write", "patch") {
			if p.State == nil || p.State.Status != "completed" {
				continue
			}
			for _, path := range editTargets(p.State.Input) {
				if !inScratch(path) {
					outside = append(outside, path)
				}
			}
		}
		add("edits only in scratch", len(outside) == 0, "edited outside the scratch roots: %v", outside)
	}

	if e.E2E != nil {
		checks = append(checks, a.e2e(*e.E2E)...)
	}
	return checks
}

func (a *asserter) e2e(e E2E) []Check {
	var checks []Check
	add := func(name string, ok bool, detail string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}

	var pushed []string
	for _, b := range strings.Split(gitOut(a.ws.Origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"), "\n") {
		if b != "" && b != "main" && b != "master" {
			pushed = append(pushed, b)
		}
	}

	if e.PushedBranchPrefix != "" {
		ok := false
		for _, b := range pushed {
			if strings.HasPrefix(b, e.PushedBranchPrefix) {
				ok = true
			}
		}
		add("pushed a "+e.PushedBranchPrefix+"* branch", ok, "remote branches: %v", pushed)
	}

	if e.RemoteMainUnchanged {
		main := gitOut(a.ws.Origin, "rev-parse", "refs/heads/main")
		add("remote main unchanged", main == a.ws.Base, "remote main=%s, want %s", main, a.ws.Base)
	}

	if e.PushedHeadApproved {
		ok, detail := len(pushed) > 0, "no branch was pushed"
		for _, b := range pushed {
			sha := gitOut(a.ws.Origin, "rev-parse", "refs/heads/"+b)
			m := noteLine.FindStringSubmatch(firstLine(gitOut(a.ws.Work, "notes", "--ref=ocreview", "show", sha)))
			if m == nil || m[1] != "APPROVE" || m[2] != sha {
				ok, detail = false, fmt.Sprintf("%s at %s has no matching approval", b, sha)
			} else if detail == "no branch was pushed" {
				detail = ""
			}
		}
		add("pushed heads approved", ok, "%s", detail)
	}

	for _, glob := range e.PushedFilesInclude {
		var files []string
		for _, b := range pushed {
			files = append(files, strings.Split(gitOut(a.ws.Origin, "diff", "--name-only", a.ws.Base, "refs/heads/"+b), "\n")...)
		}
		found := false
		for _, f := range files {
			if ok, _ := filepath.Match(glob, f); ok {
				found = true
			}
		}
		add("pushed changes include "+glob, found, "pushed files: %v", files)
	}

	if len(e.CommentForbid) > 0 {
		var hits []string
		for _, b := range pushed {
			diff := gitOut(a.ws.Origin, "diff", a.ws.Base, "refs/heads/"+b)
			for _, line := range strings.Split(diff, "\n") {
				if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") || !strings.Contains(line, "//") {
					continue
				}
				for _, f := range e.CommentForbid {
					if regexp.MustCompile(f).MatchString(line) {
						hits = append(hits, strings.TrimSpace(line[1:]))
					}
				}
			}
		}
		add("added comments stand on their own", len(hits) == 0, "%q", hits)
	}

	if len(e.ChangedOnly) > 0 || e.ChangedMin > 0 {
		changed := a.changedFiles()
		var outside []string
		for _, f := range changed {
			allowed := len(e.ChangedOnly) == 0
			for _, g := range e.ChangedOnly {
				if ok, _ := filepath.Match(g, f); ok {
					allowed = true
				}
			}
			if !allowed {
				outside = append(outside, f)
			}
		}
		if len(e.ChangedOnly) > 0 {
			add(fmt.Sprintf("changed only %v", e.ChangedOnly), len(outside) == 0, "also changed %v", outside)
		}
		if e.ChangedMin > 0 {
			add(fmt.Sprintf("changed at least %d file(s)", e.ChangedMin), len(changed) >= e.ChangedMin, "changed %v", changed)
		}
	}

	agent := e.DispatchAgent
	if agent == "" {
		agent = "reviewer"
	}
	var dispatches []Part
	for _, p := range a.tr.ToolCalls("subagent") {
		if p.State != nil && p.State.Input["agent"] == agent {
			dispatches = append(dispatches, p)
		}
	}
	if e.DispatchMin > 0 {
		add(fmt.Sprintf("%s launched at least %d time(s)", agent, e.DispatchMin), len(dispatches) >= e.DispatchMin, "launched %d time(s)", len(dispatches))
	}
	want := ""
	if r, ok := a.cfg.Agents[agent]; ok {
		want = r.Model
	}
	for i, p := range dispatches {
		prompt, _ := p.State.Input["prompt"].(string)
		name := fmt.Sprintf("%s dispatch %d", agent, i+1)
		if e.DispatchPrefix != "" {
			add(name+" uses the request template", strings.HasPrefix(strings.TrimSpace(prompt), e.DispatchPrefix), "prompt starts %q", truncate(prompt, 60))
		}
		for _, f := range e.DispatchForbid {
			if loc := regexp.MustCompile(f).FindString(prompt); loc != "" {
				add(name+" has no framing", false, "matched %q: %q", f, loc)
			}
		}
		if m, ok := p.State.Input["model"].(string); ok && m != "" {
			add(name+" does not override the model", false, "model=%s", m)
		}
		child := ChildSessionID(p)
		if child == "" || want == "" {
			add(name+" child session found", false, "child=%q", child)
			continue
		}
		exp, err := ExportSession(a.ctx, a.ws.Work, child, filepath.Join(a.outDir, fmt.Sprintf("%s-%d.json", agent, i+1)))
		if err != nil {
			add(name+" export", false, "%v", err)
			continue
		}
		c := checkModel(exp, agent, want)
		c.Name = name + " " + c.Name
		checks = append(checks, c)
	}
	return checks
}

// changedFiles lists files that differ from the workspace's base commit,
// whether committed, modified, or new. Staged configuration is excluded by
// .git/info/exclude.
func (a *asserter) changedFiles() []string {
	seen := map[string]bool{}
	var out []string
	add := func(list string) {
		for _, f := range strings.Split(list, "\n") {
			if f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	add(gitOut(a.ws.Work, "diff", "--name-only", a.ws.Base))
	add(gitOut(a.ws.Work, "ls-files", "--others", "--exclude-standard"))
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
