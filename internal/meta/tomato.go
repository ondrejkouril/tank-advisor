package meta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TomatoBase is tomato.gg's site root. Its pages are server-rendered with
// their data embedded in the HTML, which is what is read here: the public
// pages need no key, unlike api.tomato.gg (docs/spec.md section 3.2), which
// nothing in wotctx calls.
const TomatoBase = "https://tomato.gg"

// Server maps a wotctx realm to tomato.gg's server segment.
func Server(realm string) string {
	switch realm {
	case "com":
		return "na"
	default:
		return realm
	}
}

// Tomato reads tomato.gg's public pages.
type Tomato struct {
	Fetch *Fetcher
	// Base overrides TomatoBase, for tests.
	Base string
}

func (t *Tomato) url(path string) string {
	base := t.Base
	if base == "" {
		base = TomatoBase
	}
	return strings.TrimSuffix(base, "/") + path
}

// TomatoPage is one page's embedded records with their provenance.
type TomatoPage struct {
	URL       string
	FetchedAt time.Time
	// Updated is the page's own timestamp: when tomato.gg last recomputed it.
	Updated time.Time
	Records []Record
	// Body is the page itself, for a caller that reads a second record type.
	Body []byte
}

// page fetches path and extracts the records that open with lead, the first
// key of the record type wanted ("tank_id", "65").
func (t *Tomato) page(ctx context.Context, path, lead string) (TomatoPage, error) {
	f := t.Fetch
	if f == nil {
		f = &Fetcher{}
	}
	p, err := f.Get(ctx, t.url(path))
	if err != nil {
		return TomatoPage{}, err
	}
	updated, records, err := ParseTomato(p.Body, lead)
	if err != nil {
		return TomatoPage{}, fmt.Errorf("%s: %w", p.URL, err)
	}
	return TomatoPage{URL: p.URL, FetchedAt: p.FetchedAt, Updated: updated, Records: records, Body: p.Body}, nil
}

// ErrFormat marks a page whose embedded data is not where, or what, it was
// measured to be. Nothing from such a page is served.
var ErrFormat = errors.New("its format has changed")

var updatedRe = regexp.MustCompile(`updated:"([^"]+)"`)

// updatedLayouts are the two stamp formats seen (2026-10-09): the MoE and
// mastery pages write a Postgres timestamp, the others ISO 8601.
var updatedLayouts = []string{
	"2006-01-02 15:04:05.999999-07:00",
	time.RFC3339Nano,
}

// ParseTomato extracts a page's update stamp and every record that opens
// with lead.
//
// This reads a site's internal page data, not an API, so it can break with any
// redesign. It fails loudly - no stamp, no records, or a record it cannot read
// - rather than return a partial table that would be quoted as if complete.
func ParseTomato(page []byte, lead string) (time.Time, []Record, error) {
	var updated time.Time
	for _, m := range updatedRe.FindAllSubmatch(page, -1) {
		for _, layout := range updatedLayouts {
			if t, err := time.Parse(layout, string(m[1])); err == nil {
				updated = t.UTC()
				break
			}
		}
		if !updated.IsZero() {
			break
		}
	}
	if updated.IsZero() {
		return updated, nil, fmt.Errorf("tomato.gg: no update timestamp in the page; %w", ErrFormat)
	}

	marker := []byte("{" + lead + ":")
	var records []Record
	for at := 0; ; {
		i := bytes.Index(page[at:], marker)
		if i < 0 {
			break
		}
		p := &literal{s: page, i: at + i}
		v, err := p.value()
		if err != nil {
			return updated, nil, fmt.Errorf("tomato.gg: record at byte %d: %v; %w", at+i, err, ErrFormat)
		}
		obj, _ := v.(map[string]any)
		records = append(records, Record(obj))
		at = p.i
	}
	if len(records) == 0 {
		return updated, nil, fmt.Errorf("tomato.gg: no records in the page; %w", ErrFormat)
	}
	return updated, records, nil
}

// Record is one object from a page's embedded data. Numbers are float64,
// as in JSON.
type Record map[string]any

// Float returns a numeric field.
func (r Record) Float(key string) (float64, bool) {
	f, ok := r[key].(float64)
	return f, ok
}

// Int returns a numeric field rounded to an integer.
func (r Record) Int(key string) (int, bool) {
	f, ok := r.Float(key)
	return int(math.Round(f)), ok
}

// String returns a string field, or "".
func (r Record) String(key string) string {
	s, _ := r[key].(string)
	return s
}

// Bool returns a boolean field.
func (r Record) Bool(key string) (bool, bool) {
	b, ok := r[key].(bool)
	return b, ok
}

// literal parses the JavaScript literal subset tomato.gg's pages embed
// (seroval output, measured 2026-10-09): objects with bare or quoted keys,
// arrays, numbers, strings, !0 and !1 for true and false, null, void 0, and
// $R[n] references, which are either an assignment ($R[n]={...}, read as its
// value) or a back-reference to an earlier value (read as nil: no field read
// here is shared).
type literal struct {
	s []byte
	i int
}

func (p *literal) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *literal) skipSpace() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\n' || p.s[p.i] == '\t' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *literal) consume(prefix string) bool {
	if bytes.HasPrefix(p.s[p.i:], []byte(prefix)) {
		p.i += len(prefix)
		return true
	}
	return false
}

func (p *literal) value() (any, error) {
	p.skipSpace()
	switch c := p.peek(); {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.str()
	case c == '$':
		return p.ref()
	case c == '-' || c == '.' || (c >= '0' && c <= '9'):
		return p.number()
	case p.consume("!0"), p.consume("true"):
		return true, nil
	case p.consume("!1"), p.consume("false"):
		return false, nil
	case p.consume("null"), p.consume("void 0"), p.consume("undefined"):
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected %q", p.s[p.i:min(p.i+12, len(p.s))])
	}
}

func (p *literal) object() (any, error) {
	p.i++ // {
	obj := map[string]any{}
	for {
		p.skipSpace()
		if p.peek() == '}' {
			p.i++
			return obj, nil
		}
		var key string
		if p.peek() == '"' {
			k, err := p.str()
			if err != nil {
				return nil, err
			}
			key = k.(string)
		} else {
			start := p.i
			for p.i < len(p.s) && isKeyByte(p.s[p.i]) {
				p.i++
			}
			if p.i == start {
				return nil, fmt.Errorf("expected a key at %q", p.s[p.i:min(p.i+12, len(p.s))])
			}
			key = string(p.s[start:p.i])
		}
		p.skipSpace()
		if !p.consume(":") {
			return nil, fmt.Errorf("expected ':' after key %q", key)
		}
		v, err := p.value()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		obj[key] = v
		p.skipSpace()
		if p.peek() == ',' {
			p.i++
		} else if p.peek() != '}' {
			return nil, fmt.Errorf("expected ',' or '}' after %q", key)
		}
	}
}

func (p *literal) array() (any, error) {
	p.i++ // [
	var arr []any
	for {
		p.skipSpace()
		if p.peek() == ']' {
			p.i++
			return arr, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.skipSpace()
		if p.peek() == ',' {
			p.i++
		} else if p.peek() != ']' {
			return nil, fmt.Errorf("expected ',' or ']' in an array")
		}
	}
}

func (p *literal) str() (any, error) {
	start := p.i
	p.i++ // "
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case '\\':
			p.i += 2
		case '"':
			p.i++
			raw := string(p.s[start:p.i])
			if s, err := strconv.Unquote(raw); err == nil {
				return s, nil
			}
			// A JavaScript escape Go does not know (\'); keep the text.
			return strings.ReplaceAll(raw[1:len(raw)-1], `\'`, `'`), nil
		default:
			p.i++
		}
	}
	return nil, errors.New("unterminated string")
}

func (p *literal) ref() (any, error) {
	if !p.consume("$R[") {
		return nil, fmt.Errorf("unexpected %q", p.s[p.i:min(p.i+12, len(p.s))])
	}
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	if !p.consume("]") {
		return nil, errors.New("unterminated $R reference")
	}
	if p.consume("=") {
		return p.value()
	}
	return nil, nil
}

func (p *literal) number() (any, error) {
	start := p.i
	for p.i < len(p.s) && strings.IndexByte("+-.0123456789eE", p.s[p.i]) >= 0 {
		p.i++
	}
	f, err := strconv.ParseFloat(string(p.s[start:p.i]), 64)
	if err != nil {
		return nil, fmt.Errorf("number %q: %w", p.s[start:p.i], err)
	}
	return f, nil
}

func isKeyByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
