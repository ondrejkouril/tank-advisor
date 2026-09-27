package secrets

import (
	"regexp"
	"strings"
)

// Placeholder replaces a redacted value.
const Placeholder = "[redacted]"

// Patterns that look like credentials regardless of whether wotctx ever loaded
// the value. This is the safety net: exact-value redaction only catches secrets
// the process knows about, but a leak can just as easily come from an error
// message quoting a request URL.
var patterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Either credential as a query parameter, whatever shape the value has.
	// The parameter name is kept so a redacted URL is still diagnosable.
	{regexp.MustCompile(`(?i)\b(application_id|access_token)=[^&\s"']+`), "$1=" + Placeholder},
	// Tomato.gg keys are prefixed, which makes them unmistakable.
	{regexp.MustCompile(`tmgg_[A-Za-z0-9_\-]+`), Placeholder},
	// Wargaming application_id and access_token are long lowercase hex runs.
	// The 32-character floor keeps ordinary identifiers, git short shas and
	// tank ids out of the way.
	{regexp.MustCompile(`\b[0-9a-f]{32,}\b`), Placeholder},
}

// Redactor removes known secret values and credential-shaped substrings from
// text on its way to a log, an error or the terminal.
//
// A Redactor is safe for concurrent use once built.
type Redactor struct {
	values []string
}

// NewRedactor returns a Redactor that also removes the exact values given.
// Empty and very short values are ignored: redacting a one-character string
// would mangle unrelated output for no security benefit.
func NewRedactor(values ...string) *Redactor {
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) >= 8 {
			kept = append(kept, v)
		}
	}
	return &Redactor{values: kept}
}

// FromStore builds a Redactor preloaded with whatever credentials are currently
// available, so their exact values can be scrubbed even when they appear in an
// unexpected format. extra adds values held elsewhere, such as the application
// id compiled into a release build.
func FromStore(s Store, extra ...string) *Redactor {
	values := append([]string(nil), extra...)
	for _, k := range All() {
		if k == WGTokenExpiry {
			continue // a timestamp, not a secret
		}
		if v, _, err := s.Get(k); err == nil {
			values = append(values, v)
		}
	}
	return NewRedactor(values...)
}

// Redact returns text with every known secret and credential-shaped substring
// replaced by Placeholder.
func (r *Redactor) Redact(text string) string {
	if text == "" {
		return text
	}
	out := text
	for _, v := range r.values {
		out = strings.ReplaceAll(out, v, Placeholder)
	}
	for _, p := range patterns {
		out = p.re.ReplaceAllString(out, p.repl)
	}
	return out
}

// RedactError returns err's message with secrets removed. It returns "" for a
// nil error so callers can use it inline.
func (r *Redactor) RedactError(err error) string {
	if err == nil {
		return ""
	}
	return r.Redact(err.Error())
}
