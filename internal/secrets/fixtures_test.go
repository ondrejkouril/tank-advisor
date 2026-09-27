package secrets_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// credentialShapes are the patterns a real credential would match. They
// deliberately mirror the redactor's own patterns: anything the redactor would
// scrub is something that must never have been committed in the first place.
var credentialShapes = []struct {
	name string
	re   *regexp.Regexp
}{
	{"tomato.gg key", regexp.MustCompile(`tmgg_[A-Za-z0-9_\-]{8,}`)},
	{"long hex credential", regexp.MustCompile(`\b[0-9a-f]{32,}\b`)},
	{"application_id parameter", regexp.MustCompile(`(?i)application_id=[A-Za-z0-9]{16,}`)},
	{"access_token parameter", regexp.MustCompile(`(?i)access_token=[A-Za-z0-9]{16,}`)},
}

// TestFixturesCarryNoCredentials enforces the rule in docs/spec.md section 11:
// recorded fixtures are committed to git, so a real key captured while
// recording one would be published. Scrubbing is a manual step, and this is
// what makes forgetting it fail loudly.
func TestFixturesCarryNoCredentials(t *testing.T) {
	root := filepath.Join("..", "..", "testdata")

	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skip("no testdata directory yet")
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(raw)

		for _, shape := range credentialShapes {
			if match := shape.re.FindString(content); match != "" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("testdata/%s looks like it contains a %s: %q\n"+
					"scrub the fixture before committing it",
					filepath.ToSlash(rel), shape.name, truncate(match))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking testdata: %v", err)
	}
}

// TestFixtureScrubberCatchesARealLookingKey checks that the test above would
// actually fire, rather than passing because its patterns never match anything.
func TestFixtureScrubberCatchesARealLookingKey(t *testing.T) {
	samples := map[string]string{
		"tomato.gg key":            `{"key":"tmgg_9zXq-pLm3_R7tKd2VbN8"}`,
		"long hex credential":      `{"application_id":"deadbeefcafebabe0123456789abcdef"}`,
		"application_id parameter": `https://api.worldoftanks.eu/wot/?application_id=abcdef0123456789abcd`,
		"access_token parameter":   `https://api.worldoftanks.eu/wot/?access_token=abcdef0123456789abcd`,
	}

	for name, sample := range samples {
		var matched bool
		for _, shape := range credentialShapes {
			if shape.re.MatchString(sample) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("no credential shape matches a %s; the scrubber test would miss it", name)
		}
	}
}

func truncate(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + strings.Repeat(".", 3)
}
