package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newCMSTestServer builds a StaticServer pointed at a temp site with one
// block-based page and a cms.yaml, and returns it plus the source dir.
func newCMSTestServer(t *testing.T) (*StaticServer, string) {
	t.Helper()
	site := t.TempDir()
	src := filepath.Join(site, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	index := "---\ntemplate: page.html\nblocks:\n" +
		"  - type: hero\n    heading: Original Heading\n    text: Sub\n" +
		"  - type: card_grid\n    cards:\n      - { icon: A, heading: One }\n---\n"
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(index), 0644); err != nil {
		t.Fatal(err)
	}
	cms := "blocks:\n  - type: hero\n    label: Hero\n    fields:\n" +
		"      - { name: heading, widget: string }\n      - { name: text, widget: text }\n"
	if err := os.WriteFile(filepath.Join(site, "cms.yaml"), []byte(cms), 0644); err != nil {
		t.Fatal(err)
	}
	return &StaticServer{CMSEnabled: true, SrcDir: src}, src
}

func doJSON(t *testing.T, ss *StaticServer, method, url, body string) (int, map[string]interface{}) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	w := httptest.NewRecorder()
	ss.ServeHTTP(w, r)
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestCMSConfigEndpoint(t *testing.T) {
	ss, _ := newCMSTestServer(t)
	code, out := doJSON(t, ss, "GET", "/__cms/api/config", "")
	if code != 200 {
		t.Fatalf("config status %d", code)
	}
	blocks, _ := out["blocks"].([]interface{})
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block type, got %#v", out["blocks"])
	}
	first := blocks[0].(map[string]interface{})
	if first["type"] != "hero" {
		t.Errorf("block type = %v", first["type"])
	}
}

func TestCMSConfigExposesNestedListFields(t *testing.T) {
	site := t.TempDir()
	src := filepath.Join(site, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	cms := "blocks:\n  - type: card_grid\n    fields:\n      - name: cards\n        widget: list\n" +
		"        fields:\n          - { name: icon, widget: string }\n          - { name: heading, widget: string }\n"
	if err := os.WriteFile(filepath.Join(site, "cms.yaml"), []byte(cms), 0644); err != nil {
		t.Fatal(err)
	}
	ss := &StaticServer{CMSEnabled: true, SrcDir: src}
	code, out := doJSON(t, ss, "GET", "/__cms/api/config", "")
	if code != 200 {
		t.Fatalf("config status %d", code)
	}
	blocks := out["blocks"].([]interface{})
	cards := blocks[0].(map[string]interface{})["fields"].([]interface{})[0].(map[string]interface{})
	if cards["widget"] != "list" {
		t.Fatalf("cards widget = %v", cards["widget"])
	}
	itemFields, ok := cards["fields"].([]interface{})
	if !ok || len(itemFields) != 2 {
		t.Fatalf("nested list item fields not exposed: %#v", cards["fields"])
	}
}

func TestCMSReadParsesBlocks(t *testing.T) {
	ss, _ := newCMSTestServer(t)
	code, out := doJSON(t, ss, "GET", "/__cms/api/source?path=index.html", "")
	if code != 200 {
		t.Fatalf("read status %d", code)
	}
	if out["isBlockPage"] != true {
		t.Fatalf("expected isBlockPage true, got %v", out["isBlockPage"])
	}
	blocks := out["blocks"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	hero := blocks[0].(map[string]interface{})
	if hero["type"] != "hero" || hero["heading"] != "Original Heading" {
		t.Errorf("hero block = %#v", hero)
	}
}

func TestCMSSaveBlocksWritesFileAndRebuildsModel(t *testing.T) {
	ss, src := newCMSTestServer(t)
	payload := `{"path":"index.html","blocks":[
		{"type":"hero","heading":"Edited Heading","text":"Sub"},
		{"type":"card_grid","cards":[{"icon":"A","heading":"One"}]}
	]}`
	code, out := doJSON(t, ss, "POST", "/__cms/api/blocks", payload)
	if code != 200 || out["ok"] != true {
		t.Fatalf("save status %d out %#v", code, out)
	}
	got, err := os.ReadFile(filepath.Join(src, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	g := string(got)
	if !strings.Contains(g, "heading: Edited Heading") {
		t.Errorf("edit not written:\n%s", g)
	}
	// type leads each block; sibling frontmatter (template) preserved.
	if !strings.Contains(g, "- type: hero") || !strings.Contains(g, "template: page.html") {
		t.Errorf("structure/order not preserved:\n%s", g)
	}
}

func newCMSWithCollection(t *testing.T) (*StaticServer, string) {
	t.Helper()
	site := t.TempDir()
	src := filepath.Join(site, "src")
	if err := os.MkdirAll(filepath.Join(src, "blog"), 0755); err != nil {
		t.Fatal(err)
	}
	cms := "collections:\n  - name: blog\n    label: Blog Post\n    folder: blog\n    extension: md\n    slug: \"{{title}}\"\n" +
		"    fields:\n      - { name: title, widget: string }\n      - { name: date, widget: string }\n" +
		"      - { name: template, widget: hidden, default: main.html }\n      - { name: body, widget: markdown }\n"
	if err := os.WriteFile(filepath.Join(site, "cms.yaml"), []byte(cms), 0644); err != nil {
		t.Fatal(err)
	}
	return &StaticServer{CMSEnabled: true, SrcDir: src}, src
}

func TestCMSCreateEntryWritesSluggedFileWithDefaults(t *testing.T) {
	ss, src := newCMSWithCollection(t)
	body := `{"collection":"blog","fields":{"title":"My First Post!","date":"2026-06-23"},"body":"# Hello\n\nbody"}`
	code, out := doJSON(t, ss, "POST", "/__cms/api/create", body)
	if code != 200 || out["ok"] != true {
		t.Fatalf("create status %d out %#v", code, out)
	}
	if out["path"] != "blog/my-first-post.md" {
		t.Fatalf("slug path = %v", out["path"])
	}
	got, err := os.ReadFile(filepath.Join(src, "blog", "my-first-post.md"))
	if err != nil {
		t.Fatal(err)
	}
	g := string(got)
	for _, want := range []string{"title: My First Post!", "2026-06-23", "template: main.html", "# Hello"} {
		if !strings.Contains(g, want) {
			t.Errorf("missing %q in:\n%s", want, g)
		}
	}
	// frontmatter follows declared field order: title, date, template
	ti, di, te := strings.Index(g, "title:"), strings.Index(g, "date:"), strings.Index(g, "template:")
	if !(ti < di && di < te) {
		t.Errorf("field order wrong (title=%d date=%d template=%d):\n%s", ti, di, te, g)
	}
}

func TestCMSCreateRejectsDuplicateAndMissingSlug(t *testing.T) {
	ss, _ := newCMSWithCollection(t)
	first := `{"collection":"blog","fields":{"title":"Dupe"},"body":"x"}`
	if code, _ := doJSON(t, ss, "POST", "/__cms/api/create", first); code != 200 {
		t.Fatalf("first create status %d", code)
	}
	if code, _ := doJSON(t, ss, "POST", "/__cms/api/create", first); code != http.StatusConflict {
		t.Errorf("duplicate create expected 409, got %d", code)
	}
	noTitle := `{"collection":"blog","fields":{"date":"2026-06-23"},"body":"x"}`
	if code, _ := doJSON(t, ss, "POST", "/__cms/api/create", noTitle); code != http.StatusBadRequest {
		t.Errorf("missing-slug create expected 400, got %d", code)
	}
}

func TestCMSPreviewURL(t *testing.T) {
	cases := []struct{ rel, override, base, want string }{
		{"index.html", "", "/", "/"},
		{"about.md", "", "/", "/about/"},
		{"blog/welcome.html", "", "/", "/blog/welcome/"},
		{"blog.html", "", "/", "/blog/"},
		{"index.html", "", "/sub/", "/sub/"},
		{"about.md", "", "/sub/", "/sub/about/"},
		{"page.html", "/custom-path", "/", "/custom-path"},
		{"sitemap.xml", "", "/", "/sitemap.xml"}, // served at its own path
		{"css/styles.css", "", "/", "/css/styles.css"},
		{"sitemap.xml", "", "/sub/", "/sub/sitemap.xml"},
	}
	for _, c := range cases {
		if got := cmsPreviewURL(c.rel, c.override, c.base); got != c.want {
			t.Errorf("cmsPreviewURL(%q,%q,%q) = %q, want %q", c.rel, c.override, c.base, got, c.want)
		}
	}
}

func TestCMSReadReturnsPreviewURL(t *testing.T) {
	ss, _ := newCMSTestServer(t)
	ss.BaseDir = "/"
	_, out := doJSON(t, ss, "GET", "/__cms/api/source?path=index.html", "")
	if out["previewURL"] != "/" {
		t.Errorf("previewURL = %v, want /", out["previewURL"])
	}
}

func newCMSWithData(t *testing.T) (*StaticServer, string) {
	t.Helper()
	site := t.TempDir()
	src := filepath.Join(site, "src")
	data := filepath.Join(site, "data")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "site.json"), []byte(`{"title":"Old","description":"d","url":"u"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "links.json"), []byte(`[{"name":"Home","path":"/"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	cms := "data:\n  - name: site\n    file: site.json\n    fields:\n      - { name: title, widget: string }\n" +
		"      - { name: description, widget: text }\n      - { name: url, widget: string }\n" +
		"  - name: nav\n    file: links.json\n    list: true\n    fields:\n      - { name: name }\n      - { name: path }\n"
	if err := os.WriteFile(filepath.Join(site, "cms.yaml"), []byte(cms), 0644); err != nil {
		t.Fatal(err)
	}
	return &StaticServer{CMSEnabled: true, SrcDir: src, DataDir: data}, data
}

func TestCMSReadDataObjectAndList(t *testing.T) {
	ss, _ := newCMSWithData(t)
	_, obj := doJSON(t, ss, "GET", "/__cms/api/data?name=site", "")
	if v := obj["value"].(map[string]interface{}); v["title"] != "Old" {
		t.Errorf("site value = %#v", obj["value"])
	}
	_, lst := doJSON(t, ss, "GET", "/__cms/api/data?name=nav", "")
	if v := lst["value"].([]interface{}); len(v) != 1 {
		t.Errorf("nav value = %#v", lst["value"])
	}
}

func TestCMSSaveDataKeepsSchemaKeyOrder(t *testing.T) {
	ss, data := newCMSWithData(t)
	// Send keys in a scrambled order; the file must come back in schema order.
	body := `{"name":"site","value":{"url":"https://x","title":"New Title","description":"hello"}}`
	if code, out := doJSON(t, ss, "POST", "/__cms/api/data", body); code != 200 || out["ok"] != true {
		t.Fatalf("save status %d out %#v", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(data, "site.json"))
	g := string(got)
	ti, di, ui := strings.Index(g, `"title"`), strings.Index(g, `"description"`), strings.Index(g, `"url"`)
	if !(ti >= 0 && ti < di && di < ui) {
		t.Errorf("keys not in schema order (title=%d desc=%d url=%d):\n%s", ti, di, ui, g)
	}
	if !strings.Contains(g, "New Title") {
		t.Errorf("value not saved:\n%s", g)
	}
}

func TestCMSSaveDataList(t *testing.T) {
	ss, data := newCMSWithData(t)
	body := `{"name":"nav","value":[{"name":"Home","path":"/"},{"name":"Blog","path":"/blog"}]}`
	if code, out := doJSON(t, ss, "POST", "/__cms/api/data", body); code != 200 || out["ok"] != true {
		t.Fatalf("save status %d out %#v", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(data, "links.json"))
	if !strings.Contains(string(got), "Blog") {
		t.Errorf("list not saved:\n%s", got)
	}
}

func uploadReq(t *testing.T, ss *StaticServer, filename string, content []byte) (int, map[string]interface{}) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(content)
	mw.Close()
	r := httptest.NewRequest("POST", "/__cms/api/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	ss.ServeHTTP(w, r)
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestCMSUploadSavesSanitizedAndUnique(t *testing.T) {
	ss, src := newCMSTestServer(t)

	code, out := uploadReq(t, ss, "My Photo!.PNG", []byte("\x89PNG\r\n"))
	if code != 200 || out["ok"] != true {
		t.Fatalf("upload status %d out %#v", code, out)
	}
	if out["url"] != "/img/my-photo.png" {
		t.Fatalf("url = %v, want /img/my-photo.png", out["url"])
	}
	if _, err := os.Stat(filepath.Join(src, "img", "my-photo.png")); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
	// A second upload of the same name must not overwrite.
	_, out2 := uploadReq(t, ss, "My Photo!.PNG", []byte("x"))
	if out2["url"] != "/img/my-photo-1.png" {
		t.Errorf("collision url = %v, want /img/my-photo-1.png", out2["url"])
	}

	// Non-image is rejected.
	if code, _ := uploadReq(t, ss, "evil.sh", []byte("rm -rf")); code != http.StatusBadRequest {
		t.Errorf("non-image upload expected 400, got %d", code)
	}
}

// extractScripts pulls the contents of every <script> block out of html.
func extractScripts(html string) string {
	var b strings.Builder
	rest := html
	for {
		i := strings.Index(rest, "<script>")
		if i < 0 {
			break
		}
		rest = rest[i+len("<script>"):]
		j := strings.Index(rest, "</script>")
		if j < 0 {
			break
		}
		b.WriteString(rest[:j])
		b.WriteString("\n")
		rest = rest[j+len("</script>"):]
	}
	return b.String()
}

// TestCMSEditorJSParses guards the embedded editor script against syntax errors
// — the one part of the CMS with no compiler checking it. Uses `node --check`
// (syntax only, no execution, so browser globals are irrelevant); skips cleanly
// where node isn't installed.
func TestCMSEditorJSParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping editor JS syntax check")
	}
	js := extractScripts(cmsHTML)
	if strings.TrimSpace(js) == "" {
		t.Fatal("no <script> found in cmsHTML")
	}
	f := filepath.Join(t.TempDir(), "editor.js")
	if err := os.WriteFile(f, []byte(js), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Fatalf("editor JS has a syntax error:\n%s", out)
	}
}

// TestCMSFullEditingSession drives the real handlers through a realistic flow:
// config -> upload -> save blocks (with a boolean + the uploaded image) ->
// create a collection entry -> edit a data file. It catches wiring regressions
// across the whole API surface.
func TestCMSFullEditingSession(t *testing.T) {
	site := t.TempDir()
	src := filepath.Join(site, "src")
	data := filepath.Join(site, "data")
	if err := os.MkdirAll(filepath.Join(src, "blog"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"),
		[]byte("---\ntemplate: page.html\nblocks:\n  - type: hero\n    heading: Hi\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "site.json"), []byte(`{"title":"Old"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cms := "media_folder: img\n" +
		"blocks:\n  - type: hero\n    fields:\n      - { name: heading, widget: string }\n      - { name: featured, widget: boolean }\n" +
		"  - type: image\n    fields:\n      - { name: src, widget: image }\n      - { name: alt, widget: string }\n" +
		"collections:\n  - name: blog\n    folder: blog\n    extension: md\n    slug: \"{{title}}\"\n" +
		"    fields:\n      - { name: title, widget: string }\n      - { name: body, widget: markdown }\n" +
		"data:\n  - name: site\n    file: site.json\n    fields:\n      - { name: title, widget: string }\n"
	if err := os.WriteFile(filepath.Join(site, "cms.yaml"), []byte(cms), 0644); err != nil {
		t.Fatal(err)
	}
	ss := &StaticServer{CMSEnabled: true, SrcDir: src, DataDir: data, BaseDir: "/"}

	// config exposes all three sections
	_, cfg := doJSON(t, ss, "GET", "/__cms/api/config", "")
	if len(cfg["blocks"].([]interface{})) != 2 || len(cfg["collections"].([]interface{})) != 1 || len(cfg["data"].([]interface{})) != 1 {
		t.Fatalf("config sections wrong: %#v", cfg)
	}

	// upload an image, capture its URL
	_, up := uploadReq(t, ss, "My Pic.PNG", []byte("\x89PNG\r\n"))
	imgURL, _ := up["url"].(string)
	if imgURL != "/img/my-pic.png" {
		t.Fatalf("upload url = %v", up["url"])
	}

	// save blocks: a hero with a boolean + an image block referencing the upload
	payload := fmt.Sprintf(`{"path":"index.html","blocks":[`+
		`{"type":"hero","heading":"Edited","featured":true},`+
		`{"type":"image","src":%q,"alt":"a pic"}]}`, imgURL)
	if code, out := doJSON(t, ss, "POST", "/__cms/api/blocks", payload); code != 200 || out["ok"] != true {
		t.Fatalf("save blocks status %d out %#v", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(src, "index.html"))
	g := string(got)
	if !strings.Contains(g, "featured: true") {
		t.Errorf("boolean not written as a bool:\n%s", g)
	}
	if !strings.Contains(g, imgURL) {
		t.Errorf("uploaded image path not referenced:\n%s", g)
	}

	// read it back as a block page
	_, rd := doJSON(t, ss, "GET", "/__cms/api/source?path=index.html", "")
	if rd["isBlockPage"] != true || len(rd["blocks"].([]interface{})) != 2 {
		t.Fatalf("read back wrong: %#v", rd["blocks"])
	}

	// create a collection entry
	_, cr := doJSON(t, ss, "POST", "/__cms/api/create", `{"collection":"blog","fields":{"title":"Hello World"},"body":"hi"}`)
	if cr["path"] != "blog/hello-world.md" {
		t.Fatalf("create path = %v", cr["path"])
	}

	// edit a data file
	if code, out := doJSON(t, ss, "POST", "/__cms/api/data", `{"name":"site","value":{"title":"New Title"}}`); code != 200 || out["ok"] != true {
		t.Fatalf("data save status %d out %#v", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "site.json")); !strings.Contains(string(b), "New Title") {
		t.Errorf("data not saved:\n%s", b)
	}
}

func TestCMSTraversalCannotEscapeSrc(t *testing.T) {
	ss, _ := newCMSTestServer(t)
	// even with ../ the path is clamped inside SrcDir, so it can only 404
	code, _ := doJSON(t, ss, "GET", "/__cms/api/source?path=../../cms.yaml", "")
	if code == 200 {
		t.Errorf("traversal reached a file outside src (status %d)", code)
	}
}
