// Command ocx is the toolbox for this repository: static checks, evals, sandboxes,
// staging, and review-gate installation.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/plombardi89/ocx/internal/check"
	"github.com/plombardi89/ocx/internal/config"
	"github.com/plombardi89/ocx/internal/eval"
	"github.com/plombardi89/ocx/internal/hook"
	"github.com/plombardi89/ocx/internal/stage"
)

const usage = `ocx - checks, evals, and tools for the ocx agents and skills

Usage:
  ocx check [--offline]                 validate configuration, skills, hook, scenarios, permissions
  ocx eval [flags]                      run eval scenarios headlessly with opencode
  ocx sandbox [--scenario NAME]         create a throwaway project with this config staged; prints its path
  ocx stage DIR                         stage this config into DIR/.opencode (DIR must not have one)
  ocx hook install REPO                 install the pre-push review gate into REPO

Run "ocx <command> -h" for flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	root, err := config.FindRoot(".")
	if err != nil {
		fatal(err)
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "check":
		err = runCheck(root, args)
	case "eval":
		err = runEval(root, args)
	case "sandbox":
		err = runSandbox(root, args)
	case "stage":
		err = runStage(root, args)
	case "hook":
		err = runHook(root, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ocx:", err)
	os.Exit(1)
}

func runCheck(root string, args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	offline := fs.Bool("offline", false, "skip checks that run opencode")
	_ = fs.Parse(args)

	failed := 0
	for _, r := range check.Run(root, check.Options{Offline: *offline}) {
		fmt.Printf("%-4s  %s\n", r.Status, r.Name)
		for _, d := range r.Details {
			for _, line := range strings.Split(d, "\n") {
				fmt.Printf("        %s\n", line)
			}
		}
		if r.Status == check.Fail {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}

func runEval(root string, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	var names, tags, models listFlag
	fs.Var(&names, "scenario", "scenario name or glob (repeatable, comma-separated)")
	fs.Var(&tags, "tag", "only scenarios with this tag (repeatable)")
	fs.Var(&models, "models", "bake-off: run bakeoff scenarios once per model (comma-separated provider/model#variant)")
	reps := fs.Int("reps", 1, "repetitions per scenario and model")
	parallel := fs.Int("parallel", 3, "concurrent runs")
	keep := fs.Bool("keep", false, "keep workspaces of passing runs (failing runs are always kept)")
	out := fs.String("out", "", "results directory (default evals/results/<timestamp>)")
	list := fs.Bool("list", false, "list scenarios and exit")
	_ = fs.Parse(args)

	if *list {
		all, err := eval.LoadScenarios(root)
		if err != nil {
			return err
		}
		for _, s := range all {
			fmt.Printf("%-32s %-8s %-28s %s\n", s.Name, s.Agent, strings.Join(s.Tags, ","), s.Description)
		}
		return nil
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	if *out == "" {
		*out = filepath.Join(root, "evals", "results", stamp)
	}
	opts := eval.Options{
		Root:     root,
		Names:    names,
		Tags:     tags,
		Models:   models,
		Reps:     *reps,
		Parallel: *parallel,
		Keep:     *keep,
		OutDir:   *out,
		WorkDir:  filepath.Join(scratchDir(), "ocx-eval", stamp),
		Log: func(format string, a ...any) {
			fmt.Printf("%s  "+format+"\n", append([]any{time.Now().Format("15:04:05")}, a...)...)
		},
	}
	results, err := eval.Run(opts)
	if err != nil {
		return err
	}
	failed := 0
	for _, r := range results {
		if r.Status != "pass" {
			failed++
			fmt.Printf("\n%s %s #%d: %s\n", r.Scenario, r.Model, r.Rep, r.Status)
			if r.Error != "" {
				fmt.Printf("  error: %s\n", r.Error)
			}
			for _, c := range r.Checks {
				if !c.OK {
					fmt.Printf("  FAIL %s: %s\n", c.Name, c.Detail)
				}
			}
			fmt.Printf("  output: %s\n", r.OutDir)
			if r.Workspace != "" {
				fmt.Printf("  workspace: %s\n", r.Workspace)
			}
		}
	}
	fmt.Printf("\n%d/%d runs passed. Summary: %s\n", len(results)-failed, len(results), filepath.Join(*out, "summary.md"))
	if failed > 0 {
		return errors.New("evals failed")
	}
	return nil
}

func scratchDir() string {
	dir := filepath.Join(os.TempDir(), "opencode")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func runSandbox(root string, args []string) error {
	fs := flag.NewFlagSet("sandbox", flag.ExitOnError)
	name := fs.String("scenario", "", "check out this scenario's branch (default: plain fixture on main)")
	fixture := fs.String("fixture", "go-service", "fixture to copy when no scenario is given")
	_ = fs.Parse(args)

	s := eval.Scenario{Fixture: *fixture}
	if *name != "" {
		all, err := eval.LoadScenarios(root)
		if err != nil {
			return err
		}
		found := false
		for _, sc := range all {
			if sc.Name == *name {
				s, found = sc, true
			}
		}
		if !found {
			return fmt.Errorf("unknown scenario %q", *name)
		}
	}
	dir, err := os.MkdirTemp(scratchDir(), "ocx-sandbox-")
	if err != nil {
		return err
	}
	ws, err := eval.Prepare(root, s, dir, stage.Options{})
	if err != nil {
		return err
	}
	fmt.Println(ws.Work)
	return nil
}

func runStage(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ocx stage DIR")
	}
	if err := stage.Stage(root, args[0], stage.Options{}); err != nil {
		return err
	}
	fmt.Printf("staged into %s/.opencode (remove .opencode/ and opencode.json to undo)\n", args[0])
	return nil
}

func runHook(root string, args []string) error {
	if len(args) != 2 || args[0] != "install" {
		return errors.New("usage: ocx hook install REPO")
	}
	path, err := hook.Install(filepath.Join(root, config.HookFile), args[1])
	if err != nil {
		return err
	}
	fmt.Println("installed", path)
	return nil
}
