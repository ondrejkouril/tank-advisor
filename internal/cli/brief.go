package cli

import (
	"context"
	"fmt"

	"github.com/ondrejkouril/tank-advisor/internal/brief"
)

func runBrief(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "brief")
	maxChars := fs.Int("max-chars", brief.DefaultMaxChars, "cap the brief at this many characters")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if fs.NArg() > 0 {
		return usageErr(env, "brief takes no arguments")
	}
	if err := checkMaxChars(*maxChars); err != nil {
		return usageErr(env, err.Error())
	}

	out, err := env.renderBrief(ctx, *maxChars)
	if err != nil {
		return err
	}
	fmt.Fprint(env.Stdout, out)
	return nil
}

// renderBrief renders the brief from the cache; the CLI and the MCP server
// both call it.
func (e *Env) renderBrief(ctx context.Context, maxChars int) (string, error) {
	s, closeDB, err := e.queryService(ctx)
	if err != nil {
		return "", err
	}
	defer closeDB()
	return brief.Render(ctx, s, brief.Options{MaxChars: maxChars, Title: e.briefTitle()})
}
