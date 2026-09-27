package cli

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestExportClaudeAI(t *testing.T) {
	env, buf := seededEnv(t)
	if err := Run(context.Background(), env, []string{"brief"}); err != nil {
		t.Fatalf("brief: %v", err)
	}
	wantBrief := buf.String()

	out := filepath.Join(t.TempDir(), "export")
	if err := Run(context.Background(), env, []string{"export", "claude-ai", "--out", out}); err != nil {
		t.Fatalf("export claude-ai: %v", err)
	}

	gotBrief, err := os.ReadFile(filepath.Join(out, "wot-brief.md"))
	if err != nil {
		t.Fatalf("reading the exported brief: %v", err)
	}
	if string(gotBrief) != wantBrief {
		t.Error("the exported brief differs from `wotctx brief`")
	}

	zr, err := zip.OpenReader(filepath.Join(out, "wot-advisor.zip"))
	if err != nil {
		t.Fatalf("opening the skill zip: %v", err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := []string{
		"wot-advisor/SKILL.md",
		"wot-advisor/references/core.md",
		"wot-advisor/references/framework.md",
		// The rendered `wotctx guide`: claude.ai has no wotctx to run.
		"wot-advisor/references/guide.md",
		"wot-advisor/references/meta-sources.md",
		"wot-advisor/references/metrics.md",
		"wot-advisor/references/queries.md",
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("zip holds %v, want %v", names, want)
	}
}

// TestExportNeedsACache: exporting an empty brief would put a page with no
// data on the phone, which is worse than the error that says to sync.
func TestExportNeedsACache(t *testing.T) {
	env, _ := newTestEnv(t)
	err := Run(context.Background(), env, []string{"export", "claude-ai", "--out", filepath.Join(t.TempDir(), "x")})
	if err == nil || !strings.Contains(err.Error(), "wotctx sync") {
		t.Errorf("export with no cache = %v, want an error naming wotctx sync", err)
	}
}
