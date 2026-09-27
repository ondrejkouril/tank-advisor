package config

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// ownerHashes are the SHA-256 of the owner's nickname and account id, the
// only personal identifiers the project ever held. The test keeps the hashes,
// not the words, so it can look for them without naming them.
var ownerHashes = map[string]bool{
	"2a424fede2a30456a8e3f18e13caf52b08ba529f6e931b13eddf9ba6cd1bedba": true,
	"78f3d76cab6bc7274a6bbdf9e337e4613eb934611c11c8080cba3066083ff2a5": true,
}

// TestNothingNamesTheOwner holds the rule of docs/spec-desktop.md section 8.1,
// widened before the repository was published: nothing in it, code, tests,
// recorded fixtures or docs, names the owner's account. The fixtures were
// recorded from that account and then scrubbed to a placeholder.
func TestNothingNamesTheOwner(t *testing.T) {
	root := filepath.Join("..", "..")
	skipDirs := map[string]bool{".git": true, "dist": true, "node_modules": true, "payload": true}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".pyc", ".wotmod", ".mcpb", ".png", ".ico", ".db":
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, word := range strings.FieldsFunc(strings.ToLower(string(raw)), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
		}) {
			sum := sha256.Sum256([]byte(word))
			if ownerHashes[hex.EncodeToString(sum[:])] {
				t.Errorf("%s names the owner's account", path)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
