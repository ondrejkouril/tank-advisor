package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Proc is one running wotctx process.
type Proc struct {
	Path    string
	Started time.Time
}

// staleClaude counts the wotctx processes from the app's folder that started
// before the installed wotctx.exe was written: Claude sessions still serving
// the previous version's tools.
//
// The app runs wotctx in its own process and never starts wotctx.exe, so each
// one running is Claude's: the Desktop extension's, through its launcher, or a
// Claude Code session's. An MCP server keeps the code it started with, so
// after an update Claude goes on answering with the old tools, or fails
// against data the new version has upgraded, until it is restarted. Found
// after 1.1.0: the update landed four minutes after Claude Desktop started,
// and its new tools never appeared.
func staleClaude(dir string, procs []Proc) int {
	info, err := os.Stat(filepath.Join(dir, "wotctx.exe"))
	if err != nil {
		return 0
	}
	written := info.ModTime()
	n := 0
	for _, p := range procs {
		name := strings.ToLower(filepath.Base(p.Path))
		if !strings.HasPrefix(name, "wotctx") || strings.HasPrefix(name, "wotctx-launcher") {
			continue
		}
		// An updated wotctx.exe still running is renamed wotctx.old.exe in
		// the same folder, so the folder, not the name, says whose it is.
		if !strings.EqualFold(filepath.Clean(filepath.Dir(p.Path)), filepath.Clean(dir)) {
			continue
		}
		if p.Started.Before(written) {
			n++
		}
	}
	return n
}
