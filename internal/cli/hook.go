package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/brief"
)

// injectBriefOption is the Claude Code plugin's userConfig option inject_brief,
// as Claude Code exports it to hook processes (.claude-plugin/plugin.json).
const injectBriefOption = "CLAUDE_PLUGIN_OPTION_INJECT_BRIEF"

// runHookSessionStart is the plugin's synchronous SessionStart hook. Claude Code
// adds whatever it prints to the session's context, and the plugin is installed
// user-wide, so it fires in every session, most of them not about tanks. It
// therefore prints nothing unless the player turned inject_brief on.
//
// It never fails: a hook that errors shows a notice in a session that may
// have nothing to do with World of Tanks. A missing cache or a broken config
// is doctor's to report, when the player asks.
func runHookSessionStart(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "hook session-start")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if !optionOn(os.Getenv(injectBriefOption)) {
		return nil
	}
	out, err := env.renderBrief(ctx, brief.DefaultMaxChars)
	if err != nil {
		return nil
	}
	// The brief was not synced for this: the hook that syncs runs in the
	// background and may still be going. Its own Data age section says how
	// old each figure is.
	fmt.Fprintf(env.Stdout, "The World of Tanks account brief below comes from the wotctx cache as it stood when this session started; it was not synced for it. Before answering from it, check freshness (wotctx doctor, or the wot-advisor skill's procedure).\n\n%s", out)
	return nil
}

// optionOn reads a boolean plugin option as Claude Code exports it.
func optionOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true
	}
	return false
}
