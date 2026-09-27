package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ondrejkouril/tank-advisor/internal/brief"
	"github.com/ondrejkouril/tank-advisor/internal/query"
	"github.com/ondrejkouril/tank-advisor/internal/store"
	"github.com/ondrejkouril/tank-advisor/skills"
)

// mcpInstructions tell an MCP client how to use the tools. A client other than
// Claude Code has no wot-advisor skill to learn the procedure from, so the
// rules that keep answers honest travel with the server.
const mcpInstructions = `World of Tanks account data for one player, cached locally by wotctx.

Procedure:
0. Before the first recommendation or judgement in a conversation, call wot_guide once and follow it: it holds the core rules, the player's own advice settings and the answer format, which override general knowledge of the game. (Where a wot-advisor skill is available, wotctx guide prints the same text.)
1. Call wot_data_status first. If a data source is stale (a data-age warning), call wot_sync, then continue. If Wargaming auth has failed, stop and tell the user the exact command from the "fix" field; it must be run in a terminal.
2. Call wot_brief to orient, then only the narrow tools the question needs.
3. Every result carries meta.sources (with ages) and meta.caveats. State the data age and any caveat that bears on the answer; state each figure's confidence (battle count).
4. The tools return numbers, never verdicts. Do not rest a verdict on a single metric: combine win rate, WN8 and damage, and compare within a tier band.
5. Per-vehicle XP, Marks of Excellence progress, loadouts and crew are in no API. They come only from the game client, through the client mod's dump (source mod:garage): wot_tank's "client" block, wot_moe's "client", and the premium source in wot_resources. Quote the dump's capture time with them; if a caveat says battles were played after it, say XP and marks are behind. Without a dump, say they are unavailable rather than guessing.
6. The overlay (wot-overlay.yaml, edited by hand) holds the player's preferences and goals. Its premium status, researched tanks and banked XP are only a backup for when there is no client mod dump. Credits, gold, bonds, free XP, the garage and all statistics come from Wargaming, never from the overlay.
Never paste raw JSON into an answer.`

// mcpServer holds the Env the tools run against. Calls are serialised: a sync
// and a query racing on SQLite would probably be fine under WAL, but nothing
// here needs the throughput.
type mcpServer struct {
	env *Env
	mu  sync.Mutex
}

func runMCP(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "mcp")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if fs.NArg() > 0 {
		return usageErr(env, "mcp takes no arguments")
	}
	// Stdout carries the protocol. Anything a command would print goes to
	// stderr instead, where it reaches the client's log and cannot corrupt a
	// JSON-RPC frame.
	env.Stdout = env.Stderr
	return newMCPServer(env).Run(ctx, mcpTransport())
}

func newMCPServer(env *Env) *mcp.Server {
	m := &mcpServer{env: env}
	server := mcp.NewServer(&mcp.Implementation{Name: "wotctx", Title: "Tank Advisor data", Version: env.Version},
		&mcp.ServerOptions{Instructions: mcpInstructions})

	readOnly := func(title string) *mcp.ToolAnnotations {
		closed := false
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: &closed}
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_guide",
		Description: "How to advise this player: the procedure, the fixed core rules that keep answers true to the data, the player's own advice settings (who they are, how much each factor counts, how answers should look, their own rules) and the default framework. Call it once, before the first recommendation or judgement in a conversation; the player's settings override general knowledge of the game, and the core rules override everything. The default topic is all of it; other topics are reference.",
		Annotations: readOnly("Advisor guide"),
	}, m.guide)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_data_status",
		Description: "Health and freshness of the cached data: auth token expiry, age of each data source, vehicle coverage, overlay and WN8 status. Call it first. A check with status warn or fail carries a 'fix' naming what to run.",
		Annotations: readOnly("Data status"),
	}, m.dataStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_brief",
		Description: "A compact Markdown brief of the whole account (resources, garage, performance by class, strongest and weakest tanks, goals, data age, known gaps). The best first read for any question.",
		Annotations: readOnly("Account brief"),
	}, m.brief)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_resources",
		Description: "Credits, gold, bonds, free XP, premium account and WoT Plus status (from the player's overlay, never the API) and active reserves.",
		Annotations: readOnly("Resources"),
	}, m.resources)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_garage",
		Description: "Vehicles currently owned, with a lifetime random-battle line per tank (battles, win rate, damage, WN8, confidence), counted by tier and class. mastery is the Mastery Badge: 0 none, 1 3rd Class, 2 2nd Class, 3 1st Class, 4 Ace Tanker.",
		Annotations: readOnly("Garage"),
	}, m.garage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_tank",
		Description: "Everything known about one vehicle: lifetime and 30/60-day lines, all-battles figures, research links, the overlay's notes, and a 'client' block from the game client (vehicle XP, marks and progress, equipment, shells, consumables, directives, crew skills). elite means every module and every follow-on vehicle is researched; false does not show that modules are locked. Accepts a name (diacritics optional, e.g. 'Tesak') or a tank_id. mastery is the Mastery Badge: 0 none, 1 3rd Class, 2 2nd Class, 3 1st Class, 4 Ace Tanker.",
		Annotations: readOnly("One tank"),
	}, m.tank)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_performance",
		Description: "Rollups by class, tier or nation over a window, each with a confidence flag, plus a total. A window without a snapshot old enough says 'not yet' instead of reporting a shorter one.",
		Annotations: readOnly("Performance rollups"),
	}, m.performance)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_sessions",
		Description: "Recent play, one interval per pair of consecutive syncs (there is no battle log, so a sync interval is the finest grain). Each interval states its span.",
		Annotations: readOnly("Recent sessions"),
	}, m.sessions)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_candidates",
		Description: "Purchase and research options: researched but not bought, next research steps, and the path to the overlay's goals, each with XP and credit cost, whether free XP covers it, and affordability. Classes the player avoids are left out except for goals.",
		Annotations: readOnly("Purchase and research candidates"),
	}, m.candidates)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_missions",
		Description: "Personal-mission progress per operation, and the open missions per class. Name an operation to list its missions.",
		Annotations: readOnly("Personal missions"),
	}, m.missions)

	open := true
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_moe",
		Description: "Mark of Excellence damage thresholds for one vehicle, fetched from tomato.gg now (never cached), beside the player's marks, progress % and moving average from the game client (client mod dump) and a combined-damage range from statistics. Quote the table's date and the dump's.",
		Annotations: &mcp.ToolAnnotations{Title: "Mark of Excellence thresholds", ReadOnlyHint: true, OpenWorldHint: &open},
	}, m.moe)

	notDestructive := false
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_sync",
		Description: "Fetch fresh data from Wargaming and XVM into the local cache. Sources still inside their refresh interval are skipped, so repeating it is cheap. Every sync also adds a snapshot, which is how recent-form history accumulates.",
		Annotations: &mcp.ToolAnnotations{Title: "Sync", DestructiveHint: &notDestructive, IdempotentHint: true, OpenWorldHint: &open},
	}, m.sync)

	return server
}

// Tool inputs. Optional fields are omitempty so the inferred schema does not
// require them; defaults are applied in the handlers and stated in the
// descriptions.

type noInput struct{}

type guideInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"procedure (the procedure, core rules, the player's settings and the framework: read first), advice (the same without the procedure), core, framework, metrics (definitions and thresholds), queries (what each tool answers) or meta-sources (web pages for server-wide data); default procedure"`
}

type briefInput struct {
	MaxChars int `json:"max_chars,omitempty" jsonschema:"character cap, at least 2000; default 10000"`
}

type garageInput struct {
	Tier  int    `json:"tier,omitempty" jsonschema:"only vehicles of this tier, 1-11"`
	Class string `json:"class,omitempty" jsonschema:"only vehicles of this class: lightTank, mediumTank, heavyTank, AT-SPG or SPG (short forms such as LT, MT, HT, TD accepted)"`
}

type tankInput struct {
	Tank string `json:"tank" jsonschema:"vehicle name or tank_id"`
}

type performanceInput struct {
	By      string `json:"by,omitempty" jsonschema:"group by class, tier or nation; default class"`
	Window  string `json:"window,omitempty" jsonschema:"lifetime, or a number of days such as 30d; default lifetime"`
	MinTier int    `json:"min_tier,omitempty" jsonschema:"leave out tanks below this tier; 8 compares current play"`
}

type sessionsInput struct {
	Window string `json:"window,omitempty" jsonschema:"a number of days such as 7d; default 7d"`
}

type candidatesInput struct {
	BudgetCredits int `json:"budget_credits,omitempty" jsonschema:"judge affordability against this many credits instead of the account's"`
}

type missionsInput struct {
	Operation string `json:"operation,omitempty" jsonschema:"list every mission of one operation, by id or name (e.g. Object 260)"`
	OpenOnly  bool   `json:"open_only,omitempty" jsonschema:"with operation, list only the missions not yet done"`
}

type syncInput struct {
	Only string `json:"only,omitempty" jsonschema:"sync just one source: wg, wn8 or mod (the client mod's garage dump); default all"`
	Full bool   `json:"full,omitempty" jsonschema:"ignore refresh intervals and refetch everything; spends Wargaming requests"`
}

// syncOutput is wot_sync's result: the CLI's sync report as data.
type syncOutput struct {
	Synced  []string       `json:"synced"`
	Skipped []string       `json:"skipped"`
	Counts  map[string]int `json:"counts,omitempty"`
	Caveats []string       `json:"caveats"`
	Seconds float64        `json:"seconds"`
}

// guidePreamble maps the skill's CLI wording onto this server. The skill is
// written for Claude Code, where it runs wotctx in a shell; run 4 of the
// evaluation showed a model maps the commands to tools unaided, and this makes
// sure of it.
const guidePreamble = `You are reading the wot-advisor skill through the wotctx MCP server. Where it says to run a command, call the matching tool instead:

- wotctx doctor --json → wot_data_status
- wotctx sync → wot_sync
- wotctx brief → wot_brief
- wotctx query resources | garage | tank | performance | sessions | candidates | missions → wot_resources, wot_garage, wot_tank, wot_performance, wot_sessions, wot_candidates, wot_missions (flags become arguments: --min-tier 8 is min_tier: 8, --open is open_only: true)
- wotctx meta moe → wot_moe
- references/<name>.md → wot_guide with topic <name>

Its rules about shells (Bash, PowerShell, one command per call) apply only to the command line; ignore them here. Editing the overlay file and logging in to Wargaming need a terminal: tell the player the command to run.

`

// guide serves the procedure followed by `wotctx guide`'s text, or one topic
// of it. The default is exactly guidePreamble + the skill's body + the
// separator + `wotctx guide`, a relation a test pins.
func (m *mcpServer) guide(_ context.Context, _ *mcp.CallToolRequest, in guideInput) (*mcp.CallToolResult, any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	topic := strings.TrimSpace(in.Topic)
	if topic == "" || topic == "procedure" {
		text, err := m.env.guideText("advice")
		if err != nil {
			return nil, nil, m.toolErr(err)
		}
		return textResult(guidePreamble + skills.Body() + guideRule + text), nil, nil
	}
	text, err := m.env.guideText(topic)
	if err != nil {
		return nil, nil, m.toolErr(fmt.Errorf("%w; or procedure", err))
	}
	return textResult(text), nil, nil
}

func (m *mcpServer) dataStatus(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A failing check is data, not a tool error: the client needs to read it.
	return m.jsonResult(doctorReport{Version: m.env.Version, Checks: collectChecks(ctx, m.env)})
}

func (m *mcpServer) brief(ctx context.Context, _ *mcp.CallToolRequest, in briefInput) (*mcp.CallToolResult, any, error) {
	maxChars := in.MaxChars
	if maxChars == 0 {
		maxChars = brief.DefaultMaxChars
	}
	if err := checkMaxChars(maxChars); err != nil {
		return nil, nil, m.toolErr(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out, err := m.env.renderBrief(ctx, maxChars)
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	return textResult(out), nil, nil
}

func (m *mcpServer) resources(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return s.Resources(ctx) })
}

func (m *mcpServer) garage(ctx context.Context, _ *mcp.CallToolRequest, in garageInput) (*mcp.CallToolResult, any, error) {
	f, err := garageFilter(in.Tier, in.Class)
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return s.Garage(ctx, f) })
}

func (m *mcpServer) tank(ctx context.Context, _ *mcp.CallToolRequest, in tankInput) (*mcp.CallToolResult, any, error) {
	ref := strings.TrimSpace(in.Tank)
	if ref == "" {
		return nil, nil, m.toolErr(errors.New("tank needs a vehicle name or tank_id"))
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return s.Tank(ctx, ref) })
}

func (m *mcpServer) performance(ctx context.Context, _ *mcp.CallToolRequest, in performanceInput) (*mcp.CallToolResult, any, error) {
	by, window := in.By, in.Window
	if by == "" {
		by = query.ByClass
	}
	if window == "" {
		window = "lifetime"
	}
	w, err := performanceArgs(by, window, in.MinTier)
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return s.Performance(ctx, by, w, in.MinTier) })
}

func (m *mcpServer) sessions(ctx context.Context, _ *mcp.CallToolRequest, in sessionsInput) (*mcp.CallToolResult, any, error) {
	window := in.Window
	if window == "" {
		window = "7d"
	}
	w, err := sessionsWindow(window)
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return s.Sessions(ctx, w) })
}

func (m *mcpServer) candidates(ctx context.Context, _ *mcp.CallToolRequest, in candidatesInput) (*mcp.CallToolResult, any, error) {
	if err := checkBudget(in.BudgetCredits); err != nil {
		return nil, nil, m.toolErr(err)
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return s.Candidates(ctx, in.BudgetCredits) })
}

func (m *mcpServer) missions(ctx context.Context, _ *mcp.CallToolRequest, in missionsInput) (*mcp.CallToolResult, any, error) {
	if err := checkMissionsArgs(in.Operation, in.OpenOnly); err != nil {
		return nil, nil, m.toolErr(err)
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) {
		return s.Missions(ctx, in.Operation, in.OpenOnly)
	})
}

func (m *mcpServer) moe(ctx context.Context, _ *mcp.CallToolRequest, in tankInput) (*mcp.CallToolResult, any, error) {
	ref := strings.TrimSpace(in.Tank)
	if ref == "" {
		return nil, nil, m.toolErr(errors.New("wot_moe needs a vehicle name or tank_id"))
	}
	return m.query(ctx, func(s *query.Service) (query.Envelope, error) { return m.env.moe(ctx, s, ref) })
}

func (m *mcpServer) sync(ctx context.Context, _ *mcp.CallToolRequest, in syncInput) (*mcp.CallToolResult, any, error) {
	if err := checkSyncSource(in.Only); err != nil {
		return nil, nil, m.toolErr(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	report, err := m.env.sync(ctx, syncOptions{Only: in.Only, Full: in.Full})
	if errors.Is(err, store.ErrSyncRunning) {
		// Not an error for the model to act on: the data is being fetched.
		return textResult("another sync (a session-start hook, or another client) is still running; call wot_data_status in a minute to see its result"), nil, nil
	}
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	if !report.OK() && len(report.Caveats) > 0 {
		return nil, nil, m.toolErr(fmt.Errorf("nothing could be synced: %s", strings.Join(report.Caveats, "; ")))
	}
	out := syncOutput{
		Synced:  nonNil(report.Synced),
		Skipped: nonNil(report.Skipped),
		Counts:  report.Counts,
		Caveats: nonNil(report.Caveats),
	}
	if !report.Finished.IsZero() {
		out.Seconds = report.Finished.Sub(report.Started).Round(100 * time.Millisecond).Seconds()
	}
	return m.jsonResult(out)
}

// query opens the cache, runs one query and returns its envelope as the same
// compact JSON the CLI prints.
func (m *mcpServer) query(ctx context.Context, run func(*query.Service) (query.Envelope, error)) (*mcp.CallToolResult, any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, closeDB, err := m.env.queryService(ctx)
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	defer closeDB()
	result, err := run(s)
	if err != nil {
		return nil, nil, m.toolErr(err)
	}
	return m.jsonResult(result)
}

func (m *mcpServer) jsonResult(v any) (*mcp.CallToolResult, any, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		return nil, nil, fmt.Errorf("encoding result: %w", err)
	}
	return textResult(strings.TrimSuffix(buf.String(), "\n")), nil, nil
}

// toolErr redacts an error on its way to the client, as main does on its way
// to the terminal: errors can quote request URLs. The SDK returns it as a tool
// result with isError set, so the model reads it instead of the call failing.
func (m *mcpServer) toolErr(err error) error {
	return errors.New(m.env.Redactor().RedactError(err))
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// mcpTransport is the process's stdin and stdout; tests replace it.
var mcpTransport = func() mcp.Transport { return &mcp.StdioTransport{} }
