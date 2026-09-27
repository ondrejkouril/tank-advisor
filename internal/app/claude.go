package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const claudeDownloadURL = "https://claude.ai/download"

// ClaudeLocator finds Claude Desktop and Claude Code on this computer, from
// the folders Windows gives each user. Plan step D1 recorded where each
// keeps what.
type ClaudeLocator struct {
	LocalAppData string // %LOCALAPPDATA%: Claude Desktop's program
	AppData      string // %APPDATA%: Claude Desktop's settings
	Home         string // the user's home: Claude Code's settings
}

// DefaultClaudeLocator reads this user's folders.
func DefaultClaudeLocator() ClaudeLocator {
	home, _ := os.UserHomeDir()
	return ClaudeLocator{LocalAppData: os.Getenv("LOCALAPPDATA"), AppData: os.Getenv("APPDATA"), Home: home}
}

// ClaudeState is what is installed.
type ClaudeState struct {
	Desktop bool
	// Extension is the installed Tank Advisor extension's version, or "".
	Extension string
	// ExtensionKnown is false when Desktop's record could not be read. Its
	// format is undocumented, so an unreadable one means "unknown", not
	// "missing".
	ExtensionKnown bool
	Plugin         bool
	PluginKnown    bool
}

// desktopExe is Claude Desktop's Squirrel stub, whose path survives Claude's
// own updates.
func (l ClaudeLocator) desktopExe() string {
	if l.LocalAppData == "" {
		return ""
	}
	return filepath.Join(l.LocalAppData, "AnthropicClaude", "claude.exe")
}

// Detect reports what is installed. It only reads files.
func (l ClaudeLocator) Detect() ClaudeState {
	var st ClaudeState
	if exe := l.desktopExe(); exe != "" {
		if _, err := os.Stat(exe); err == nil {
			st.Desktop = true
		}
	}
	if l.AppData != "" {
		st.Extension, st.ExtensionKnown = installedExtension(filepath.Join(l.AppData, "Claude", "extensions-installations.json"))
	}
	if l.Home != "" {
		st.Plugin, st.PluginKnown = installedPlugin(filepath.Join(l.Home, ".claude", "plugins", "installed_plugins.json"))
	}
	return st
}

// installedExtension reads Claude Desktop's record of installed extensions.
// Ours is the one whose manifest is named wotctx; its id carries the author,
// local.mcpb.<author>.wotctx, which a rebuilt bundle may change. No file at
// all means Desktop has never installed an extension.
func installedExtension(path string) (version string, known bool) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", true
	}
	if err != nil {
		return "", false
	}
	var record struct {
		Extensions map[string]struct {
			Version  string `json:"version"`
			Manifest struct {
				Name string `json:"name"`
			} `json:"manifest"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return "", false
	}
	for id, ext := range record.Extensions {
		if ext.Manifest.Name == "wotctx" || strings.HasSuffix(id, ".wotctx") {
			return ext.Version, true
		}
	}
	return "", true
}

// installedPlugin reads Claude Code's list of installed plugins for wot@….
func installedPlugin(path string) (installed, known bool) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	var record struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return false, false
	}
	for name := range record.Plugins {
		if strings.HasPrefix(name, "wot@") {
			return true, true
		}
	}
	return false, true
}

// Install hands the bundle to Claude Desktop, which asks the player to
// confirm. Claude Desktop does not claim .mcpb files, so opening the file
// through the shell would only ask which program to use; running claude.exe
// with the file brings up its install dialog (plan step D1).
func (l ClaudeLocator) Install(bundle string) error {
	exe := l.desktopExe()
	if exe == "" {
		return errors.New("Claude Desktop was not found")
	}
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	cmd := exec.Command(exe, bundle)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// ClaudeCode installs the Claude Code plugin through the claude command
// (docs/spec-desktop.md section 7.2).
type ClaudeCode interface {
	Available() bool
	InstallPlugin(ctx context.Context) (output string, err error)
}

// ClaudeCLI is the claude command on the PATH.
type ClaudeCLI struct{}

// Available reports whether the claude command is on the PATH.
func (ClaudeCLI) Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// InstallPlugin adds this repository as a plugin marketplace and installs the
// wot plugin from it. Claude Code's marketplace keeps it current from then on.
func (ClaudeCLI) InstallPlugin(ctx context.Context) (string, error) {
	var out strings.Builder
	for _, args := range [][]string{
		{"plugin", "marketplace", "add", repository},
		{"plugin", "install", "wot@tank-advisor"},
	} {
		cmd := exec.CommandContext(ctx, "claude", args...)
		hideWindow(cmd)
		b, err := cmd.CombinedOutput()
		out.Write(b)
		// Adding a marketplace that is already there is not a failure.
		if err != nil && !strings.Contains(strings.ToLower(string(b)), "already") {
			return out.String(), fmt.Errorf("claude %s: %w", strings.Join(args, " "), err)
		}
	}
	return out.String(), nil
}

func (s *Service) installPlugin(ctx context.Context) Result {
	if !s.opts.ClaudeCode.Available() {
		return Result{Message: "Claude Code's claude command was not found."}
	}
	out, err := s.opts.ClaudeCode.InstallPlugin(ctx)
	if err != nil {
		return Result{Message: err.Error(), Output: strings.TrimSpace(out)}
	}
	return Result{OK: true, Message: "The wot plugin is installed. Start a new Claude Code session to use it.", Output: strings.TrimSpace(out)}
}
