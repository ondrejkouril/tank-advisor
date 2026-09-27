// Package skills embeds the wot-advisor skill, so that an MCP client with no
// skill - Claude Desktop, as installed from the bundle - can still read the
// procedure and the framework through the wot_guide tool. The Markdown files
// beside this one stay the only copy of the text (docs/plan.md, phase 3).
//
// The package lives here rather than under internal/ because go:embed cannot
// reach a directory above its own package.
package skills

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed wot-advisor/SKILL.md wot-advisor/references/*.md
var files embed.FS

// Advisor is the wot-advisor skill directory: SKILL.md and references/.
var Advisor fs.FS

func init() {
	sub, err := fs.Sub(files, "wot-advisor")
	if err != nil {
		panic(err) // the embed pattern above guarantees the directory
	}
	Advisor = sub
}

// Body returns SKILL.md without its YAML frontmatter, which describes the skill
// to Claude Code and means nothing to a reader of the text.
func Body() string {
	raw, err := fs.ReadFile(Advisor, "SKILL.md")
	if err != nil {
		panic(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return strings.TrimLeft(body, "\n")
		}
	}
	return text
}

// Reference returns one file from references/ by its base name, such as
// "framework" for references/framework.md.
func Reference(name string) (string, error) {
	raw, err := fs.ReadFile(Advisor, "references/"+name+".md")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
