package sitegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

func TestSplitFrontmatter(t *testing.T) {
	raw := "---\ntitle: Hello\ntemplate: main.html\n---\n\n{{define \"content\"}}body{{end}}\n"
	meta, body, ok := splitFrontmatter([]byte(raw))
	if !ok {
		t.Fatal("expected frontmatter to be detected")
	}
	if string(meta) != "title: Hello\ntemplate: main.html\n" {
		t.Errorf("meta = %q", meta)
	}
	if string(body) != "\n{{define \"content\"}}body{{end}}\n" {
		t.Errorf("body = %q", body)
	}

	// No frontmatter -> body is the whole file.
	noFM := "<html>just markup</html>"
	if m, b, ok := splitFrontmatter([]byte(noFM)); ok || m != nil || string(b) != noFM {
		t.Errorf("no-frontmatter split = (%q, %q, %v)", m, b, ok)
	}
}

func TestSetFrontmatterFieldPreservesOrder(t *testing.T) {
	root, err := parseFrontmatterNode([]byte("title: Old\ndate: 2026-01-01\ntemplate: main.html\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Update an existing middle key; it must keep its position.
	if root, err = setFrontmatterField(root, "title", "New"); err != nil {
		t.Fatal(err)
	}
	// Append a brand-new key; it must land at the end.
	if root, err = setFrontmatterField(root, "summary", "added"); err != nil {
		t.Fatal(err)
	}
	out, err := marshalFrontmatterNode(root)
	if err != nil {
		t.Fatal(err)
	}
	want := "title: New\ndate: 2026-01-01\ntemplate: main.html\nsummary: added\n"
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestSaveRoundTripPreservesOrderCommentsAndDates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	original := "---\n" +
		"title: Welcome\n" +
		"date: 2026-01-01 # publish date, do not quote\n" +
		"summary: hello\n" +
		"template: main.html\n" +
		"---\n\n" +
		"# Welcome\n\nBody stays put.\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	s := &Source{Local: path}
	if err := s.Save(map[string]interface{}{"title": "Welcome, updated"}, nil); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := string(got)

	if !strings.Contains(g, "title: Welcome, updated") {
		t.Errorf("updated value missing:\n%s", g)
	}
	// Comment preserved.
	if !strings.Contains(g, "# publish date, do not quote") {
		t.Errorf("comment not preserved:\n%s", g)
	}
	// Date kept as an unquoted plain string, not reformatted to a timestamp.
	if !strings.Contains(g, "date: 2026-01-01") {
		t.Errorf("date reformatted:\n%s", g)
	}
	// Key order preserved (title < date < summary < template).
	order := []string{"title:", "date:", "summary:", "template:"}
	last := -1
	for _, k := range order {
		i := strings.Index(g, k)
		if i < 0 {
			t.Fatalf("key %q missing:\n%s", k, g)
		}
		if i < last {
			t.Errorf("key %q out of order:\n%s", k, g)
		}
		last = i
	}
	// Body untouched.
	if !strings.Contains(g, "# Welcome\n\nBody stays put.\n") {
		t.Errorf("body changed:\n%s", g)
	}
}

func TestSaveBlocksOrdersTypeFirstAndPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	original := "---\n# page layout\ntemplate: page.html\nblocks:\n  - type: hero\n    heading: old\n---\nignored body\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	s := &Source{Local: path}
	blocks := []map[string]interface{}{
		{"text": "Subtitle", "type": "hero", "heading": "New Title"},
		{"type": "card_grid", "cards": []interface{}{
			map[string]interface{}{"heading": "Fast", "icon": "🚀"},
		}},
	}
	if err := s.SaveBlocks(blocks, nil); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := string(got)

	// "type" must lead each block, not the alphabetical "heading"/"text".
	if i := strings.Index(g, "- type: hero"); i < 0 {
		t.Errorf("type not first in block:\n%s", g)
	}
	// Sibling frontmatter keys (and the comment) survive.
	if !strings.Contains(g, "# page layout") || !strings.Contains(g, "template: page.html") {
		t.Errorf("other frontmatter not preserved:\n%s", g)
	}
	// New value written.
	if !strings.Contains(g, "heading: New Title") {
		t.Errorf("updated value missing:\n%s", g)
	}

	// And it must decode back into a usable structure for the engine.
	var probe map[string]interface{}
	meta, _, _ := splitFrontmatter(got)
	if err := yaml.Unmarshal(meta, &probe); err != nil {
		t.Fatalf("result frontmatter does not parse: %v\n%s", err, g)
	}
	bs, ok := probe["blocks"].([]interface{})
	if !ok || len(bs) != 2 {
		t.Fatalf("blocks did not round-trip: %#v", probe["blocks"])
	}
}

func TestSaveBlocksKeepsEmojiLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, []byte("---\ntemplate: page.html\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Source{Local: path}
	blocks := []map[string]interface{}{
		{"type": "card_grid", "cards": []interface{}{
			map[string]interface{}{"icon": "🚀", "heading": "Fast"},
		}},
	}
	if err := s.SaveBlocks(blocks, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	g := string(got)
	if !strings.Contains(g, "🚀") {
		t.Errorf("emoji not written literally:\n%s", g)
	}
	if strings.Contains(g, `\U`) || strings.Contains(g, `\u`) {
		t.Errorf("emoji was escaped:\n%s", g)
	}
	// And a second save must be stable (still literal, not re-escaped).
	if err := s.SaveBlocks(blocks, nil); err != nil {
		t.Fatal(err)
	}
	g2, _ := os.ReadFile(path)
	if string(g2) != g {
		t.Errorf("re-save not stable:\nfirst:\n%s\nsecond:\n%s", g, g2)
	}
}

func TestSaveUnchangedFrontmatterIsByteExact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	original := "---\ntitle: Stable\ntemplate: main.html\n---\n\nBody.\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Source{Local: path}
	// No updates, keep body: output must equal input byte-for-byte.
	if err := s.Save(nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("round-trip not byte-exact:\ngot:\n%q\nwant:\n%q", got, original)
	}
}
