// Package game knows the few things wotctx needs about the installed World of
// Tanks client: its version, and where a mod for that version goes
// (docs/plan.md, phase 4).
//
// A game update installs into a new mods/<version>/ folder and leaves the old
// one behind, so every mod has to be copied again after each update. Reading
// the version from the client is what lets wotctx say when that is due.
package game

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ModID is the client mod's technical id, the prefix of its package name.
const ModID = "ondrejkouril.wotctx"

// versionFile is the client's own record of its build.
type versionFile struct {
	Version string `xml:"version"`
}

// numeric picks "2.4.0.1" out of " v.2.4.0.1 #952 ".
var numeric = regexp.MustCompile(`\d+(?:\.\d+)+`)

// Version reads the installed client's version, as the name of its mods
// folder spells it: "2.4.0.1".
func Version(gameDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(gameDir, "version.xml"))
	if err != nil {
		return "", fmt.Errorf("reading the game version: %w (is game_dir right?)", err)
	}
	var v versionFile
	if err := xml.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("parsing %s: %w", filepath.Join(gameDir, "version.xml"), err)
	}
	found := numeric.FindString(v.Version)
	if found == "" {
		return "", fmt.Errorf("no version number in %q", strings.TrimSpace(v.Version))
	}
	return found, nil
}

// pathsFile is the client's list of resource folders; the one masked
// "*.wotmod" is where it loads mods from.
type pathsFile struct {
	Paths []struct {
		Mask  string `xml:"mask,attr"`
		Value string `xml:",chardata"`
	} `xml:"Paths>Path"`
}

// ModsDir is where mods for the installed version are loaded from: the folder
// paths.xml names, which is authoritative. Deriving it from version.xml is the
// fallback; the two agree for a production client, but the Common Test's
// folder is "mods/2.4.1.0 Common Test" (docs/plan.md step D1).
func ModsDir(gameDir string) (string, error) {
	if raw, err := os.ReadFile(filepath.Join(gameDir, "paths.xml")); err == nil {
		var p pathsFile
		if xml.Unmarshal(raw, &p) == nil {
			for _, path := range p.Paths {
				if path.Mask == "*.wotmod" {
					rel := strings.TrimPrefix(strings.TrimSpace(path.Value), "./")
					return filepath.Join(gameDir, filepath.FromSlash(rel)), nil
				}
			}
		}
	}
	version, err := Version(gameDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(gameDir, "mods", version), nil
}

// InstalledPackages lists this mod's packages in dir, by file name.
func InstalledPackages(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, ModID+"_*.wotmod"))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = filepath.Base(m)
	}
	return names, nil
}
