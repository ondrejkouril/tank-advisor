package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ondrejkouril/tank-advisor/skills"

	"github.com/ondrejkouril/tank-advisor/internal/testseed"
)

// seededEnv is an Env over the shared seed account, so tool output can be
// compared with the CLI's on data whose every figure is known.
func seededEnv(t *testing.T) (*Env, *bytes.Buffer) {
	t.Helper()
	fx := testseed.Seed(t)
	env, buf := newTestEnv(t)
	dir := filepath.Dir(fx.OverlayPath)
	env.Paths.ConfigDir, env.Paths.DataDir = dir, dir
	env.Paths.ConfigFile = filepath.Join(dir, "config.yaml")
	env.Paths.DBFile = filepath.Join(dir, "wotctx.db")
	env.Config.OverlayPath = fx.OverlayPath
	env.Clock = func() time.Time { return testseed.Now }
	return env, buf
}

// connect serves env over an in-memory transport and returns a client session.
func connect(t *testing.T, env *Env) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := newMCPServer(env).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call invokes a tool and returns its single text block and error flag.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s, %v) protocol error: %v", name, args, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("CallTool(%s) returned %d content blocks, want 1", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s) content is %T, want text", name, res.Content[0])
	}
	return text.Text, res.IsError
}

func TestMCPListsTheTools(t *testing.T) {
	env, _ := newTestEnv(t)
	cs := connect(t, env)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		// wot_sync is the only tool that writes anything.
		if got, want := tool.Annotations.ReadOnlyHint, tool.Name != "wot_sync"; got != want {
			t.Errorf("%s readOnlyHint = %v, want %v", tool.Name, got, want)
		}
	}
	sort.Strings(names)
	want := []string{"wot_brief", "wot_candidates", "wot_data_status", "wot_garage", "wot_guide", "wot_missions",
		"wot_moe", "wot_performance", "wot_resources", "wot_sessions", "wot_sync", "wot_tank"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("tools = %v\nwant    %v", names, want)
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "wot_data_status") {
		t.Error("server instructions do not name the first tool to call")
	}
}

// TestMCPToolsMatchTheCLI pins spec section 9: each tool wraps the same query
// call as its CLI counterpart and returns the same envelope, byte for byte.
func TestMCPToolsMatchTheCLI(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		cli  []string
	}{
		{"wot_resources", nil, []string{"query", "resources"}},
		{"wot_garage", nil, []string{"query", "garage"}},
		{"wot_garage", map[string]any{"tier": 9, "class": "LT"}, []string{"query", "garage", "--tier", "9", "--class", "LT"}},
		{"wot_tank", map[string]any{"tank": "AMX 13 90"}, []string{"query", "tank", "AMX", "13", "90"}},
		{"wot_tank", map[string]any{"tank": "Tesak"}, []string{"query", "tank", "Tesak"}},
		{"wot_performance", nil, []string{"query", "performance"}},
		{"wot_performance", map[string]any{"by": "tier", "window": "30d", "min_tier": 8},
			[]string{"query", "performance", "--by", "tier", "--window", "30d", "--min-tier", "8"}},
		{"wot_sessions", nil, []string{"query", "sessions"}},
		{"wot_sessions", map[string]any{"window": "30d"}, []string{"query", "sessions", "--window", "30d"}},
		{"wot_candidates", nil, []string{"query", "candidates"}},
		{"wot_candidates", map[string]any{"budget_credits": 7000000}, []string{"query", "candidates", "--budget-credits", "7000000"}},
		{"wot_missions", nil, []string{"query", "missions"}},
		{"wot_missions", map[string]any{"operation": "t 55", "open_only": true},
			[]string{"query", "missions", "--operation", "t 55", "--open"}},
		{"wot_brief", nil, []string{"brief"}},
		{"wot_brief", map[string]any{"max_chars": 3000}, []string{"brief", "--max-chars", "3000"}},
	}
	env, buf := seededEnv(t)
	cs := connect(t, env)
	for _, c := range cases {
		buf.Reset()
		if err := Run(context.Background(), env, c.cli); err != nil {
			t.Fatalf("Run(%v) = %v", c.cli, err)
		}
		want := strings.TrimSuffix(buf.String(), "\n")

		got, isErr := call(t, cs, c.tool, c.args)
		if isErr {
			t.Errorf("%s %v: tool error: %s", c.tool, c.args, got)
			continue
		}
		// The brief ends in a newline of its own; a JSON envelope does not.
		if got = strings.TrimSuffix(got, "\n"); got != want {
			t.Errorf("%s %v differs from `wotctx %s`:\n got: %s\nwant: %s",
				c.tool, c.args, strings.Join(c.cli, " "), got, want)
		}
	}
}

// TestMCPDataStatusMatchesDoctor: the same checks as `doctor --json`, compact.
func TestMCPDataStatusMatchesDoctor(t *testing.T) {
	env, buf := seededEnv(t)
	cs := connect(t, env)
	got, isErr := call(t, cs, "wot_data_status", nil)
	if isErr {
		t.Fatalf("wot_data_status: tool error: %s", got)
	}
	// Doctor exits non-zero on a failed check; the report is printed either way.
	_ = Run(context.Background(), env, []string{"doctor", "--json"})

	var fromTool, fromCLI doctorReport
	if err := json.Unmarshal([]byte(got), &fromTool); err != nil {
		t.Fatalf("tool output is not a doctor report: %v\n%s", err, got)
	}
	if err := json.Unmarshal(buf.Bytes(), &fromCLI); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, buf.String())
	}
	if !reflect.DeepEqual(fromTool, fromCLI) {
		t.Errorf("wot_data_status = %+v\nwant %+v", fromTool, fromCLI)
	}
}

// TestMCPBadArgumentsAreToolErrors: the model must be able to read what it got
// wrong, so a bad argument is a tool result with isError, not a protocol error.
func TestMCPBadArgumentsAreToolErrors(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"wot_garage", map[string]any{"tier": 12}, "out of range"},
		{"wot_garage", map[string]any{"class": "SPGs"}, "unknown class"},
		{"wot_garage", map[string]any{"tier": "nine"}, "tier"},
		{"wot_tank", map[string]any{"tank": " "}, "name or tank_id"},
		{"wot_tank", map[string]any{"tank": "Maus Mk II"}, "Maus"},
		{"wot_performance", map[string]any{"by": "crew"}, "class, tier or nation"},
		{"wot_performance", map[string]any{"window": "month"}, "month"},
		{"wot_sessions", map[string]any{"window": "lifetime"}, "number of days"},
		{"wot_candidates", map[string]any{"budget_credits": -1}, "negative"},
		{"wot_missions", map[string]any{"open_only": true}, "needs --operation"},
		{"wot_moe", map[string]any{"tank": ""}, "name or tank_id"},
		{"wot_brief", map[string]any{"max_chars": 100}, "at least 2000"},
		{"wot_sync", map[string]any{"only": "tomato"}, "want wg, wn8 or mod"},
		{"wot_guide", map[string]any{"topic": "crew"}, "unknown topic"},
		// The seeded account has no application_id, so sync names the fix.
		{"wot_sync", nil, "wotctx auth wg"},
	}
	env, _ := seededEnv(t)
	cs := connect(t, env)
	for _, c := range cases {
		got, isErr := call(t, cs, c.tool, c.args)
		if !isErr {
			t.Errorf("%s %v succeeded, want a tool error", c.tool, c.args)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s %v error %q does not mention %q", c.tool, c.args, got, c.want)
		}
	}
}

// TestMCPWithoutCacheNamesSync: as on the CLI, a query never creates the cache,
// and its error says what to run.
func TestMCPWithoutCacheNamesSync(t *testing.T) {
	env, _ := newTestEnv(t)
	cs := connect(t, env)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"wot_brief", nil}, {"wot_resources", nil}, {"wot_garage", nil}, {"wot_tank", map[string]any{"tank": "Leox"}},
		{"wot_performance", nil}, {"wot_sessions", nil}, {"wot_candidates", nil}, {"wot_missions", nil},
		{"wot_moe", map[string]any{"tank": "Leox"}},
	} {
		got, isErr := call(t, cs, c.tool, c.args)
		if !isErr || !strings.Contains(got, "wotctx sync") {
			t.Errorf("%s = %q (isError %v), want an error naming wotctx sync", c.tool, got, isErr)
		}
	}
	if env.HasStore() {
		t.Error("a tool created the cache")
	}
	// Status is not a query: with no cache it reports that, successfully.
	if got, isErr := call(t, cs, "wot_data_status", nil); isErr {
		t.Errorf("wot_data_status with no cache: tool error %s", got)
	}
}

// TestMCPKeepsStdoutForTheProtocol: in mcp mode the command's own stdout is
// the JSON-RPC stream, so runMCP must point env.Stdout elsewhere before any
// tool can print.
func TestMCPKeepsStdoutForTheProtocol(t *testing.T) {
	serverT, clientT := mcp.NewInMemoryTransports()
	orig := mcpTransport
	mcpTransport = func() mcp.Transport { return serverT }
	t.Cleanup(func() { mcpTransport = orig })

	env, _ := seededEnv(t)
	var stdout, stderr bytes.Buffer
	env.Stdout, env.Stderr = &stdout, &stderr

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, env, []string{"mcp"}) }()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	for _, tool := range []string{"wot_data_status", "wot_brief", "wot_sync"} {
		if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool}); err != nil {
			t.Fatalf("CallTool(%s): %v", tool, err)
		}
	}
	cs.Close()
	cancel()
	<-done

	if stdout.Len() > 0 {
		t.Errorf("tools wrote to the protocol stream:\n%s", stdout.String())
	}
	if env.Stdout != &stderr {
		t.Error("env.Stdout was not pointed at stderr")
	}
}

// TestMCPGuideServesTheSkill: a client with no skill reads the same text as
// Claude Code does, from the copy embedded in the binary, so the two cannot
// drift (docs/plan.md, step P1).
func TestMCPGuideServesTheSkill(t *testing.T) {
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join("..", "..", "skills", "wot-advisor", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return string(raw)
	}
	env, _ := newTestEnv(t)
	cs := connect(t, env)

	got, isErr := call(t, cs, "wot_guide", nil)
	if isErr {
		t.Fatalf("wot_guide: tool error: %s", got)
	}
	if !strings.HasPrefix(got, guidePreamble) {
		t.Error("wot_guide does not start with the CLI-to-tool mapping")
	}
	if !strings.Contains(got, read("references/framework.md")) {
		t.Error("wot_guide does not carry references/framework.md byte for byte")
	}
	if !strings.Contains(got, "### 1. Freshness gate") {
		t.Error("wot_guide does not carry SKILL.md's procedure")
	}
	if strings.Contains(got, "allowed-tools:") {
		t.Error("wot_guide carries SKILL.md's frontmatter")
	}

	for _, topic := range guideTopics {
		got, isErr := call(t, cs, "wot_guide", map[string]any{"topic": topic})
		if isErr {
			t.Errorf("wot_guide %s: tool error: %s", topic, got)
			continue
		}
		if want := read("references/" + topic + ".md"); got != want {
			t.Errorf("wot_guide %s differs from references/%s.md", topic, topic)
		}
	}

	if !strings.Contains(cs.InitializeResult().Instructions, "wot_guide") {
		t.Error("server instructions do not say to read wot_guide")
	}
}

// TestBundleManifestListsTheTools: Desktop shows the bundle manifest's tool
// list before the server has run, so it must name exactly the tools served.
func TestBundleManifestListsTheTools(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "packaging", "mcpb", "manifest.json"))
	if err != nil {
		t.Fatalf("reading the bundle manifest: %v", err)
	}
	var manifest struct {
		Tools []struct{ Name string } `json:"tools"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parsing the bundle manifest: %v", err)
	}
	var declared []string
	for _, tool := range manifest.Tools {
		declared = append(declared, tool.Name)
	}

	env, _ := newTestEnv(t)
	res, err := connect(t, env).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var served []string
	for _, tool := range res.Tools {
		served = append(served, tool.Name)
	}

	sort.Strings(declared)
	sort.Strings(served)
	if !reflect.DeepEqual(declared, served) {
		t.Errorf("manifest declares %v\nserver serves      %v", declared, served)
	}
}

// TestMCPGuideIsTheProcedurePlusWotctxGuide pins the relation of
// docs/plan.md step D3: wot_guide's default is the procedure followed by
// exactly what `wotctx guide` prints, so Desktop and the plugin read the same
// settings in the same words.
func TestMCPGuideIsTheProcedurePlusWotctxGuide(t *testing.T) {
	env, buf := newTestEnv(t)
	overlayPath := env.Config.OverlayFile(env.Paths)
	os.WriteFile(overlayPath, []byte("version: 1\nupdated_at: 2026-09-18\nprofile:\n  session_minutes: 90\n"), 0o644)

	if err := Run(context.Background(), env, []string{"guide"}); err != nil {
		t.Fatalf("guide: %v", err)
	}
	cliText := buf.String()
	if !strings.Contains(cliText, "90 minutes (yours)") {
		t.Errorf("wotctx guide does not render the player's setting:\n%s", cliText)
	}
	for _, part := range []string{"# Core rules", "## Your advice", "# WoT analysis framework"} {
		if !strings.Contains(cliText, part) {
			t.Errorf("wotctx guide lacks %q", part)
		}
	}
	if strings.Index(cliText, "# Core rules") > strings.Index(cliText, "## Your advice") ||
		strings.Index(cliText, "## Your advice") > strings.Index(cliText, "# WoT analysis framework") {
		t.Error("the guide is not in the order of precedence: core, the player's settings, the framework")
	}

	got, isErr := call(t, connect(t, env), "wot_guide", nil)
	if isErr {
		t.Fatalf("wot_guide: %s", got)
	}
	if want := guidePreamble + skills.Body() + guideRule + cliText; got != want {
		t.Error("wot_guide is not the procedure followed by wotctx guide, byte for byte")
	}
}

// TestGuideSurvivesABrokenOverlay: without the guide the model has no rules,
// so an overlay that cannot be read leaves the defaults and says why.
func TestGuideSurvivesABrokenOverlay(t *testing.T) {
	env, buf := newTestEnv(t)
	os.WriteFile(env.Config.OverlayFile(env.Paths), []byte("version: [\n"), 0o644)
	if err := Run(context.Background(), env, []string{"guide"}); err != nil {
		t.Fatalf("guide: %v", err)
	}
	if !strings.Contains(buf.String(), "could not be read") || !strings.Contains(buf.String(), "## Your advice") {
		t.Errorf("guide with a broken overlay:\n%s", buf.String())
	}
}
