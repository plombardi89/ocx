// Package ocx carries the OpenCode configuration this repository ships, so
// the ocx binary can install it without a checkout.
package ocx

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed config/opencode.jsonc config/global-AGENTS.md config/prompts skills commands
var embedded embed.FS

// Files returns the configuration keyed by where it goes inside OpenCode's
// global configuration directory: opencode.jsonc, AGENTS.md, prompts/,
// skills/, and commands/.
func Files() (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := embedded.ReadFile(path)
		if err != nil {
			return err
		}
		files[installPath(path)] = data
		return nil
	})
	return files, err
}

func installPath(path string) string {
	switch path {
	case "config/opencode.jsonc":
		return "opencode.jsonc"
	case "config/global-AGENTS.md":
		return "AGENTS.md"
	default:
		return strings.TrimPrefix(path, "config/")
	}
}
