package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaleClaude(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "wotctx.exe")
	write(t, exe, "new")
	written := time.Date(2026, 10, 9, 21, 32, 0, 0, time.UTC)
	if err := os.Chtimes(exe, written, written); err != nil {
		t.Fatal(err)
	}
	before, after := written.Add(-4*time.Minute), written.Add(time.Minute)
	other := t.TempDir()

	procs := []Proc{
		{Path: exe, Started: before},                                       // Desktop's, before the update: stale
		{Path: strings.ToUpper(exe), Started: before},                      // the same folder, spelled otherwise
		{Path: filepath.Join(dir, "wotctx.old.exe"), Started: before},      // renamed while it ran: stale
		{Path: exe, Started: after},                                        // restarted since: current
		{Path: filepath.Join(dir, "wotctx-launcher.exe"), Started: before}, // the launcher, not a server
		{Path: filepath.Join(other, "wotctx.exe"), Started: before},        // a developer's own build elsewhere
	}
	if got := staleClaude(dir, procs); got != 3 {
		t.Errorf("stale = %d, want 3", got)
	}
	if got := staleClaude(t.TempDir(), procs); got != 0 {
		t.Errorf("with no wotctx.exe installed, stale = %d, want 0", got)
	}
}

// TestClaudeRowAsksForARestart: an extension that is current is not enough
// when Claude Desktop still runs the wotctx from before the update.
func TestClaudeRowAsksForARestart(t *testing.T) {
	f := newFixture(t, true)
	write(t, filepath.Join(f.payload, "wotctx-0.5.0.mcpb"), "zip")
	write(t, filepath.Join(f.claude.LocalAppData, "AnthropicClaude", "claude.exe"), "")
	write(t, filepath.Join(f.claude.AppData, "Claude", "extensions-installations.json"),
		`{"extensions":{"local.mcpb.someone.wotctx":{"version":"0.5.0","manifest":{"name":"wotctx"}}}}`)
	exe := filepath.Join(f.payload, "wotctx.exe")
	write(t, exe, "new")

	if row := f.row(t, "claude"); row.State != stateOK {
		t.Fatalf("with no Claude running: %+v", row)
	}

	f.procs = []Proc{{Path: exe, Started: time.Now().Add(-time.Hour)}}
	row := f.row(t, "claude")
	if row.State != stateWarn || row.Summary != "Restart Claude to use this version." || !strings.Contains(row.Hint, "Quit") {
		t.Errorf("row = %+v", row)
	}
	if !strings.Contains(strings.Join(row.Lines, "\n"), "1 Claude session(s) still run") {
		t.Errorf("lines = %v", row.Lines)
	}

	f.procs = []Proc{{Path: exe, Started: time.Now().Add(time.Minute)}}
	if row := f.row(t, "claude"); row.State != stateOK {
		t.Errorf("after the restart: %+v", row)
	}
}
