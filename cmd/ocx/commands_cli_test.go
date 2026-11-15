package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

var usageCommand = regexp.MustCompile(`(?m)^\s+ocx\s+([a-z][a-z-]*)\b`)

func commandsListedIn(usage string) []string {
	var names []string
	for _, m := range usageCommand.FindAllStringSubmatch(usage, -1) {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
}

func TestVersionPrintsOneLineFromAnyDirectory(t *testing.T) {
	s := newSandbox(t)
	r := s.run("version")
	if r.Code != 0 {
		t.Fatalf("version exited %d: %s", r.Code, r.shown())
	}
	out := strings.TrimSuffix(r.Stdout, "\n")
	if out == "" || strings.Contains(out, "\n") {
		t.Errorf("version printed %q, want a single non-empty line", r.Stdout)
	}
}

func TestUsageListsOnlyApplyRemoveAndVersion(t *testing.T) {
	s := newSandbox(t)
	r := s.run()
	got := commandsListedIn(r.combined())
	want := []string{"apply", "remove", "version"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the usage lists the commands %v, want %v\n%s", got, want, r.shown())
	}
}

func TestRemovedCommandsDoNothing(t *testing.T) {
	for _, name := range []string{"install", "uninstall", "doctor", "check", "eval"} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			r := s.run(name)
			if r.Code == 0 {
				t.Errorf("ocx %s exited 0", name)
			}
			for _, listed := range commandsListedIn(r.combined()) {
				if listed == name {
					t.Errorf("the output still lists %s as a command:\n%s", name, r.shown())
				}
			}
			if got := treeState(t, s.home); len(got) != 0 {
				t.Errorf("ocx %s created %v in the home directory", name, sortedKeys(got))
			}
		})
	}
}
