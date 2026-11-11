package review

import (
	"strings"
	"testing"
)

func TestApprovalMustNameTheReviewedCommit(t *testing.T) {
	head, other := strings.Repeat("a", 40), strings.Repeat("c", 40)
	note := func(verdict, h string) string {
		return "ocreview: verdict=" + verdict + " spec=PASS quality=PASS base=" + strings.Repeat("b", 40) + " head=" + h
	}
	cases := []struct {
		name string
		o    Outcome
		want bool
	}{
		{"report and note approve this commit", Outcome{Head: head, Verdict: "APPROVE", SummaryHead: head, NoteFirstLine: note("APPROVE", head)}, true},
		{"note approves another commit", Outcome{Head: head, Verdict: "APPROVE", SummaryHead: head, NoteFirstLine: note("APPROVE", other)}, false},
		{"report approves another commit", Outcome{Head: head, Verdict: "APPROVE", SummaryHead: other, NoteFirstLine: note("APPROVE", head)}, false},
		{"note names no commit", Outcome{Head: head, Verdict: "APPROVE", SummaryHead: head, NoteFirstLine: "ocreview: verdict=APPROVE spec=PASS quality=PASS"}, false},
		{"note requests changes", Outcome{Head: head, Verdict: "APPROVE", SummaryHead: head, NoteFirstLine: note("REQUEST_CHANGES", head)}, false},
		{"report requests changes", Outcome{Head: head, Verdict: "REQUEST_CHANGES", SummaryHead: head, NoteFirstLine: note("APPROVE", head)}, false},
	}
	for _, c := range cases {
		if got := c.o.Approved(); got != c.want {
			t.Errorf("%s: Approved() = %v, want %v", c.name, got, c.want)
		}
	}
}
