package cli

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/ondrejkouril/tank-advisor/internal/brief"
	"github.com/ondrejkouril/tank-advisor/skills"
)

// runExportClaudeAI writes what claude.ai needs for use away from this machine
// (docs/plan.md, step P5): the brief, for a Project's files, and the skill as
// the zip that Settings → Capabilities → Skills accepts. There is no shell on
// claude.ai, so the skill answers from the brief alone there.
func runExportClaudeAI(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "export claude-ai")
	out := fs.String("out", "wotctx-claude-ai", "directory to write wot-brief.md and wot-advisor.zip into")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if fs.NArg() > 0 {
		return usageErr(env, "export claude-ai takes no arguments")
	}

	text, err := env.renderBrief(ctx, brief.DefaultMaxChars)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	briefPath := filepath.Join(*out, "wot-brief.md")
	if err := os.WriteFile(briefPath, []byte(text), 0o644); err != nil {
		return err
	}
	zipPath := filepath.Join(*out, "wot-advisor.zip")
	guide, err := env.guideText("advice")
	if err != nil {
		return err
	}
	if err := writeSkillZip(zipPath, guide); err != nil {
		return err
	}

	fmt.Fprintf(env.Stdout, "wrote %s\nwrote %s\n", briefPath, zipPath)
	fmt.Fprintln(env.Stdout, "Upload the zip once per skill change (Settings > Capabilities > Skills), and the brief to your Project after each sync you want to take with you.")
	return nil
}

// writeSkillZip packs the embedded skill as wot-advisor/..., the layout
// claude.ai expects: one folder with SKILL.md at its top. guide, the rendered
// `wotctx guide`, goes in as references/guide.md, since claude.ai has no
// wotctx to run: the phone then follows the same settings (docs/spec-desktop.md
// section 7.3).
func writeSkillZip(dest, guide string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	w := zip.NewWriter(f)
	err = fs.WalkDir(skills.Advisor, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(skills.Advisor, name)
		if err != nil {
			return err
		}
		entry, err := w.Create(path.Join("wot-advisor", name))
		if err != nil {
			return err
		}
		_, err = entry.Write(raw)
		return err
	})
	if err == nil {
		var entry io.Writer
		if entry, err = w.Create("wot-advisor/references/guide.md"); err == nil {
			_, err = io.WriteString(entry, guide)
		}
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
