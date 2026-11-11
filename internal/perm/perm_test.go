package perm

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, value string
		want           bool
	}{
		{"*", "", true},
		{"*", "anything/at/all", true},
		{"git status *", "git status", true},
		{"git status *", "git status --short", true},
		{"git status *", "git statusx", false},
		{"git push * main", "git push origin main", true},
		{"git push * main", "git push origin main-feature", false},
		{"git push * main *", "git push origin main", true},
		{"git push * main *", "git push -u origin main --tags", true},
		{"git push *--force*", "git push --force-with-lease origin feat/x", true},
		{"git push *:main", "git push origin HEAD:main", true},
		{".git/*", ".git/hooks/pre-push", true},
		{"*.env", ".env", true},
		{"*.env", "config/prod.env", true},
		{"*.env", "env.go", false},
		{"go te?t *", "go test ./...", true},
		{"git notes --ref=ocreview add *", "git notes --ref=ocreview add -f -F - abc <<'X'\nbody\nX", true},
		{"shell:*.git/hooks*", "shell:rm .git/hooks/pre-push", true},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.value); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.value, got, c.want)
		}
	}
}

func TestEvaluateLastMatchWins(t *testing.T) {
	rules := []Rule{
		{"*", "*", Allow},
		{"shell", "*", Ask},
		{"shell", "git status *", Allow},
		{"shell", "git status --porcelain", Deny},
	}
	if got := Evaluate(rules, "shell", "git status"); got != Allow {
		t.Errorf("git status: got %s", got)
	}
	if got := Evaluate(rules, "shell", "git status --porcelain"); got != Deny {
		t.Errorf("git status --porcelain: got %s", got)
	}
	if got := Evaluate(rules, "shell", "rm -rf /"); got != Ask {
		t.Errorf("rm: got %s", got)
	}
	if got := Evaluate(nil, "edit", "x"); got != Ask {
		t.Errorf("no rules: got %s, want ask", got)
	}
}

func TestDecidePolicyOverridesAllow(t *testing.T) {
	rules := []Rule{{"shell", "*", Allow}}
	policies := []Rule{
		{"tool.use", "shell:git config *", Deny},
		{"tool.use", "shell:git config --get *", Allow},
	}
	if got := Decide(rules, policies, "shell", "git config core.hooksPath /dev/null"); got != Deny {
		t.Errorf("config write: got %s", got)
	}
	if got := Decide(rules, policies, "shell", "git config --get user.email"); got != Allow {
		t.Errorf("config read: got %s", got)
	}
}
