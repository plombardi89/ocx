package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// OpencodeBin returns the opencode executable, overridable with OCX_OPENCODE.
func OpencodeBin() string {
	if bin := os.Getenv("OCX_OPENCODE"); bin != "" {
		return bin
	}
	return "opencode"
}

// Part is the subset of an event part ocx uses.
type Part struct {
	ID        string     `json:"id"`
	SessionID string     `json:"sessionID"`
	MessageID string     `json:"messageID"`
	Type      string     `json:"type"`
	Text      string     `json:"text"`
	Tool      string     `json:"tool"`
	State     *ToolState `json:"state"`
}

// ToolState is a tool call's state.
type ToolState struct {
	Status   string          `json:"status"`
	Input    map[string]any  `json:"input"`
	Output   any             `json:"output"`
	Error    any             `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type event struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      Part   `json:"part"`
}

// Transcript is the parsed `opencode run --format json` stream.
type Transcript struct {
	SessionID string
	Texts     []Part
	Tools     []Part
	Duration  time.Duration
}

// RunOpencode runs one headless session in dir and writes the raw event
// stream and stderr next to each other in outDir.
func RunOpencode(ctx context.Context, dir, agent, model, prompt, eventsPath, stderrPath string) (*Transcript, error) {
	args := []string{"run", "--standalone", "--format", "json", "--auto", "--agent", agent}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, OpencodeBin(), args...)
	cmd.Dir = dir
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)
	_ = os.WriteFile(eventsPath, stdout.Bytes(), 0o644)
	_ = os.WriteFile(stderrPath, stderr.Bytes(), 0o644)

	t := &Transcript{Duration: elapsed}
	sc := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev event
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if t.SessionID == "" && ev.SessionID != "" {
			t.SessionID = ev.SessionID
		}
		switch ev.Type {
		case "text":
			t.Texts = append(t.Texts, ev.Part)
		case "tool_use":
			t.Tools = append(t.Tools, ev.Part)
		}
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return t, fmt.Errorf("opencode run timed out after %s", elapsed.Round(time.Second))
		}
		return t, fmt.Errorf("opencode run: %w: %s", runErr, lastLine(stderr.String()))
	}
	if t.SessionID == "" {
		return t, fmt.Errorf("opencode run produced no session: %s", lastLine(stderr.String()))
	}
	return t, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// FinalMessage returns the text of the last assistant message that has text.
func (t *Transcript) FinalMessage() string {
	if len(t.Texts) == 0 {
		return ""
	}
	last := t.Texts[len(t.Texts)-1].MessageID
	var b strings.Builder
	for _, p := range t.Texts {
		if p.MessageID == last {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// ToolCalls returns tool calls whose tool name is one of names.
func (t *Transcript) ToolCalls(names ...string) []Part {
	var out []Part
	for _, p := range t.Tools {
		for _, n := range names {
			if p.Tool == n {
				out = append(out, p)
			}
		}
	}
	return out
}

// ChildSessionID extracts the child session of a subagent tool call.
func ChildSessionID(p Part) string {
	if p.State == nil || len(p.State.Metadata) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(p.State.Metadata, &v) != nil {
		return ""
	}
	var found string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if id, ok := x["sessionID"].(string); ok && strings.HasPrefix(id, "ses_") && id != p.SessionID && found == "" {
				found = id
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
	return found
}

// ModelRef is a model as recorded on a message.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant"`
}

func (m ModelRef) String() string {
	s := m.ProviderID + "/" + m.ID
	if m.Variant != "" {
		s += "#" + m.Variant
	}
	return s
}

// Export is the subset of `opencode session export` ocx uses.
type Export struct {
	Info struct {
		ID     string  `json:"id"`
		Cost   float64 `json:"cost"`
		Tokens struct {
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
		} `json:"tokens"`
	} `json:"info"`
	Messages []struct {
		Type  string    `json:"type"`
		Agent string    `json:"agent"`
		Model *ModelRef `json:"model"`
	} `json:"messages"`
}

// AssistantModels returns the distinct agent/model pairs that produced
// assistant messages, formatted as "agent=model".
func (e *Export) AssistantModels() []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range e.Messages {
		if m.Type != "assistant" || m.Model == nil {
			continue
		}
		k := m.Agent + "=" + m.Model.String()
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// ExportSession exports a session and saves the raw JSON to path.
func ExportSession(ctx context.Context, dir, sessionID, path string) (*Export, error) {
	cmd := exec.CommandContext(ctx, OpencodeBin(), "session", "export", "--standalone", sessionID)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("export %s: %w", sessionID, err)
	}
	_ = os.WriteFile(path, out, 0o644)
	var e Export
	if err := json.Unmarshal(out, &e); err != nil {
		return nil, fmt.Errorf("export %s: %w", sessionID, err)
	}
	return &e, nil
}
