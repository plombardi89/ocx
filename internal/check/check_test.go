package check

import (
	"slices"
	"testing"
)

func TestLetterListItem(t *testing.T) {
	cases := map[string]bool{
		"   a. sub step":        true,
		"b) option":             true,
		"iv. fourth":            true,
		"I. Introduction":       true,
		"1. first":              false,
		"   2. nested":          false,
		"- bullet":              false,
		"a.k.a. something":      false,
		"e.g. an example":       false,
		"Use a. as a separator": false,
	}
	for line, want := range cases {
		if got := letterListItem.MatchString(line); got != want {
			t.Errorf("letterListItem(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestCoupledGoComments(t *testing.T) {
	src := "package x\n" +
		"\n" +
		"// Implements task 2 of the pagination work.\n" +
		"func a() {}\n" +
		"\n" +
		"// Cap the page size so one request can't return the whole catalog.\n" +
		"func b() {\n" +
		"\ts := \"// see docs/plans/0001-x.md\" // Fix for review finding 2.\n" +
		"\t_ = s\n" +
		"\t/* planned capacity is 100, as requested */\n" +
		"}\n"
	lines, err := coupledGoComments("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{3, 8, 10}; !slices.Equal(lines, want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
}
