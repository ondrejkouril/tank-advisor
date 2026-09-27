// Package cli implements the wotctx command tree: dispatch, help and flag setup.
//
// The tree was declared in full from the start, so that `wotctx --help` always
// described the whole intended surface (docs/spec.md section 7) while plan steps
// filled it in. Every command is now implemented, the MCP server (mcp.go)
// included.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/secrets"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// ErrUsage reports that a usage message has already been written to stderr.
// main exits with status 2 without printing anything further.
var ErrUsage = errors.New("cli: usage")

// Env carries the handles a command needs. Later steps attach the store and the
// API clients here rather than reaching for package globals.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is read for confirmations; nil means the process's stdin.
	Stdin   io.Reader
	Version string

	Paths   config.Paths
	Config  config.Config
	Secrets secrets.Store

	// Clock supplies the current time. Tests override it so that age and
	// staleness assertions are exact rather than approximate.
	Clock func() time.Time

	// ConfigErr records a failure to resolve paths or load the config file.
	// Bootstrap does not abort on one, so that `version`, `--help` and `doctor`
	// keep working on a machine whose config is broken - diagnosing that is
	// precisely what doctor is for.
	ConfigErr error

	redactor *secrets.Redactor
}

// Bootstrap builds the Env for a real run.
func Bootstrap(stdout, stderr io.Writer, version string) *Env {
	env := &Env{
		Stdout:  stdout,
		Stderr:  stderr,
		Version: version,
		Config:  config.Default(),
		Secrets: secrets.New(),
	}

	paths, err := config.Resolve()
	if err != nil {
		env.ConfigErr = err
		return env
	}
	env.Paths = paths

	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		env.ConfigErr = err
		return env
	}
	env.Config = cfg
	return env
}

func (e *Env) stdin() io.Reader {
	if e.Stdin != nil {
		return e.Stdin
	}
	return os.Stdin
}

// now returns the current time, honouring a test clock if one is set.
func (e *Env) now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

// OpenStore opens the cache database, creating the data directory if it is
// missing. The caller must Close the result.
func (e *Env) OpenStore(ctx context.Context) (*store.DB, error) {
	if e.ConfigErr != nil {
		return nil, e.ConfigErr
	}
	if err := e.Paths.EnsureDataDir(); err != nil {
		return nil, err
	}
	return store.Open(ctx, e.Paths.DBFile)
}

// HasStore reports whether a cache database exists yet, without creating one.
// Diagnosis must not have side effects, so doctor asks this before opening.
func (e *Env) HasStore() bool {
	if e.Paths.DBFile == "" {
		return false
	}
	info, err := os.Stat(e.Paths.DBFile)
	return err == nil && !info.IsDir()
}

// Redactor returns a redactor preloaded with whatever credentials this run can
// see. Every path that prints an error touching a URL or a credential must pass
// the text through it.
func (e *Env) Redactor() *secrets.Redactor {
	if e.redactor == nil {
		if e.Secrets != nil {
			e.redactor = secrets.FromStore(e.Secrets, wg.BuiltinApplicationID())
		} else {
			e.redactor = secrets.NewRedactor(wg.BuiltinApplicationID())
		}
	}
	return e.redactor
}

// Command is one node of the command tree. A node has either Run or Sub, never both.
type Command struct {
	Name  string
	Args  string // argument summary for the usage line, e.g. "<name|id>"
	Short string // one line, shown when a parent lists its children
	Sub   []*Command
	Run   func(ctx context.Context, env *Env, args []string) error
}

// Run dispatches args against the command tree.
func Run(ctx context.Context, env *Env, args []string) error {
	return dispatch(ctx, env, root(), []string{"wotctx"}, args)
}

func root() *Command {
	return &Command{
		Name:  "wotctx",
		Short: "World of Tanks account context for Claude",
		Sub: []*Command{
			{
				Name:  "auth",
				Short: "Store and refresh API credentials",
				Sub: []*Command{
					{
						Name:  "wg",
						Args:  "[--realm eu|com|asia] [--application-id ID] [--relogin] [--prolong] [--logout] [--manual]",
						Short: "Log in to the Wargaming API over OpenID",
						Run:   runAuthWG,
					},
					// No Tomato.gg command: its API requires a paid
					// subscription to issue a key, so it is out of the design
					// (docs/spec.md section 3.2).
				},
			},
			{
				Name:  "sync",
				Args:  "[--only wg|wn8|mod] [--full] [--dry-run] [--quiet]",
				Short: "Fetch fresh data into the local cache",
				Run:   runSync,
			},
			{
				Name:  "brief",
				Args:  "[--max-chars 10000]",
				Short: "Print a compact Markdown account brief",
				Run:   runBrief,
			},
			{
				Name:  "query",
				Short: "Answer one narrow question as JSON",
				Sub: []*Command{
					{
						Name:  "garage",
						Args:  "[--tier N] [--class X]",
						Short: "Vehicles currently owned",
						Run:   runQueryGarage,
					},
					{
						Name:  "tank",
						Args:  "<name|id>",
						Short: "Everything known about one vehicle",
						Run:   runQueryTank,
					},
					{
						Name:  "performance",
						Args:  "[--by class|tier|nation] [--window lifetime|30d|60d] [--min-tier N]",
						Short: "Rollups with confidence flags",
						Run:   runQueryPerformance,
					},
					{
						Name:  "resources",
						Short: "Credits, gold, bonds, free XP and premium status",
						Run:   runQueryResources,
					},
					{
						Name:  "sessions",
						Args:  "[--window 7d]",
						Short: "Recent play, one interval per pair of syncs",
						Run:   runQuerySessions,
					},
					{
						Name:  "candidates",
						Args:  "[--budget-credits N]",
						Short: "Purchase and research options, with affordability",
						Run:   runQueryCandidates,
					},
					{
						Name:  "missions",
						Args:  "[--operation X [--open]]",
						Short: "Personal-mission progress, and the open missions per class",
						Run:   runQueryMissions,
					},
				},
			},
			{
				Name:  "guide",
				Args:  "[--topic advice|core|framework|metrics|queries|meta-sources]",
				Short: "Print the advice guide: core rules, the player's settings, the framework",
				Run:   runGuide,
			},
			{
				Name:  "doctor",
				Args:  "[--json]",
				Short: "Report auth status, data age and known gaps",
				Run:   runDoctor,
			},
			{
				Name:  "data",
				Short: "The data wotctx holds about the account",
				Sub: []*Command{
					{
						Name:  "delete",
						Args:  "[--yes]",
						Short: "Delete the cache, the mod's dump, the login token and the recorded account",
						Run:   runDataDelete,
					},
				},
			},
			{
				Name:  "overlay",
				Short: "Inspect the hand-edited overlay",
				Sub: []*Command{
					{
						Name:  "validate",
						Short: "Check the overlay against the schema and the vehicle list",
						Run:   runOverlayValidate,
					},
					{
						Name:  "path",
						Short: "Print the overlay file path",
						Run:   runOverlayPath,
					},
				},
			},
			{
				Name:  "meta",
				Short: "Fetch server-wide reference data at question time (never cached)",
				Sub: []*Command{
					{
						Name:  "moe",
						Args:  "[--pretty] <name|id>",
						Short: "Mark of Excellence thresholds from tomato.gg, beside your combined damage",
						Run:   runMetaMoE,
					},
				},
			},
			{
				Name:  "mcp",
				Short: "Serve the query layer to MCP clients over stdio",
				Run:   runMCP,
			},
			{
				Name:  "mod",
				Short: "The World of Tanks client mod",
				Sub: []*Command{
					{
						Name:  "install",
						Args:  "[--game-dir DIR] <package.wotmod>",
						Short: "Copy the mod into the game's mods folder for its current version",
						Run:   runModInstall,
					},
				},
			},
			{
				Name:  "export",
				Short: "Write files for use elsewhere",
				Sub: []*Command{
					{
						Name:  "claude-ai",
						Args:  "[--out DIR]",
						Short: "The brief and the skill zip, for a claude.ai Project",
						Run:   runExportClaudeAI,
					},
				},
			},
			{
				Name:  "hook",
				Short: "Entry points for the Claude Code plugin's hooks",
				Sub: []*Command{
					{
						Name:  "session-start",
						Short: "Print the cached brief if the plugin's inject_brief option is on",
						Run:   runHookSessionStart,
					},
				},
			},
			{
				Name:  "version",
				Short: "Print the build version",
				Run:   runVersion,
			},
		},
	}
}

func dispatch(ctx context.Context, env *Env, cmd *Command, path, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		writeHelp(env.Stdout, cmd, path)
		return nil
	}
	if cmd.Run != nil {
		return cmd.Run(ctx, env, args)
	}
	if len(args) == 0 {
		writeHelp(env.Stderr, cmd, path)
		return ErrUsage
	}
	for _, sub := range cmd.Sub {
		if sub.Name == args[0] {
			return dispatch(ctx, env, sub, join(path, args[0]), args[1:])
		}
	}
	fmt.Fprintf(env.Stderr, "unknown command %q\n\n", strings.Join(join(path[1:], args[0]), " "))
	writeHelp(env.Stderr, cmd, path)
	return ErrUsage
}

func isHelpArg(s string) bool {
	return s == "-h" || s == "--help" || s == "help"
}

// join appends to a path without aliasing the caller's backing array.
func join(path []string, name string) []string {
	out := make([]string, 0, len(path)+1)
	out = append(out, path...)
	return append(out, name)
}

func writeHelp(w io.Writer, cmd *Command, path []string) {
	full := strings.Join(path, " ")
	fmt.Fprintf(w, "%s - %s\n\nUsage:\n", full, cmd.Short)

	if cmd.Run != nil {
		if cmd.Args == "" {
			fmt.Fprintf(w, "  %s\n", full)
		} else {
			fmt.Fprintf(w, "  %s %s\n", full, cmd.Args)
		}
		return
	}

	fmt.Fprintf(w, "  %s <command> [flags]\n\nCommands:\n", full)
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	writeCommandList(tw, cmd, nil)
	tw.Flush()
}

// writeCommandList prints the subtree depth-first so that root help shows every
// command, including nested ones.
func writeCommandList(tw *tabwriter.Writer, cmd *Command, prefix []string) {
	for _, sub := range cmd.Sub {
		name := strings.Join(join(prefix, sub.Name), " ")
		if sub.Run != nil && sub.Args != "" {
			name += " " + sub.Args
		}
		fmt.Fprintf(tw, "  %s\t%s\n", name, sub.Short)
		if sub.Run == nil {
			writeCommandList(tw, sub, join(prefix, sub.Name))
		}
	}
}

// newFlagSet builds a flag set that reports errors through env.Stderr, so that
// commands never write to the process streams directly.
func newFlagSet(env *Env, name string) *flag.FlagSet {
	fs := flag.NewFlagSet("wotctx "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

func runVersion(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "version")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	fmt.Fprintln(env.Stdout, env.Version)
	return nil
}
