// Package yamledit changes keys in a YAML file people also edit by hand,
// keeping every comment and the order of the keys it does not touch. It serves
// config.yaml and wot-overlay.yaml alike (docs/spec-desktop.md sections 5.5
// and 10).
package yamledit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Update sets one key. Key is dotted ("account.account_id"); a nil Value
// removes the key.
type Update struct {
	Key   string
	Value any
}

// Set applies updates to the YAML file at path and writes it back. A missing
// file is created. check, when given, inspects the result as a file before it
// replaces the original; an error from it means nothing is written. Only the
// lines the updates touch change (see Apply).
func Set(path string, check func(tmp string) error, updates ...Update) error {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	out, err := Apply(raw, updates...)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return writeAtomically(path, out, check)
}

// Write replaces the file at path with data, atomically, after check accepts
// it: for callers that prepare the bytes with Apply first.
func Write(path string, data []byte, check func(tmp string) error) error {
	return writeAtomically(path, data, check)
}

// applyNodes encodes the whole document through yaml.v3. It serves a file
// with nothing hand-written to keep: new, empty, or only comments.
func applyNodes(raw []byte, updates ...Update) ([]byte, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		// A file of only comments parses as an empty document.
		if len(doc.Content) == 0 || isEmpty(doc.Content[0]) {
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: doc.HeadComment}}
			doc.HeadComment = ""
		} else {
			return nil, errors.New("the top level is not a mapping")
		}
	}

	for _, u := range updates {
		if err := setKey(doc.Content[0], strings.Split(u.Key, "."), u.Value); err != nil {
			return nil, fmt.Errorf("setting %s: %w", u.Key, err)
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isEmpty(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Value == "" || n.Kind == yaml.MappingNode && len(n.Content) == 0
}

func setKey(m *yaml.Node, path []string, value any) error {
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value != path[0] {
			continue
		}
		if len(path) > 1 {
			child := m.Content[i+1]
			if child.Kind != yaml.MappingNode {
				if value == nil {
					return nil
				}
				child.Kind, child.Tag, child.Value, child.Content = yaml.MappingNode, "!!map", "", nil
			}
			return setKey(child, path[1:], value)
		}
		if value == nil {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return nil
		}
		var v yaml.Node
		if err := v.Encode(value); err != nil {
			return err
		}
		// Keep the comment written beside the old value.
		v.LineComment = m.Content[i+1].LineComment
		*m.Content[i+1] = v
		return nil
	}
	if value == nil {
		return nil // nothing to remove
	}

	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[0]}
	if len(path) > 1 {
		child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		m.Content = append(m.Content, key, child)
		return setKey(child, path[1:], value)
	}
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return err
	}
	m.Content = append(m.Content, key, &v)
	return nil
}

// writeAtomically writes data beside path, lets check inspect the temporary
// file, and only then renames it into place, so a reader never sees half a
// file and a rejected result never replaces a good one.
func writeAtomically(path string, data []byte, check func(tmp string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if check != nil {
		if err := check(name); err != nil {
			return err
		}
	}
	return os.Rename(name, path)
}
