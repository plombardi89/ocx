package config

import "testing"

func TestFrontmatter(t *testing.T) {
	fm, body, err := Frontmatter([]byte("---\nname: x\ndescription: Use when testing\n---\n\n# Body\n"))
	if err != nil || fm["name"] != "x" || fm["description"] != "Use when testing" || body != "\n# Body\n" {
		t.Fatalf("got %v %q %v", fm, body, err)
	}
	fm, body, err = Frontmatter([]byte("no frontmatter\n"))
	if err != nil || fm != nil || body != "no frontmatter\n" {
		t.Fatalf("plain: got %v %q %v", fm, body, err)
	}
	if _, _, err := Frontmatter([]byte("---\nname: x\n")); err == nil {
		t.Fatal("unterminated frontmatter should fail")
	}
}

func TestParseModel(t *testing.T) {
	m, err := ParseModel("github-copilot/gpt-6.1-sol#xhigh")
	if err != nil || m.Provider != "github-copilot" || m.ID != "gpt-6.1-sol" || m.Variant != "xhigh" {
		t.Fatalf("got %+v %v", m, err)
	}
	if m.String() != "github-copilot/gpt-6.1-sol#xhigh" {
		t.Fatalf("String = %s", m)
	}
	for _, bad := range []string{"", "gpt-6.1-sol", "a/b#", "a/b#XHIGH", "/b"} {
		if _, err := ParseModel(bad); err == nil {
			t.Errorf("ParseModel(%q) should fail", bad)
		}
	}
}

func TestSystemFile(t *testing.T) {
	if got := SystemFile("/r/config/opencode.jsonc", "{file:./prompts/reviewer.md}"); got != "/r/config/prompts/reviewer.md" {
		t.Fatalf("got %q", got)
	}
	if got := SystemFile("/r/config/opencode.jsonc", "inline prompt"); got != "" {
		t.Fatalf("inline: got %q", got)
	}
}
