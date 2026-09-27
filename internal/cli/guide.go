package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/advice"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/skills"
)

// guideTopics are the topics `wotctx guide` and wot_guide serve beyond the
// default, "advice". Each is one reference file, verbatim.
var guideTopics = []string{"core", "framework", "metrics", "queries", "meta-sources"}

const guideRule = "\n---\n\n"

// guideText is what `wotctx guide` prints for a topic. The default, "advice",
// is the whole judgement in its order of precedence (docs/spec-desktop.md
// section 8.2): the fixed core rules, the player's settings rendered from the
// overlay, then the framework's defaults. The same function serves wot_guide
// and the claude.ai export, so every surface reads the same words.
func (e *Env) guideText(topic string) (string, error) {
	if topic == "" || topic == "advice" {
		core, err := skills.Reference("core")
		if err != nil {
			return "", err
		}
		framework, err := skills.Reference("framework")
		if err != nil {
			return "", err
		}
		return core + guideRule + e.renderAdvice() + guideRule + framework, nil
	}
	if !slices.Contains(guideTopics, topic) {
		return "", fmt.Errorf("unknown topic %q, want advice, %s", topic, strings.Join(guideTopics, ", "))
	}
	return skills.Reference(topic)
}

// renderAdvice renders the player's settings. An overlay that is missing or
// cannot be decoded leaves the defaults, and the section says why: the guide
// must never fail, since without it the model has no rules at all.
func (e *Env) renderAdvice() string {
	if e.ConfigErr != nil {
		return advice.Render(nil)
	}
	path := e.Config.OverlayFile(e.Paths)
	o, findings, err := overlay.Load(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return advice.Render(nil)
	case err != nil || o == nil:
		note := "The overlay at " + path + " could not be read"
		if len(findings) > 0 {
			note += " (" + findings[0].String() + ")"
		}
		return "> " + note + ", so the defaults apply. Tell the player: `wotctx overlay validate` shows what to fix.\n\n" + advice.Render(nil)
	}
	return advice.Render(o)
}

func runGuide(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "guide")
	topic := fs.String("topic", "advice", "advice (the core rules, the player's settings and the framework), or one of: "+strings.Join(guideTopics, ", "))
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if fs.NArg() > 0 {
		return usageErr(env, "guide takes no arguments; use --topic")
	}
	text, err := env.guideText(*topic)
	if err != nil {
		return usageErr(env, err.Error())
	}
	fmt.Fprint(env.Stdout, text)
	return nil
}
