package game

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Install is one World of Tanks client found on this machine.
type Install struct {
	Dir string `json:"dir"`
	// ID is the client's own name for itself, from game_info.xml:
	// WOT.EU.PRODUCTION for the real EU client, WOT.CT.PRODUCTION for the
	// Common Test.
	ID string `json:"id"`
	// Realm is the account realm the client plays on ("eu", "com", "asia"), or
	// "" for a client that is not a production one.
	Realm string `json:"realm"`
	// Source says how it was found: "config", "game-center", "running",
	// "steam", "default".
	Source string `json:"source"`
}

// Process is a running game client.
type Process struct {
	PID uint32
	Exe string
}

// Locator finds installed clients. Its fields are the places it looks, so a
// test can point them at a temporary tree.
type Locator struct {
	// GameCenterPrefs is Game Center's preferences.xml, which lists every
	// install it manages (measured in docs/plan.md step D1).
	GameCenterPrefs string
	// SteamRoots are Steam installs whose steamapps/libraryfolders.vdf lists
	// the libraries to search.
	SteamRoots []string
	// Running lists the executables of running game clients. A client
	// installed somewhere unusual is found this way while it runs.
	Running func() []string
	// Defaults are glob patterns tried last.
	Defaults []string
}

// DefaultLocator looks where Game Center and Steam keep things on Windows.
func DefaultLocator() Locator {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return Locator{
		GameCenterPrefs: filepath.Join(programData, "Wargaming.net", "GameCenter", "preferences.xml"),
		SteamRoots:      []string{`C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`},
		Running:         runningClients,
		Defaults:        []string{`C:\Games\World_of_Tanks*`},
	}
}

// Find lists the production clients for realm, best candidate first. Test
// clients are left out: Game Center lists the Common Test beside the real
// client, and the mod belongs in the one the account plays on.
func (l Locator) Find(realm string) []Install {
	var found []Install
	seen := map[string]bool{}
	add := func(dir, source string) {
		key := strings.ToLower(filepath.Clean(dir))
		if dir == "" || seen[key] {
			return
		}
		seen[key] = true
		id, err := ReadID(dir)
		if err != nil {
			return
		}
		inst := Install{Dir: dir, ID: id, Realm: RealmOfID(id), Source: source}
		if inst.Realm != "" && (realm == "" || inst.Realm == realm) {
			found = append(found, inst)
		}
	}

	for _, dir := range gameCenterDirs(l.GameCenterPrefs) {
		add(dir, "game-center")
	}
	if l.Running != nil {
		for _, exe := range l.Running() {
			add(ClientDir(exe), "running")
		}
	}
	for _, root := range l.SteamRoots {
		for _, lib := range steamLibraries(root) {
			base := filepath.Join(lib, "steamapps", "common", "World of Tanks")
			add(base, "steam")
			// Community reports put the client in a realm subfolder.
			entries, _ := os.ReadDir(base)
			for _, e := range entries {
				if e.IsDir() {
					add(filepath.Join(base, e.Name()), "steam")
				}
			}
		}
	}
	for _, pattern := range l.Defaults {
		matches, _ := filepath.Glob(pattern)
		sort.Strings(matches)
		for _, m := range matches {
			add(m, "default")
		}
	}
	return found
}

// ClientDir is the game folder of a client executable: WorldOfTanks.exe sits
// in win64 (or win32) below it.
func ClientDir(exe string) string {
	dir := filepath.Dir(exe)
	switch strings.ToLower(filepath.Base(dir)) {
	case "win64", "win32":
		return filepath.Dir(dir)
	}
	return dir
}

// gameCenterPrefs is the part of preferences.xml that lists installs.
type gameCenterPrefs struct {
	Games []struct {
		WorkingDir string `xml:"working_dir"`
	} `xml:"application>games_manager>games>game"`
	Selected string `xml:"application>games_manager>selectedGames>WOT"`
}

// gameCenterDirs returns Game Center's installs, its selected one first.
func gameCenterDirs(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p gameCenterPrefs
	if err := xml.Unmarshal(raw, &p); err != nil {
		return nil
	}
	var dirs []string
	if p.Selected != "" {
		dirs = append(dirs, strings.TrimSpace(p.Selected))
	}
	for _, g := range p.Games {
		dirs = append(dirs, strings.TrimSpace(g.WorkingDir))
	}
	return dirs
}

var vdfPath = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// steamLibraries lists the Steam library folders under a Steam install.
func steamLibraries(root string) []string {
	libs := []string{root}
	raw, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	for _, m := range vdfPath.FindAllStringSubmatch(string(raw), -1) {
		libs = append(libs, strings.ReplaceAll(m[1], `\\`, `\`))
	}
	return libs
}

// gameInfo is the part of game_info.xml that names the client.
type gameInfo struct {
	ID string `xml:"game>id"`
}

// ReadID returns the client's id from its game_info.xml.
func ReadID(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "game_info.xml"))
	if err != nil {
		return "", err
	}
	var g gameInfo
	if err := xml.Unmarshal(raw, &g); err != nil {
		return "", err
	}
	return strings.TrimSpace(g.ID), nil
}

// RealmOfID maps a client id to the account realm it plays on, or "" for a
// test or unknown client. WOT.EU was measured (docs/plan.md step D1); the
// others follow Wargaming's naming and are unverified until a player on those
// realms confirms them.
func RealmOfID(id string) string {
	parts := strings.Split(strings.ToUpper(id), ".")
	if len(parts) != 3 || parts[0] != "WOT" || parts[2] != "PRODUCTION" {
		return ""
	}
	switch parts[1] {
	case "EU":
		return "eu"
	case "NA", "US", "COM":
		return "com"
	case "ASIA", "SG":
		return "asia"
	}
	return ""
}
