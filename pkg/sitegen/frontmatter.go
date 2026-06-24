package sitegen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	yaml "gopkg.in/yaml.v3"
)

// frontmatter.go provides an ordered, comment-preserving view over YAML
// frontmatter, used by the CMS write path (Source.Save). The render path in
// source.go intentionally stays on the plain map[string]interface{} decode —
// this module is additive and only runs when a source is written back, so
// existing reads and template behavior are untouched.
//
// We use yaml.v3 *yaml.Node rather than a plain map so that a round-trip
// (read file -> change one field -> write file) preserves:
//   - the original order of keys,
//   - comments attached to keys,
//   - the raw scalar representation (e.g. an unquoted date 2026-01-01 stays a
//     string and is not reformatted into an RFC3339 timestamp).

// parseFrontmatterNode decodes raw frontmatter bytes into the root mapping
// node. It returns (nil, nil) when meta is empty/blank. An error is returned
// for malformed YAML or non-mapping frontmatter.
func parseFrontmatterNode(meta []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(meta, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil // empty frontmatter
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("frontmatter is not a mapping")
	}
	return root, nil
}

// setFrontmatterField updates key to value on an ordered mapping node. Existing
// keys keep their position (and their key node's comments); unknown keys are
// appended at the end. A nil root is treated as an empty mapping and a fresh
// node is returned.
func setFrontmatterField(root *yaml.Node, key string, value interface{}) (*yaml.Node, error) {
	valNode := &yaml.Node{}
	if err := valNode.Encode(value); err != nil {
		return nil, err
	}
	return setFrontmatterFieldNode(root, key, valNode)
}

// setFrontmatterFieldNode is setFrontmatterField but takes a pre-built value
// node, so callers that need precise control over nested ordering (e.g. blocks)
// can construct the node themselves.
func setFrontmatterFieldNode(root *yaml.Node, key string, valNode *yaml.Node) (*yaml.Node, error) {
	if root == nil {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("frontmatter is not a mapping")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = valNode
			return root, nil
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	root.Content = append(root.Content, keyNode, valNode)
	return root, nil
}

// blocksToNode builds an ordered YAML sequence node from a list of block maps.
// Within each block "type" is emitted first, then the remaining keys sorted
// alphabetically, so the serialized output is deterministic and diff-friendly
// regardless of Go map iteration order.
func blocksToNode(blocks []map[string]interface{}) (*yaml.Node, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, b := range blocks {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		rest := make([]string, 0, len(b))
		for k := range b {
			if k != "type" {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		ordered := rest
		if _, ok := b["type"]; ok {
			ordered = append([]string{"type"}, rest...)
		}
		for _, k := range ordered {
			vn := &yaml.Node{}
			if err := vn.Encode(b[k]); err != nil {
				return nil, err
			}
			m.Content = append(m.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, vn)
		}
		seq.Content = append(seq.Content, m)
	}
	return seq, nil
}

// SaveBlocks writes the page's "blocks" frontmatter key from a structured list,
// preserving the order and comments of all other frontmatter keys. It is the
// engine hook the CMS block builder uses. Pass a nil body to keep the existing
// body. The "blocks" key keeps its position if it already exists, else it is
// appended.
func (s *Source) SaveBlocks(blocks []map[string]interface{}, body []byte) error {
	raw, err := os.ReadFile(s.Local)
	if err != nil {
		return fmt.Errorf("save blocks: read %s: %w", s.Local, err)
	}
	meta, oldBody, _ := splitFrontmatter(raw)
	root, err := parseFrontmatterNode(meta)
	if err != nil {
		return fmt.Errorf("save blocks %s: %w", s.Local, err)
	}
	seq, err := blocksToNode(blocks)
	if err != nil {
		return fmt.Errorf("save blocks %s: %w", s.Local, err)
	}
	if root, err = setFrontmatterFieldNode(root, "blocks", seq); err != nil {
		return fmt.Errorf("save blocks %s: %w", s.Local, err)
	}
	fm, err := marshalFrontmatterNode(root)
	if err != nil {
		return fmt.Errorf("save blocks %s: %w", s.Local, err)
	}
	if body == nil {
		body = oldBody
	}
	var buf bytes.Buffer
	if len(fm) > 0 {
		buf.WriteString("---\n")
		buf.Write(fm)
		buf.WriteString("---\n")
	}
	buf.Write(body)
	if err := os.WriteFile(s.Local, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("save blocks: write %s: %w", s.Local, err)
	}
	s.content = nil
	s.Err = nil
	return nil
}

// marshalFrontmatterNode renders a mapping node back to YAML bytes with 2-space
// indentation. Returns nil for a nil node.
func marshalFrontmatterNode(root *yaml.Node) ([]byte, error) {
	if root == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return unescapeYAMLUnicode(buf.Bytes()), nil
}

// yamlUnicodeEsc matches the \U######## (non-BMP) and \u#### (BMP) escapes that
// yaml.v3's emitter always produces for non-ASCII runes inside double-quoted
// scalars.
var yamlUnicodeEsc = regexp.MustCompile(`\\U[0-9A-Fa-f]{8}|\\u[0-9A-Fa-f]{4}`)

// unescapeYAMLUnicode rewrites printable non-ASCII unicode escapes back to
// literal UTF-8 so emoji and accented text survive a save instead of turning
// into \U0001F680. go-yaml v3 escapes these regardless of node style, so this
// post-process is the only reliable fix. Control characters (non-printable) are
// left escaped, since emitting them raw would corrupt the YAML.
func unescapeYAMLUnicode(b []byte) []byte {
	return yamlUnicodeEsc.ReplaceAllFunc(b, func(m []byte) []byte {
		cp, err := strconv.ParseInt(string(m[2:]), 16, 32)
		if err != nil {
			return m
		}
		r := rune(cp)
		if r >= 0x80 && utf8.ValidRune(r) && unicode.IsPrint(r) {
			return []byte(string(r))
		}
		return m
	})
}

// splitFrontmatter separates a source file into its raw frontmatter YAML and
// the body that follows. It mirrors the engine's "---" delimiter convention
// but returns a clean body (everything strictly after the closing delimiter
// line), so that reconstructing "---\n"+meta+"---\n"+body reproduces the
// original bytes when the frontmatter is unchanged. ok is false when the file
// has no leading frontmatter block, in which case body == raw.
//
// Only "\n" line endings are handled (matching the existing source files).
func splitFrontmatter(raw []byte) (meta, body []byte, ok bool) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return nil, raw, false
	}
	after := s[len("---\n"):]
	marker := strings.Index(after, "\n---")
	if marker < 0 {
		return nil, raw, false
	}
	meta = []byte(after[:marker+1]) // include the newline that ends the last key
	rest := after[marker+1:]        // begins at the closing "---"
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		body = []byte(rest[nl+1:])
	} else {
		body = []byte{}
	}
	return meta, body, true
}

// FrontmatterField is one ordered key/value pair for CreateSource.
type FrontmatterField struct {
	Key   string
	Value interface{}
}

// CreateSource writes a brand-new source file at path with frontmatter built
// from fields (in the given order) followed by body. It is the engine hook the
// CMS uses to create a new collection entry. It fails if the file already
// exists, and creates parent directories as needed. Frontmatter is emitted with
// the same emoji-safe marshaling as the edit paths.
func CreateSource(path string, fields []FrontmatterField, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create source: mkdir %s: %w", path, err)
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, f := range fields {
		vn := &yaml.Node{}
		if err := vn.Encode(f.Value); err != nil {
			return fmt.Errorf("create source: encode %q: %w", f.Key, err)
		}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.Key}, vn)
	}
	fm, err := marshalFrontmatterNode(root)
	if err != nil {
		return fmt.Errorf("create source %s: %w", path, err)
	}
	var buf bytes.Buffer
	if len(fm) > 0 {
		buf.WriteString("---\n")
		buf.Write(fm)
		buf.WriteString("---\n")
	}
	buf.Write(body)

	// O_EXCL so a concurrent/duplicate create can't clobber an existing entry.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create source %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("create source: write %s: %w", path, err)
	}
	return nil
}

// SplitFrontmatter separates raw source bytes into their frontmatter YAML and
// the body that follows, using the engine's "---" delimiter convention. ok is
// false when there is no leading frontmatter block (in which case body == raw).
// It is the exported entry point used by the CMS server so that reading and the
// engine stay in lock-step on the delimiter format.
func SplitFrontmatter(raw []byte) (meta, body []byte, ok bool) {
	return splitFrontmatter(raw)
}

// sortedKeys returns the keys of m in deterministic (sorted) order so that
// newly-added frontmatter keys are appended reproducibly across saves.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Save writes the source file back with the given frontmatter updates applied
// and the given body. Frontmatter keys present in updates are changed in place
// (preserving their original order and comments); keys absent from updates are
// left untouched; keys in updates but not in the file are appended. Pass a nil
// body to keep the file's existing body unchanged.
//
// This is the engine hook the CMS uses to persist edits. It re-reads the file
// from disk (rather than trusting cached state) so a save reflects any external
// edits, then refreshes the in-memory content/Meta via ReloadContent.
func (s *Source) Save(updates map[string]interface{}, body []byte) error {
	raw, err := os.ReadFile(s.Local)
	if err != nil {
		return fmt.Errorf("save: read %s: %w", s.Local, err)
	}
	meta, oldBody, _ := splitFrontmatter(raw)
	root, err := parseFrontmatterNode(meta)
	if err != nil {
		return fmt.Errorf("save %s: %w", s.Local, err)
	}
	for _, k := range sortedKeys(updates) {
		if root, err = setFrontmatterField(root, k, updates[k]); err != nil {
			return fmt.Errorf("save %s: %w", s.Local, err)
		}
	}
	fm, err := marshalFrontmatterNode(root)
	if err != nil {
		return fmt.Errorf("save %s: %w", s.Local, err)
	}
	if body == nil {
		body = oldBody
	}
	var buf bytes.Buffer
	if len(fm) > 0 {
		buf.WriteString("---\n")
		buf.Write(fm)
		buf.WriteString("---\n")
	}
	buf.Write(body)
	if err := os.WriteFile(s.Local, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("save: write %s: %w", s.Local, err)
	}
	// Invalidate the cache so the next LoadContent re-reads from disk through
	// the normal render path. We avoid calling ReloadContent here directly
	// because that path dereferences s.sg, which a standalone caller may not
	// have wired; the fsnotify watcher also rebuilds on this write.
	s.content = nil
	s.Err = nil
	return nil
}
