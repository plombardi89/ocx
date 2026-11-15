package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"

	"github.com/plombardi89/ocx/internal/update"
)

// executable is replaced in tests, so they don't replace the test binary.
var executable = os.Executable

// usageError makes run print the error and exit with status 2.
type usageError struct{ error }

// exitStatus makes run exit with that status without printing anything,
// because the command that failed already printed its own message.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func runUpdate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "only report whether an update is available")
	applyAfter := fs.Bool("apply", false, "run the new ocx's apply afterwards")
	dir := fs.String("config-dir", "", "OpenCode's global configuration directory, for --apply")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	switch {
	case fs.NArg() > 0:
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	case *check && *applyAfter:
		return usageError{errors.New("--check and --apply can't be used together")}
	case *dir != "" && !*applyAfter:
		return usageError{errors.New("--config-dir only goes with --apply")}
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("find the running ocx: %w", err)
	}
	info, ok := debug.ReadBuildInfo()
	current := version(info, ok)
	res, err := update.Run(update.Options{
		Location:   update.Location(os.Getenv("OCX_RELEASE_URL")),
		Executable: exe,
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
	}, *check)
	if err != nil {
		return err
	}
	switch {
	case res.UpToDate:
		fmt.Fprintf(stdout, "Already up to date: %s\n", current)
	case *check:
		fmt.Fprintln(stdout, "An update is available.")
	default:
		fmt.Fprintf(stdout, "Updated ocx from %s to %s.\n", current, res.NewVersion)
	}
	if !*applyAfter {
		return nil
	}
	// The new binary carries the new configuration; this process still has
	// the old one, so it can't apply it itself.
	applyArgs := []string{"apply"}
	if *dir != "" {
		applyArgs = append(applyArgs, "--config-dir", *dir)
	}
	cmd := exec.Command(res.Path, applyArgs...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitStatus(exitErr.ExitCode())
		}
		return fmt.Errorf("run %s apply: %w", res.Path, err)
	}
	return nil
}
