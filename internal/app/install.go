package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/game"
)

// The app carries everything else it installs (docs/spec-desktop.md section
// 9): wotctx.exe, the launcher, the Claude Desktop bundle and the client mod.
// One signed executable is then the whole update. At start, and when the
// installer runs it with --install-payloads, it writes out each payload that
// differs from the copy on disk.

// payloadPatterns are the versioned payloads: a newer one replaces the old
// file of the same kind rather than sitting beside it.
var payloadPatterns = []string{"wotctx-*.mcpb", game.ModID + "_*.wotmod"}

// InstallPayloads writes the payloads in fsys into dir, skipping any whose
// contents are already there. A running executable (Claude Desktop holds
// wotctx.exe open) cannot be overwritten, but it can be renamed: it becomes
// name.old.exe, the new one takes its place, and the old copy goes at a later
// start (CleanOldPayloads). It returns the names written.
func InstallPayloads(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	present := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.EqualFold(name, "README.md") {
			continue
		}
		present[name] = true
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return written, err
		}
		target := filepath.Join(dir, name)
		if same, _ := sameContents(target, data); same {
			continue
		}
		if err := replaceFile(target, data); err != nil {
			return written, fmt.Errorf("writing %s: %w", name, err)
		}
		written = append(written, name)
	}
	// An older version of a versioned payload goes once its successor is in.
	for _, pattern := range payloadPatterns {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		newest := false
		for _, m := range matches {
			newest = newest || present[filepath.Base(m)]
		}
		for _, m := range matches {
			if newest && !present[filepath.Base(m)] {
				os.Remove(m)
			}
		}
	}
	return written, nil
}

func sameContents(path string, data []byte) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	want := sha256.Sum256(data)
	return bytes.Equal(h.Sum(nil), want[:]), nil
}

// replaceFile writes data beside path and swaps it in; a busy executable is
// renamed out of the way first.
func replaceFile(path string, data []byte) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	old := strings.TrimSuffix(path, filepath.Ext(path)) + ".old" + filepath.Ext(path)
	os.Remove(old)
	if err := os.Rename(path, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// CleanOldPayloads removes the executables a previous update renamed aside.
// One still running stays until a later start.
func CleanOldPayloads(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.old.exe"))
	for _, m := range matches {
		os.Remove(m)
	}
}

// UninstallReport says what Uninstall did.
type UninstallReport struct {
	Removed  []string
	Problems []string
}

// Uninstall takes out what the app put outside its own folder: the mod from
// every folder it was placed in, and the window's browser cache. With
// deleteData it also deletes the account's data, as Delete my data does. The
// registry entries are the caller's (cmd/tankadvisor), and the program files
// the installer's.
func (s *Service) Uninstall(ctx context.Context, deleteData bool, localAppData string) UninstallReport {
	var r UninstallReport
	env := s.opts.NewEnv(io.Discard, io.Discard)
	if env.ConfigErr == nil {
		for _, dir := range env.Config.Mod.Placed {
			names, _ := game.InstalledPackages(dir)
			for _, n := range names {
				p := filepath.Join(dir, n)
				if err := os.Remove(p); err != nil {
					r.Problems = append(r.Problems, fmt.Sprintf("%s: %v (is the game running?)", p, err))
				} else {
					r.Removed = append(r.Removed, p)
				}
			}
		}
		if _, err := os.Stat(env.Paths.ConfigFile); err == nil {
			config.Set(env.Paths.ConfigFile, config.Update{Key: "mod"}, config.Update{Key: "setup"}, config.Update{Key: "duties"})
		}
	}
	if localAppData != "" {
		cache := filepath.Join(localAppData, "Tank Advisor")
		if err := os.RemoveAll(cache); err != nil {
			r.Problems = append(r.Problems, fmt.Sprintf("%s: %v", cache, err))
		} else {
			r.Removed = append(r.Removed, cache)
		}
	}
	if deleteData {
		res := s.wotctx(ctx, "", "data", "delete", "--yes")
		if res.OK {
			r.Removed = append(r.Removed, "the cache, the mod's dump and the login")
		} else {
			r.Problems = append(r.Problems, res.Message)
		}
	}
	return r
}
