package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// appDir is the per-user directory name used under every platform's config and
// data roots.
const appDir = "wotctx"

// Paths holds every filesystem location wotctx uses. Config and data are kept
// apart because they have different backup and sync expectations: the config is
// small and worth keeping, the cache is large and disposable.
type Paths struct {
	ConfigDir  string
	ConfigFile string
	DataDir    string
	DBFile     string
}

// Resolve computes the paths for the current user and platform.
//
// Windows uses %APPDATA% for config and %LOCALAPPDATA% for data, matching the
// roaming/local split Windows expects. Everywhere else follows the XDG base
// directory spec.
//
// WOTCTX_CONFIG_DIR and WOTCTX_DATA_DIR replace the two directories outright.
// They let an evaluation run a whole Claude session against a copy of the data
// without touching the environment Claude itself relies on. The client mod
// still writes under %LOCALAPPDATA%\wotctx\mod, so a moved data directory
// sees only the dump copied into it.
func Resolve() (Paths, error) {
	configDir, dataDir := os.Getenv("WOTCTX_CONFIG_DIR"), os.Getenv("WOTCTX_DATA_DIR")
	if configDir == "" || dataDir == "" {
		configRoot, dataRoot, err := roots()
		if err != nil {
			return Paths{}, err
		}
		if configDir == "" {
			configDir = filepath.Join(configRoot, appDir)
		}
		if dataDir == "" {
			dataDir = filepath.Join(dataRoot, appDir)
		}
	}

	return Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.yaml"),
		DataDir:    dataDir,
		DBFile:     filepath.Join(dataDir, "wotctx.db"),
	}, nil
}

func roots() (configRoot, dataRoot string, err error) {
	if runtime.GOOS == "windows" {
		configRoot = os.Getenv("APPDATA")
		dataRoot = os.Getenv("LOCALAPPDATA")
		if configRoot == "" || dataRoot == "" {
			return "", "", fmt.Errorf("APPDATA and LOCALAPPDATA must both be set")
		}
		return configRoot, dataRoot, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("locating home directory: %w", err)
	}

	configRoot = os.Getenv("XDG_CONFIG_HOME")
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}
	dataRoot = os.Getenv("XDG_DATA_HOME")
	if dataRoot == "" {
		dataRoot = filepath.Join(home, ".local", "share")
	}
	return configRoot, dataRoot, nil
}

// EnsureDataDir creates the data directory if it is missing. Callers that only
// read should not need this; sync and the store do.
func (p Paths) EnsureDataDir() error {
	if p.DataDir == "" {
		return fmt.Errorf("data directory is not resolved")
	}
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", p.DataDir, err)
	}
	return nil
}
