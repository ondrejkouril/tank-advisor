// Package mcpb embeds the Claude Desktop extension's manifest, so the Tank
// Advisor app can tell when the installed extension is out of date: the
// bundle needs reinstalling only when its manifest changes, which in practice
// means its tool list (docs/spec-desktop.md section 7.1).
package mcpb

import (
	_ "embed"
	"encoding/json"
)

//go:embed manifest.json
var Manifest []byte

// ToolNames returns the tools the manifest declares, in its order.
func ToolNames() []string {
	var m struct {
		Tools []struct{ Name string } `json:"tools"`
	}
	if err := json.Unmarshal(Manifest, &m); err != nil {
		panic(err) // a test parses the same file; a broken one never ships
	}
	names := make([]string, len(m.Tools))
	for i, t := range m.Tools {
		names[i] = t.Name
	}
	return names
}
