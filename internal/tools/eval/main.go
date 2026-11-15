// Command eval runs this repository's eval scenarios headlessly with
// OpenCode. It's developer tooling for this repository, run through the
// Makefile, and isn't part of the ocx binary.
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
)

func main() {
	root, err := config.FindRoot(".")
	if err == nil {
		err = runEval(root, os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for s := range strings.SplitSeq(v, ",") {
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
		WorkDir:  filepath.Join(check.ScratchDir(), "ocx-eval", stamp),
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
