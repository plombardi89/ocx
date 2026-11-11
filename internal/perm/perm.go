// Package perm evaluates OpenCode permission rules and policy statements using
// the matching rules from the OpenCode V2 documentation: whole-value
// wildcards, last matching rule wins, and policies can only tighten.
package perm

import "strings"

// Effects shared by permission rules and policy statements.
const (
	Allow = "allow"
	Ask   = "ask"
	Deny  = "deny"
)

// Rule is a permission rule or a policy statement. Both use the same shape.
type Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// Match reports whether value matches pattern. '*' matches any run of
// characters, including '/' and newlines; '?' matches exactly one character.
// A pattern ending in " *" also matches the value without trailing arguments,
// so "git status *" matches "git status".
func Match(pattern, value string) bool {
	if wildcard([]rune(pattern), []rune(value)) {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, " *"); ok {
		return wildcard([]rune(prefix), []rune(value))
	}
	return false
}

func wildcard(p, v []rune) bool {
	pi, vi := 0, 0
	star, mark := -1, 0
	for vi < len(v) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == v[vi]):
			pi++
			vi++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, vi
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			vi = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// Evaluate returns the effect of the last rule matching action and resource.
// OpenCode asks when no rule matches.
func Evaluate(rules []Rule, action, resource string) string {
	effect := Ask
	for _, r := range rules {
		if Match(r.Action, action) && Match(r.Resource, resource) {
			effect = r.Effect
		}
	}
	return effect
}

// PolicyDenies reports whether tool.use policy statements hard-deny the
// permission check for action and resource. Policy resources have the form
// "<action>:<value>"; the last matching statement wins and no match allows.
func PolicyDenies(policies []Rule, action, resource string) bool {
	value := action + ":" + resource
	effect := Allow
	for _, s := range policies {
		if s.Action == "tool.use" && Match(s.Resource, value) {
			effect = s.Effect
		}
	}
	return effect == Deny
}

// Decide combines agent permission rules with policy statements the way
// OpenCode does: a policy deny overrides any permission outcome.
func Decide(rules, policies []Rule, action, resource string) string {
	if PolicyDenies(policies, action, resource) {
		return Deny
	}
	return Evaluate(rules, action, resource)
}
