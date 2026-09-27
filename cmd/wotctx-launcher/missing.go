package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// missingReport has the shape of wot_data_status's usual result, one failing
// check, so a model reading it acts as it would on any failed check: stop and
// tell the player the fix.
type missingReport struct {
	Version string         `json:"version"`
	Checks  []missingCheck `json:"checks"`
}

type missingCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// serveMissing is the launcher's answer when Tank Advisor is not installed:
// an MCP server with one tool, wot_data_status, that says so and what to do.
func serveMissing(ctx context.Context, transport mcp.Transport, tried []string) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "wotctx", Title: "Tank Advisor data", Version: version},
		&mcp.ServerOptions{Instructions: "The Tank Advisor app is not installed on this computer, so no World of Tanks data is available. Call wot_data_status and tell the player its fix."})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wot_data_status",
		Description: "Health of the World of Tanks data. Right now it reports that the Tank Advisor app is missing, and how to fix that.",
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		raw, err := json.Marshal(missingReport{Version: version, Checks: []missingCheck{{
			Name:   "app",
			Status: "fail",
			Detail: "the Tank Advisor app is not installed, or its wotctx.exe is missing; looked in: " + strings.Join(tried, "; "),
			Fix:    "install (or repair) Tank Advisor from https://github.com/ondrejkouril/tank-advisor/releases, then restart Claude Desktop",
		}}})
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
	})
	return server.Run(ctx, transport)
}
