package ocx

import (
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflowStep struct {
	Run             string `yaml:"run"`
	ContinueOnError any    `yaml:"continue-on-error"`
}

type workflowJob struct {
	Needs           any               `yaml:"needs"`
	Permissions     map[string]string `yaml:"permissions"`
	ContinueOnError any               `yaml:"continue-on-error"`
	Steps           []workflowStep    `yaml:"steps"`
}

type releaseWorkflow struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress any    `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs yaml.Node `yaml:"jobs"`
}

type namedJob struct {
	name string
	workflowJob
}

func loadReleaseWorkflow(t *testing.T) (releaseWorkflow, []namedJob) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf releaseWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("release.yml isn't valid YAML: %v", err)
	}
	var jobs []namedJob
	for i := 0; i+1 < len(wf.Jobs.Content); i += 2 {
		var job workflowJob
		if err := wf.Jobs.Content[i+1].Decode(&job); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, namedJob{name: wf.Jobs.Content[i].Value, workflowJob: job})
	}
	if len(jobs) == 0 {
		t.Fatal("release.yml defines no jobs")
	}
	return wf, jobs
}

func (j workflowJob) needed() []string {
	switch v := j.Needs.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, n := range v {
			out = append(out, fmt.Sprint(n))
		}
		return out
	}
	return nil
}

func (j workflowJob) script() string {
	var parts []string
	for _, s := range j.Steps {
		if s.Run != "" {
			parts = append(parts, s.Run)
		}
	}
	return strings.Join(parts, "\n")
}

func isTrue(v any) bool {
	return v == true || fmt.Sprint(v) == "true"
}

// checkJobAndTheRest returns the job that runs make check-offline and the
// jobs that come after it.
func checkJobAndTheRest(t *testing.T, jobs []namedJob) (namedJob, []namedJob) {
	t.Helper()
	var check namedJob
	var rest []namedJob
	found := false
	for _, j := range jobs {
		if !found && strings.Contains(j.script(), "make check-offline") {
			check, found = j, true
			continue
		}
		rest = append(rest, j)
	}
	if !found {
		t.Fatal("no job runs make check-offline")
	}
	return check, rest
}

func TestReleaseWorkflowRunsOnEveryPushToMainAndNothingElse(t *testing.T) {
	wf, _ := loadReleaseWorkflow(t)
	push, ok := wf.On["push"].(map[string]any)
	if !ok {
		t.Fatalf("the workflow isn't triggered by push: %v", wf.On)
	}
	branches, _ := push["branches"].([]any)
	if len(branches) != 1 || fmt.Sprint(branches[0]) != "main" {
		t.Errorf("push branches = %v, want only main", branches)
	}
	for _, filter := range []string{"paths", "paths-ignore", "branches-ignore", "tags", "tags-ignore"} {
		if _, ok := push[filter]; ok {
			t.Errorf("the push trigger has a %s filter, so not every push to main runs it", filter)
		}
	}
	for _, trigger := range []string{"pull_request", "pull_request_target"} {
		if _, ok := wf.On[trigger]; ok {
			t.Errorf("the workflow also runs on %s, which would publish code that isn't on main", trigger)
		}
	}
}

func TestReleaseWorkflowRunsOneAtATimeAndNeverCancelsARunInProgress(t *testing.T) {
	wf, _ := loadReleaseWorkflow(t)
	group := wf.Concurrency.Group
	if group == "" {
		t.Fatal("the workflow has no concurrency group")
	}
	for _, perRun := range []string{"github.sha", "github.run_id", "github.run_number", "github.run_attempt"} {
		if strings.Contains(group, perRun) {
			t.Errorf("the concurrency group %q differs for every run (%s), so runs don't queue", group, perRun)
		}
	}
	if isTrue(wf.Concurrency.CancelInProgress) {
		t.Error("cancel-in-progress is true: a newer push would cancel a run that has started")
	}
}

func TestReleaseWorkflowStopsWithoutBuildingWhenTheChecksFail(t *testing.T) {
	_, jobs := loadReleaseWorkflow(t)
	check, rest := checkJobAndTheRest(t, jobs)
	if isTrue(check.ContinueOnError) {
		t.Error("the check job continues on error")
	}
	for _, s := range check.Steps {
		if isTrue(s.ContinueOnError) {
			t.Error("a step of the check job continues on error")
		}
		if strings.Contains(s.Run, "make check-offline") && regexp.MustCompile(`\|\||;\s*true|exit 0`).MatchString(s.Run) {
			t.Errorf("the check step hides a failure: %q", s.Run)
		}
	}
	if len(rest) == 0 {
		t.Fatal("no job builds or publishes")
	}
	// Every other job has to wait for the check job, directly or through
	// another job that does.
	waits := map[string]bool{check.name: true}
	for changed := true; changed; {
		changed = false
		for _, j := range rest {
			if waits[j.name] {
				continue
			}
			for _, n := range j.needed() {
				if waits[n] {
					waits[j.name], changed = true, true
				}
			}
		}
	}
	for _, j := range rest {
		if !waits[j.name] {
			t.Errorf("job %s doesn't wait for the checks", j.name)
		}
		if isTrue(j.ContinueOnError) {
			t.Errorf("job %s continues on error", j.name)
		}
	}
}

func TestReleaseWorkflowBuildsBothLinuxBinariesWithoutCgo(t *testing.T) {
	_, jobs := loadReleaseWorkflow(t)
	_, rest := checkJobAndTheRest(t, jobs)
	var script strings.Builder
	for _, j := range rest {
		script.WriteString(j.script() + "\n")
	}
	text := script.String()
	for _, want := range []string{"go build", "CGO_ENABLED=0", "GOOS=linux", "GOARCH", "amd64", "arm64", "ocx-linux-amd64", "ocx-linux-arm64"} {
		if !strings.Contains(text, want) {
			t.Errorf("the build steps never mention %q", want)
		}
	}
}

func TestReleaseWorkflowOrdersBuildChecksumsTipCheckAndPublish(t *testing.T) {
	_, jobs := loadReleaseWorkflow(t)
	_, rest := checkJobAndTheRest(t, jobs)
	var script strings.Builder
	for _, j := range rest {
		script.WriteString(j.script() + "\n")
	}
	text := script.String()
	if !strings.Contains(text, "SHA256SUMS") || !strings.Contains(text, "sha256sum") {
		t.Fatal("the steps never write checksums to SHA256SUMS with sha256sum")
	}
	steps := []struct{ name, marker string }{
		{"build", "go build"},
		{"checksums", "sha256sum"},
		{"tip check", "ls-remote"},
		{"publish", "gh release"},
	}
	last := -1
	for _, s := range steps {
		at := strings.Index(text, s.marker)
		if at < 0 {
			t.Errorf("the steps never run %q (%s)", s.marker, s.name)
			continue
		}
		if at < last {
			t.Errorf("the %s comes too early: %q appears before the previous stage", s.name, s.marker)
		}
		last = at
	}
	if !regexp.MustCompile(`ls-remote[^\n]*refs/heads/main`).MatchString(text) {
		t.Error("the tip check doesn't ask the remote for refs/heads/main")
	}
	if !regexp.MustCompile(`GITHUB_SHA|github\.sha`).MatchString(text) {
		t.Error("the steps never compare with the commit being built")
	}
}

func TestReleaseWorkflowPublishesTheThreeFilesToTheLatestRelease(t *testing.T) {
	wf, jobs := loadReleaseWorkflow(t)
	_, rest := checkJobAndTheRest(t, jobs)
	var script strings.Builder
	var publisher namedJob
	for _, j := range rest {
		script.WriteString(j.script() + "\n")
		if strings.Contains(j.script(), "gh release") {
			publisher = j
		}
	}
	text := script.String()
	if publisher.name == "" {
		t.Fatal("no job runs gh release")
	}
	if wf.Permissions["contents"] != "write" && publisher.Permissions["contents"] != "write" {
		t.Error("the publishing job doesn't have contents: write")
	}

	create := regexp.MustCompile(`gh release create latest\b`)
	view := regexp.MustCompile(`gh release view latest\b`)
	if !create.MatchString(text) {
		t.Error("the release is never created with the tag latest")
	}
	if !view.MatchString(text) || view.FindStringIndex(text)[0] > create.FindStringIndex(text)[0] {
		t.Error("the release is created without first checking whether it exists")
	}
	upload := regexp.MustCompile(`gh release upload latest[^\n]*`).FindString(text)
	if upload == "" {
		t.Fatal("the files are never uploaded to the latest release")
	}
	for _, name := range []string{"ocx-linux-amd64", "ocx-linux-arm64", "SHA256SUMS"} {
		if !strings.Contains(upload, name) {
			t.Errorf("the upload %q doesn't include %s", upload, name)
		}
	}
	if !strings.Contains(upload, "--clobber") {
		t.Errorf("the upload %q doesn't replace files that are already there", upload)
	}
	notes := regexp.MustCompile(`gh release edit latest[^\n]*(--notes|-n)[^\n]*`).FindString(text)
	if notes == "" || !regexp.MustCompile(`GITHUB_SHA|github\.sha`).MatchString(notes) {
		t.Errorf("the release notes are never set to name the commit: %q", notes)
	}
	if strings.Index(text, "gh release upload") > strings.Index(text, "gh release edit") {
		t.Error("the notes are set before the files are uploaded")
	}
}

func TestBothReleaseBinariesBuildWithoutCgoForTheirArchitecture(t *testing.T) {
	for arch, machine := range map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64} {
		t.Run(arch, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "ocx-linux-"+arch)
			cmd := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", bin, "./cmd/ocx")
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build for linux/%s: %v\n%s", arch, err, out)
			}
			f, err := elf.Open(bin)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			if f.Machine != machine {
				t.Errorf("machine = %v, want %v", f.Machine, machine)
			}
			if f.Section(".interp") != nil {
				t.Error("the binary asks for a dynamic loader, so it isn't statically linked")
			}
		})
	}
}
