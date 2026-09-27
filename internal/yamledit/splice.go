package yamledit

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Apply returns raw with the updates made, changing only the lines they touch.
//
// Re-encoding a file through yaml.v3 keeps the comments' words but not their
// layout: it drops the blank lines between sections, collapses aligned
// comment columns and moves a comment's continuation lines. For a file people
// format by hand, that is a rewrite. So Apply splices text instead. yaml.v3
// finds where each key and value is; a changed scalar or inline list is
// rewritten on its own line, with its comment kept at the same column; a
// mapping is edited key by key; a list is rebuilt from its old items' own
// text, so unchanged items (and the comments above and inside them) are
// copied as they are, and a changed item is edited in place. Every other byte
// is left alone, and an update that changes nothing changes nothing.
func Apply(raw []byte, updates ...Update) ([]byte, error) {
	for _, u := range updates {
		var err error
		if raw, err = applyOne(raw, splitKey(u.Key), u.Value); err != nil {
			return nil, fmt.Errorf("setting %s: %w", u.Key, err)
		}
	}
	return raw, nil
}

func splitKey(key string) []string { return strings.Split(key, ".") }

// text is a file as lines, remembering how it ended its lines.
type text struct {
	lines    []string
	crlf     bool
	finalEOL bool
}

func split(raw []byte) text {
	s := string(raw)
	t := text{crlf: strings.Contains(s, "\r\n"), finalEOL: strings.HasSuffix(s, "\n")}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s != "" || t.finalEOL {
		t.lines = strings.Split(s, "\n")
	}
	return t
}

func (t text) join() []byte {
	eol := "\n"
	if t.crlf {
		eol = "\r\n"
	}
	out := strings.Join(t.lines, eol)
	if t.finalEOL || len(t.lines) > 0 {
		out += eol
	}
	return []byte(out)
}

// replace swaps lines [from, to] (inclusive) for with.
func (t *text) replace(from, to int, with []string) {
	lines := slices.Clone(t.lines[:from])
	lines = append(lines, with...)
	t.lines = append(lines, t.lines[to+1:]...)
}

func (t *text) insert(at int, with []string) {
	lines := slices.Clone(t.lines[:at])
	lines = append(lines, with...)
	t.lines = append(lines, t.lines[at:]...)
}

func parse(raw []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 || isEmpty(doc.Content[0]) {
		return nil, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the top level is not a mapping")
	}
	return doc.Content[0], nil
}

func applyOne(raw []byte, path []string, value any) ([]byte, error) {
	root, err := parse(raw)
	if err != nil {
		return nil, err
	}
	if root == nil || root.Style&yaml.FlowStyle != 0 {
		// Nothing hand-written to keep: encode the lot.
		return applyNodes(raw, Update{Key: strings.Join(path, "."), Value: value})
	}
	t := split(raw)
	if err := setIn(&t, root, 0, path, value); err != nil {
		return nil, err
	}
	return t.join(), nil
}

// setIn sets path below the block mapping m, whose keys sit at indent.
func setIn(t *text, m *yaml.Node, indent int, path []string, value any) error {
	for i := 0; i < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Value != path[0] {
			continue
		}
		if len(path) > 1 {
			if v.Kind == yaml.MappingNode && v.Style&yaml.FlowStyle == 0 && len(v.Content) > 0 {
				return setIn(t, v, v.Content[0].Column-1, path[1:], value)
			}
			// A scalar, an empty or inline mapping: set the whole value.
			var old any
			_ = v.Decode(&old)
			nested, _ := old.(map[string]any)
			if nested == nil {
				nested = map[string]any{}
			}
			if err := setNested(nested, path[1:], value); err != nil {
				return err
			}
			if value == nil && len(nested) == 0 {
				return replaceValue(t, k, v, nil)
			}
			return replaceValue(t, k, v, nested)
		}
		return replaceValue(t, k, v, value)
	}
	if value == nil {
		return nil // nothing to remove
	}
	var v any = value
	for i := len(path) - 1; i >= 1; i-- {
		v = map[string]any{path[i]: v}
	}
	return insertKey(t, m, indent, path[0], v)
}

func setNested(m map[string]any, path []string, value any) error {
	if len(path) == 1 {
		if value == nil {
			delete(m, path[0])
		} else {
			m[path[0]] = value
		}
		return nil
	}
	child, _ := m[path[0]].(map[string]any)
	if child == nil {
		if value == nil {
			return nil
		}
		child = map[string]any{}
		m[path[0]] = child
	}
	if err := setNested(child, path[1:], value); err != nil {
		return err
	}
	if len(child) == 0 {
		delete(m, path[0])
	}
	return nil
}

func toNode(value any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(value); err != nil {
		return nil, err
	}
	return &n, nil
}

func decoded(n *yaml.Node) any {
	var v any
	if n != nil {
		_ = n.Decode(&v)
	}
	return v
}

// sameValue reports whether two nodes say the same thing, however they are
// written: quoting, flow or block style and key order do not matter. Tags
// do, with one exception: a string and a timestamp with the same text are
// the same, since a date the file writes unquoted comes back from a caller
// as a string. An integer and a string are not: an unquoted 123 is a tank_id
// in the overlay, and "123" a name.
func sameValue(a, b *yaml.Node) bool {
	a, b = resolveAlias(a), resolveAlias(b)
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return isNull(a) && isNull(b)
	}
	switch a.Kind {
	case yaml.ScalarNode:
		if !compatibleTags(a.ShortTag(), b.ShortTag()) {
			return false
		}
		return a.Value == b.Value || sameInstant(a.Value, b.Value)
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !sameValue(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	case yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := 0; i < len(a.Content); i += 2 {
			other := mapValue(b, a.Content[i].Value)
			if other == nil || !sameValue(a.Content[i+1], other) {
				return false
			}
		}
		return true
	case yaml.DocumentNode:
		return len(a.Content) == len(b.Content) && (len(a.Content) == 0 || sameValue(a.Content[0], b.Content[0]))
	}
	return reflect.DeepEqual(decoded(a), decoded(b))
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// sameInstant reports whether two texts are the same timestamp written two
// ways: a date, and the midnight yaml.v3 writes for it.
func sameInstant(a, b string) bool {
	ta, errA := parseStamp(a)
	tb, errB := parseStamp(b)
	return errA == nil && errB == nil && ta.Equal(tb)
}

func parseStamp(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("not a timestamp")
}

func compatibleTags(a, b string) bool {
	if a == b {
		return true
	}
	text := map[string]bool{"!!str": true, "!!timestamp": true}
	return text[a] && text[b]
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// replaceValue puts value under key k, whose current value is v; nil removes
// the key.
func replaceValue(t *text, k, v *yaml.Node, value any) error {
	keyLine, keyIndent := k.Line-1, k.Column-1
	if value == nil {
		deleteBlock(t, keyLine, keyIndent, isBlockSeqAt(v, keyIndent))
		return nil
	}
	nv, err := toNode(value)
	if err != nil {
		return err
	}
	if sameValue(v, nv) {
		return nil
	}

	block := v.Style&yaml.FlowStyle == 0 && len(v.Content) > 0
	switch {
	case block && v.Kind == yaml.MappingNode && nv.Kind == yaml.MappingNode:
		return mergeMapping(t, k, v, nv)
	case block && v.Kind == yaml.SequenceNode && nv.Kind == yaml.SequenceNode:
		return rebuildSequence(t, v, nv)
	}
	return rewriteValue(t, keyLine, keyIndent, k, v, nv)
}

// mergeMapping edits a block mapping key by key: each edit is made and the
// text parsed again before the next, so positions stay true.
func mergeMapping(t *text, k, v, nv *yaml.Node) error {
	newValues := map[string]*yaml.Node{}
	var order []string
	for i := 0; i < len(nv.Content); i += 2 {
		newValues[nv.Content[i].Value] = nv.Content[i+1]
		order = append(order, nv.Content[i].Value)
	}
	var ops []struct {
		key   string
		value any
	}
	for i := 0; i < len(v.Content); i += 2 {
		if _, keep := newValues[v.Content[i].Value]; !keep {
			ops = append(ops, struct {
				key   string
				value any
			}{v.Content[i].Value, nil})
		}
	}
	for _, key := range order {
		ops = append(ops, struct {
			key   string
			value any
		}{key, decoded(newValues[key])})
	}
	// The path to this mapping, from the root, to re-find it after each edit.
	path := pathTo(t, k)
	for _, op := range ops {
		raw, err := applyOne(t.join(), append(slices.Clone(path), op.key), op.value)
		if err != nil {
			return err
		}
		*t = split(raw)
	}
	return nil
}

// pathTo finds the key path of key node k by walking the text's tree.
func pathTo(t *text, k *yaml.Node) []string {
	root, _ := parse(t.join())
	var walk func(m *yaml.Node, prefix []string) []string
	walk = func(m *yaml.Node, prefix []string) []string {
		for i := 0; i < len(m.Content); i += 2 {
			key, val := m.Content[i], m.Content[i+1]
			p := append(slices.Clone(prefix), key.Value)
			if key.Line == k.Line && key.Column == k.Column {
				return p
			}
			if val.Kind == yaml.MappingNode {
				if found := walk(val, p); found != nil {
					return found
				}
			}
		}
		return nil
	}
	if root == nil {
		return nil
	}
	return walk(root, nil)
}

// rewriteValue replaces a key's line, and the lines of its value, with the
// new value: inline when it fits on the key's line, as a block below it
// otherwise. The key's comment keeps its column, and comment lines inside the
// old value are kept below the new one.
func rewriteValue(t *text, keyLine, keyIndent int, k, v, nv *yaml.Node) error {
	end := blockEnd(t, keyLine, keyIndent, isBlockSeqAt(v, keyIndent))
	content, comment, col := splitComment(t.lines[keyLine])
	prefix := content[:keyEndIndex(content, k)]

	var kept []string
	for _, l := range t.lines[keyLine+1 : end+1] {
		if isComment(l) {
			kept = append(kept, l)
		}
	}

	inline := nv.Kind == yaml.ScalarNode || isFlowable(nv) && (v.Style&yaml.FlowStyle != 0 || v.Kind != nv.Kind || len(v.Content) == 0)
	var out []string
	if inline {
		if nv.Kind != yaml.ScalarNode {
			nv.Style = yaml.FlowStyle
		}
		rendered, err := render(nv)
		if err != nil {
			return err
		}
		out = append(out, withComment(prefix+" "+rendered[0], comment, col))
		for _, l := range rendered[1:] {
			out = append(out, strings.Repeat(" ", keyIndent)+l)
		}
	} else {
		rendered, err := render(nv)
		if err != nil {
			return err
		}
		out = append(out, withComment(prefix, comment, col))
		for _, l := range rendered {
			out = append(out, indentLine(l, keyIndent+2))
		}
	}
	t.replace(keyLine, end, append(out, kept...))
	return nil
}

// isFlowable reports whether a collection reads well on one line: a list or
// mapping of scalars.
func isFlowable(n *yaml.Node) bool {
	if n.Kind != yaml.SequenceNode && n.Kind != yaml.MappingNode {
		return false
	}
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode {
			return false
		}
	}
	return true
}

// keyEndIndex is where "key:" ends on the key's line.
func keyEndIndex(content string, k *yaml.Node) int {
	runes := []rune(content)
	start := k.Column - 1
	for i := start; i < len(runes); i++ {
		if runes[i] == ':' && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])) {
			return len(string(runes[:i+1]))
		}
	}
	return len(content)
}

// rebuildSequence rewrites a block list from its old items' own text:
// an unchanged item keeps its lines and the comments above it, a changed
// mapping item is edited in place, and a new item is rendered.
func rebuildSequence(t *text, v, nv *yaml.Node) error {
	type chunk struct {
		head, body []string // comment lines above the item; the item
		node       *yaml.Node
	}
	var chunks []chunk
	dash := -1
	first, last := -1, -1
	for _, item := range v.Content {
		line := item.Line - 1
		d := strings.IndexRune(t.lines[line], '-')
		if dash < 0 {
			dash = d
		}
		end := blockEnd(t, line, d, false)
		head := line
		for head > 0 && isComment(t.lines[head-1]) && indentOf(t.lines[head-1]) <= d {
			head--
		}
		if first < 0 {
			first = head
		}
		last = end
		chunks = append(chunks, chunk{head: slices.Clone(t.lines[head:line]), body: slices.Clone(t.lines[line : end+1]), node: item})
	}
	// The spacing between the first two items is kept between all of them.
	var gap []string
	if len(v.Content) > 1 {
		secondHead := v.Content[1].Line - 1 - len(chunks[1].head)
		firstEnd := v.Content[0].Line - 1 + len(chunks[0].body) - 1
		gap = slices.Clone(t.lines[firstEnd+1 : secondHead])
	}

	used := make([]bool, len(chunks))
	pick := make([]int, len(nv.Content))
	for j := range pick {
		pick[j] = -1
		for i, c := range chunks {
			if !used[i] && sameValue(c.node, nv.Content[j]) {
				used[i], pick[j] = true, i
				break
			}
		}
	}
	// A changed item takes the next unused old item of its kind, so its
	// comments stay with it.
	for j := range pick {
		if pick[j] >= 0 {
			continue
		}
		for i, c := range chunks {
			if !used[i] && c.node.Kind == nv.Content[j].Kind && c.node.Kind == yaml.MappingNode {
				used[i], pick[j] = true, i
				break
			}
		}
	}

	var out []string
	for j, n := range nv.Content {
		if j > 0 {
			out = append(out, gap...)
		}
		i := pick[j]
		switch {
		case i >= 0 && sameValue(chunks[i].node, n):
			out = append(out, chunks[i].head...)
			out = append(out, chunks[i].body...)
		case i >= 0:
			body, err := editItem(chunks[i].body, dash, decoded(n))
			if err != nil {
				return err
			}
			out = append(out, chunks[i].head...)
			out = append(out, body...)
		default:
			rendered, err := render(&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{n}})
			if err != nil {
				return err
			}
			for _, l := range rendered {
				out = append(out, indentLine(l, dash))
			}
		}
	}
	t.replace(first, last, out)
	return nil
}

// editItem edits one list item's lines as a document of its own: the dash is
// set aside, the item's mapping updated key by key, and the dash put back.
func editItem(body []string, dash int, value any) ([]string, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("a list item changed kind")
	}
	shift := dash + 2
	doc := make([]string, len(body))
	for i, l := range body {
		if i == 0 {
			l = l[:dash] + "  " + l[dash+2:]
		}
		doc[i] = trimIndent(l, shift)
	}
	raw := []byte(strings.Join(doc, "\n") + "\n")
	root, err := parse(raw)
	if err != nil {
		return nil, err
	}
	var updates []Update
	for i := 0; i < len(root.Content); i += 2 {
		if _, keep := m[root.Content[i].Value]; !keep {
			updates = append(updates, Update{Key: root.Content[i].Value})
		}
	}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if nv, keep := m[key]; keep {
			updates = append(updates, Update{Key: key, Value: nv})
		}
	}
	var newKeys []string
	for key := range m {
		if !hasMapKey(root, key) {
			newKeys = append(newKeys, key)
		}
	}
	slices.Sort(newKeys)
	for _, key := range newKeys {
		updates = append(updates, Update{Key: key, Value: m[key]})
	}
	// Keys are item keys, never dotted paths.
	for _, u := range updates {
		if raw, err = applyOne(raw, []string{u.Key}, u.Value); err != nil {
			return nil, err
		}
	}
	lines := split(raw).lines
	for i, l := range lines {
		lines[i] = indentLine(l, shift)
	}
	if len(lines) > 0 {
		lines[0] = lines[0][:dash] + "- " + lines[0][dash+2:]
	}
	return lines, nil
}

func hasMapKey(m *yaml.Node, key string) bool {
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return true
		}
	}
	return false
}

// insertKey adds key: value at the end of the block mapping m.
func insertKey(t *text, m *yaml.Node, indent int, key string, value any) error {
	rendered, err := render(&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, mustNode(value),
	}})
	if err != nil {
		return err
	}
	var out []string
	for _, l := range rendered {
		out = append(out, indentLine(l, indent))
	}
	lastKey := m.Content[len(m.Content)-2]
	lastVal := m.Content[len(m.Content)-1]
	end := blockEnd(t, lastKey.Line-1, lastKey.Column-1, isBlockSeqAt(lastVal, lastKey.Column-1))
	// A new section at the top level gets the blank line the others have.
	if indent == 0 && len(out) > 1 && strings.TrimSpace(t.lines[end]) != "" {
		out = append([]string{""}, out...)
	}
	t.insert(end+1, out)
	return nil
}

func mustNode(value any) *yaml.Node {
	n, err := toNode(value)
	if err != nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
	return n
}

// deleteBlock removes a key's lines, with the comment lines directly above it.
func deleteBlock(t *text, keyLine, keyIndent int, seqAtIndent bool) {
	end := blockEnd(t, keyLine, keyIndent, seqAtIndent)
	start := keyLine
	for start > 0 && isComment(t.lines[start-1]) && indentOf(t.lines[start-1]) == keyIndent {
		start--
	}
	t.replace(start, end, nil)
	// Do not leave two blank lines where one was.
	if start > 0 && start < len(t.lines) && strings.TrimSpace(t.lines[start-1]) == "" && strings.TrimSpace(t.lines[start]) == "" {
		t.replace(start, start, nil)
	}
}

// blockEnd is the last line of the block that starts at line start with the
// given indent: its deeper lines, and deeper comments, but not the blank
// lines after it or a comment at its own level, which belongs to what follows.
// seqAtIndent lets a list written at the key's own indent belong to it.
func blockEnd(t *text, start, indent int, seqAtIndent bool) int {
	last := start
	for i := start + 1; i < len(t.lines); i++ {
		l := t.lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := indentOf(l)
		trimmed := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			if ind > indent {
				last = i
				continue
			}
		case ind > indent, seqAtIndent && ind == indent && strings.HasPrefix(trimmed, "-"):
			last = i
			continue
		}
		break
	}
	return last
}

func isBlockSeqAt(v *yaml.Node, indent int) bool {
	return v != nil && v.Kind == yaml.SequenceNode && v.Style&yaml.FlowStyle == 0 && len(v.Content) > 0 && v.Content[0].Column-3 == indent
}

func indentOf(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

func isComment(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "#") }

func indentLine(l string, n int) string {
	if strings.TrimSpace(l) == "" {
		return ""
	}
	return strings.Repeat(" ", n) + l
}

func trimIndent(l string, n int) string {
	if indentOf(l) >= n {
		return l[n:]
	}
	return strings.TrimLeft(l, " ")
}

// splitComment separates a line's content from its comment, and says at
// which column (in runes) the comment started, or -1.
func splitComment(line string) (content, comment string, col int) {
	runes := []rune(line)
	var quote rune
	for i, r := range runes {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			if i == 0 || unicode.IsSpace(runes[i-1]) || runes[i-1] == ':' || runes[i-1] == '[' || runes[i-1] == ',' || runes[i-1] == '-' {
				quote = r
			}
		case r == '#' && (i == 0 || unicode.IsSpace(runes[i-1])):
			return strings.TrimRight(string(runes[:i]), " \t"), string(runes[i:]), i
		}
	}
	return strings.TrimRight(line, " \t"), "", -1
}

// withComment appends comment to content, at column col when the content
// leaves room, else one space after it.
func withComment(content, comment string, col int) string {
	if comment == "" {
		return content
	}
	width := len([]rune(content))
	pad := 1
	if col > width {
		pad = col - width
	}
	return content + strings.Repeat(" ", pad) + comment
}

// render writes a node as YAML lines, indented two spaces per level.
func render(n *yaml.Node) ([]string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n"), nil
}
