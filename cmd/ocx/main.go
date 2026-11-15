// Command ocx installs the OpenCode configuration it carries: agents,
// prompts, working agreements, skills, and commands. It plays no part in
// the development loop itself.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/plombardi89/ocx"
	"github.com/plombardi89/ocx/internal/apply"
)

const usage = `ocx - install the ocx agents, skills, and commands into OpenCode

Usage:
  ocx apply [--dry-run] [--config-dir DIR]    install or upgrade the configuration this binary carries
  ocx remove [--dry-run] [--config-dir DIR]   remove what apply installed
  ocx update [--check | --apply] [--config-dir DIR]
                                              replace this binary with the latest release, and with
                                              --apply install its configuration
  ocx version                                 print the commit this binary was built from

The configuration directory defaults to $XDG_CONFIG_HOME/opencode, or
~/.config/opencode. Apply overwrites the files at the paths it writes and
keeps no backups. To upgrade, run ocx update --apply. Set OCX_RELEASE_URL
to download releases from somewhere other than GitHub.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "apply":
		err = runApply(args[1:], stdout)
	case "remove":
		err = runRemove(args[1:], stdout)
	case "update":
		err = runUpdate(args[1:], stdout, stderr)
	case "version":
		info, ok := debug.ReadBuildInfo()
		fmt.Fprintln(stdout, version(info, ok))
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	var status exitStatusError
	var usage usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &status):
		return int(status)
	case errors.As(err, &usage):
		fmt.Fprintln(stderr, "ocx:", err)
		return 2
	default:
		fmt.Fprintln(stderr, "ocx:", err)
		return 1
	}
}

func flags(name string, args []string) (dir string, dryRun bool, err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.BoolVar(&dryRun, "dry-run", false, "print the changes without making them")
	fs.StringVar(&dir, "config-dir", "", "OpenCode's global configuration directory")
	if err := fs.Parse(args); err != nil {
		return "", false, err
	}
	if fs.NArg() > 0 {
		return "", false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if dir == "" {
		dir, err = apply.ConfigDir()
	}
	return dir, dryRun, err
}

func runApply(args []string, out io.Writer) error {
	dir, dryRun, err := flags("apply", args)
	if err != nil {
		return err
	}
	files, err := ocx.Files()
	if err != nil {
		return err
	}
	changes, err := apply.Apply(dir, files, dryRun)
	if err != nil {
		return err
	}
	report(out, changes)
	info, ok := debug.ReadBuildInfo()
	if dryRun {
		fmt.Fprintf(out, "Dry run: nothing changed in %s.\n", dir)
	} else {
		fmt.Fprintf(out, "Applied %s to %s.\n", version(info, ok), dir)
	}
	return nil
}

func runRemove(args []string, out io.Writer) error {
	dir, dryRun, err := flags("remove", args)
	if err != nil {
		return err
	}
	changes, err := apply.Remove(dir, dryRun)
	if errors.Is(err, apply.ErrNotInstalled) {
		fmt.Fprintf(out, "Nothing to remove in %s.\n", dir)
		return nil
	}
	if err != nil {
		return err
	}
	report(out, changes)
	if dryRun {
		fmt.Fprintf(out, "Dry run: nothing changed in %s.\n", dir)
	} else {
		fmt.Fprintf(out, "Removed ocx from %s.\n", dir)
	}
	return nil
}

func report(out io.Writer, changes []apply.Change) {
	for _, c := range changes {
		fmt.Fprintf(out, "%-6s  %s\n", c.Op, c.Path)
	}
}

// commit is set with -ldflags "-X main.commit=..." by make ocx and make
// apply, because Go doesn't record the commit when it builds in a git
// worktree.
var commit string

// version names the commit the binary was built from: the checkout's
// commit, marked when it had uncommitted changes, or the module version for
// a go install build.
func version(info *debug.BuildInfo, ok bool) string {
	if !ok {
		if commit != "" {
			return commit
		}
		return "unknown"
	}
	revision, modified := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision != "" {
		if modified {
			revision += "-modified"
		}
		return revision
	}
	if commit != "" {
		return commit
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "unknown"
}
