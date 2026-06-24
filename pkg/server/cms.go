package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/altlimit/sitegen/pkg/sitegen"
	yaml "gopkg.in/yaml.v3"
)

// cms.go mounts a minimal, raw-mode editing UI + JSON API under /__cms, served
// from the dev server next to the /__hotreload branch. It reads and writes the
// same source files the generator consumes: a save writes the file, the
// existing fsnotify watch rebuilds, and the SSE hot-reload refreshes the
// preview. No database, no separate content store.
//
// This is the MVP "raw mode" from ROADMAP.md: list source files, edit their
// frontmatter (as YAML text) and body, save. Typed widgets / collections
// (cms.yaml) build on top of this later, and can route saves through
// sitegen.Source.Save for order/comment-preserving structured edits.

// editableExts are the text source types the raw editor exposes. Binary/asset
// files (images, etc.) are intentionally excluded.
var editableExts = map[string]bool{
	".html": true,
	".md":   true,
	".xml":  true,
	".css":  true,
	".json": true,
}

// serveCMS dispatches all /__cms requests. It is only reached when CMSEnabled.
func (ss *StaticServer) serveCMS(w http.ResponseWriter, r *http.Request) {
	if !ss.cmsAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Basic realm="sitegen cms"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch {
	case r.URL.Path == "/__cms" || r.URL.Path == "/__cms/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(cmsHTML))
	case r.URL.Path == "/__cms/api/sources":
		ss.cmsListSources(w, r)
	case r.URL.Path == "/__cms/api/config":
		ss.cmsConfig(w, r)
	case r.URL.Path == "/__cms/api/blocks":
		ss.cmsSaveBlocks(w, r)
	case r.URL.Path == "/__cms/api/create":
		ss.cmsCreate(w, r)
	case r.URL.Path == "/__cms/api/data":
		if r.Method == http.MethodPost {
			ss.cmsSaveData(w, r)
		} else {
			ss.cmsReadData(w, r)
		}
	case r.URL.Path == "/__cms/api/upload":
		ss.cmsUpload(w, r)
	case r.URL.Path == "/__cms/api/source":
		if r.Method == http.MethodPost {
			ss.cmsSaveSource(w, r)
		} else {
			ss.cmsReadSource(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func (ss *StaticServer) cmsAuthorized(r *http.Request) bool {
	if ss.CMSAuth == "" {
		return true
	}
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	want := strings.SplitN(ss.CMSAuth, ":", 2)
	if len(want) != 2 {
		return false
	}
	// constant-time compare to avoid leaking length/contents via timing
	uOK := subtle.ConstantTimeCompare([]byte(user), []byte(want[0])) == 1
	pOK := subtle.ConstantTimeCompare([]byte(pass), []byte(want[1])) == 1
	return uOK && pOK
}

// resolveSrc maps a client-supplied relative path to an absolute path inside
// SrcDir, neutralising any "../" traversal. ok is false if the result escapes
// SrcDir.
func (ss *StaticServer) resolveSrc(rel string) (string, bool) {
	clean := filepath.Clean("/" + filepath.ToSlash(rel)) // root the path, drop ".."
	full := filepath.Join(ss.SrcDir, clean)
	base := filepath.Clean(ss.SrcDir)
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", false
	}
	return full, true
}

func (ss *StaticServer) cmsListSources(w http.ResponseWriter, r *http.Request) {
	var files []string
	root := filepath.Clean(ss.SrcDir)
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !editableExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	writeJSON(w, http.StatusOK, map[string]interface{}{"sources": files})
}

func (ss *StaticServer) cmsReadSource(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	full, ok := ss.resolveSrc(rel)
	if rel == "" || !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	meta, body, hasFM := sitegen.SplitFrontmatter(raw)

	// Parse out blocks (if any) so the editor can render the typed block
	// builder. A page is "block-based" when its frontmatter has a blocks: list.
	var fm struct {
		Blocks []map[string]interface{} `yaml:"blocks"`
		Path   string                   `yaml:"path"`
	}
	_ = yaml.Unmarshal(meta, &fm)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":           rel,
		"frontmatter":    string(meta),
		"body":           string(body),
		"hasFrontmatter": hasFM,
		"blocks":         fm.Blocks,
		"isBlockPage":    fm.Blocks != nil,
		"previewURL":     cmsPreviewURL(rel, fm.Path, ss.BaseDir),
	})
}

// cmsPreviewURL maps a source's relative path to the URL the dev server serves
// it at, mirroring the engine's path rules (trim .html/.htm/.md and a trailing
// "index"; honor a frontmatter path: override). Returns "" for files that
// aren't standalone viewable pages (css, xml, …), so the editor leaves the
// preview where it is.
func cmsPreviewURL(rel, override, base string) string {
	if base == "" {
		base = "/"
	}
	join := func(p string) string {
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		return base + strings.TrimPrefix(p, "/")
	}
	if override != "" {
		return join(override)
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".html", ".htm", ".md":
		p := strings.TrimSuffix(rel, filepath.Ext(rel))
		p = strings.TrimSuffix(p, "index")
		p = strings.Trim(p, "/")
		if p == "" {
			return base
		}
		return join(p) + "/"
	default:
		// Non-page assets (sitemap.xml, css, …) are served at their own path,
		// so the preview can still show them.
		return join(rel)
	}
}

// cmsField / cmsBlockType / cmsConfig mirror the optional site/cms.yaml schema.
// When present they let the editor offer "add block -> pick type" and typed
// widgets; when absent the editor infers fields from a page's existing blocks.
type cmsField struct {
	Name    string      `yaml:"name" json:"name"`
	Label   string      `yaml:"label,omitempty" json:"label,omitempty"`
	Widget  string      `yaml:"widget,omitempty" json:"widget,omitempty"`
	Default interface{} `yaml:"default,omitempty" json:"default,omitempty"`
	// Fields describes the shape of each item for widget: list (or the keys of
	// an object widget), enabling a typed sub-form + "add item".
	Fields []cmsField `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// cmsCollection describes a folder collection (many entries as files under
// src/<folder>) from site/cms.yaml. Body is the field whose widget is markdown
// (defaults to "body"); every other field is a frontmatter key.
type cmsCollection struct {
	Name      string     `yaml:"name" json:"name"`
	Label     string     `yaml:"label,omitempty" json:"label,omitempty"`
	Folder    string     `yaml:"folder" json:"folder"`
	Extension string     `yaml:"extension,omitempty" json:"extension,omitempty"`
	Slug      string     `yaml:"slug,omitempty" json:"slug,omitempty"`
	Fields    []cmsField `yaml:"fields,omitempty" json:"fields,omitempty"`
}

type cmsBlockType struct {
	Type   string     `yaml:"type" json:"type"`
	Label  string     `yaml:"label,omitempty" json:"label,omitempty"`
	Fields []cmsField `yaml:"fields" json:"fields"`
}

// cmsDataFile describes an editable data/*.json file. list=true means the file
// holds a JSON array (edited as a list of items); otherwise a single object.
type cmsDataFile struct {
	Name   string     `yaml:"name" json:"name"`
	Label  string     `yaml:"label,omitempty" json:"label,omitempty"`
	File   string     `yaml:"file" json:"file"`
	List   bool       `yaml:"list,omitempty" json:"list,omitempty"`
	Fields []cmsField `yaml:"fields,omitempty" json:"fields,omitempty"`
}

type cmsConfig struct {
	Blocks      []cmsBlockType  `yaml:"blocks,omitempty" json:"blocks,omitempty"`
	Collections []cmsCollection `yaml:"collections,omitempty" json:"collections,omitempty"`
	Data        []cmsDataFile   `yaml:"data,omitempty" json:"data,omitempty"`
	// MediaFolder is where image uploads land, relative to the source dir
	// (default "img"). Uploaded images are processed by the normal build/webp
	// pipeline on the next rebuild.
	MediaFolder string `yaml:"media_folder,omitempty" json:"media_folder,omitempty"`
}

// configPath is site/cms.yaml — the sibling of the source dir.
func (ss *StaticServer) configPath() string {
	return filepath.Join(filepath.Dir(filepath.Clean(ss.SrcDir)), "cms.yaml")
}

// cmsConfig serves the parsed site/cms.yaml, or an empty config if absent.
// Either way the editor works (hybrid: inference + optional schema).
func (ss *StaticServer) cmsConfig(w http.ResponseWriter, r *http.Request) {
	cfg := cmsConfig{}
	if raw, err := os.ReadFile(ss.configPath()); err == nil {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"blocks":      []cmsBlockType{},
				"configError": err.Error(),
			})
			return
		}
	}
	if cfg.Blocks == nil {
		cfg.Blocks = []cmsBlockType{}
	}
	if cfg.Collections == nil {
		cfg.Collections = []cmsCollection{}
	}
	if cfg.Data == nil {
		cfg.Data = []cmsDataFile{}
	}
	writeJSON(w, http.StatusOK, cfg)
}

// loadDataFile returns the named data-file definition from cms.yaml.
func (ss *StaticServer) loadDataFile(name string) (cmsDataFile, bool) {
	var cfg cmsConfig
	if raw, err := os.ReadFile(ss.configPath()); err == nil {
		if err := yaml.Unmarshal(raw, &cfg); err == nil {
			for _, d := range cfg.Data {
				if d.Name == name {
					return d, true
				}
			}
		}
	}
	return cmsDataFile{}, false
}

// resolveData maps a data-file's configured filename to an absolute path inside
// DataDir, rejecting any "../" escape.
func (ss *StaticServer) resolveData(file string) (string, bool) {
	clean := filepath.Clean("/" + filepath.ToSlash(file))
	full := filepath.Join(ss.DataDir, clean)
	base := filepath.Clean(ss.DataDir)
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", false
	}
	return full, true
}

func (ss *StaticServer) cmsReadData(w http.ResponseWriter, r *http.Request) {
	def, ok := ss.loadDataFile(r.URL.Query().Get("name"))
	if !ok {
		http.Error(w, "unknown data file", http.StatusBadRequest)
		return
	}
	full, okp := ss.resolveData(def.File)
	if !okp {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	var value interface{}
	if raw, err := os.ReadFile(full); err == nil {
		if err := json.Unmarshal(raw, &value); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"name": def.Name, "error": "file is not valid JSON: " + err.Error()})
			return
		}
	} else if def.List {
		value = []interface{}{}
	} else {
		value = map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"name": def.Name, "value": value})
}

type cmsDataReq struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

func (ss *StaticServer) cmsSaveData(w http.ResponseWriter, r *http.Request) {
	var req cmsDataReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	def, ok := ss.loadDataFile(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "unknown data file: " + req.Name})
		return
	}
	full, okp := ss.resolveData(def.File)
	if !okp {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	order := make([]string, 0, len(def.Fields))
	for _, f := range def.Fields {
		order = append(order, f.Name)
	}
	out, err := marshalDataJSON(req.Value, order)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if err := os.WriteFile(full, out, 0644); err != nil {
		http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// marshalDataJSON renders value as pretty (4-space) JSON, emitting object keys
// in the schema's field order first (then any remaining keys sorted), so data
// files humans also edit keep a stable, readable key order across saves.
func marshalDataJSON(value interface{}, order []string) ([]byte, error) {
	compact, err := marshalOrdered(value, order)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact, "", "    "); err != nil {
		return nil, err
	}
	pretty.WriteByte('\n')
	return pretty.Bytes(), nil
}

func marshalOrdered(v interface{}, order []string) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		seen := map[string]bool{}
		keys := make([]string, 0, len(val))
		for _, k := range order {
			if _, ok := val[k]; ok && !seen[k] {
				keys = append(keys, k)
				seen[k] = true
			}
		}
		rest := make([]string, 0)
		for k := range val {
			if !seen[k] {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		keys = append(keys, rest...)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := marshalOrdered(val[k], order)
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []interface{}:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, e := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			vb, err := marshalOrdered(e, order)
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(v)
	}
}

// loadCollection returns the named collection from cms.yaml, or ok=false.
func (ss *StaticServer) loadCollection(name string) (cmsCollection, bool) {
	var cfg cmsConfig
	if raw, err := os.ReadFile(ss.configPath()); err == nil {
		if err := yaml.Unmarshal(raw, &cfg); err == nil {
			for _, c := range cfg.Collections {
				if c.Name == name {
					return c, true
				}
			}
		}
	}
	return cmsCollection{}, false
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases s and collapses any run of non-alphanumerics into single
// hyphens, trimming hyphens from the ends.
func slugify(s string) string {
	return strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

var slugTemplateField = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

type cmsCreateReq struct {
	Collection string                 `json:"collection"`
	Fields     map[string]interface{} `json:"fields"`
	Body       string                 `json:"body"`
}

// cmsCreate creates a new entry in a folder collection: it derives a slug,
// writes src/<folder>/<slug>.<ext> with the collection's fields as frontmatter
// (in declared order) plus the body, and returns the new entry's relative path.
func (ss *StaticServer) cmsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req cmsCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	col, ok := ss.loadCollection(req.Collection)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "unknown collection: " + req.Collection})
		return
	}
	ext := col.Extension
	if ext == "" {
		ext = "md"
	}
	// Which field carries the body? widget: markdown, else a field named "body".
	bodyField := "body"
	for _, f := range col.Fields {
		if f.Widget == "markdown" {
			bodyField = f.Name
			break
		}
	}
	// Derive the slug: from the slug template's referenced field, else "title".
	slugField := "title"
	if m := slugTemplateField.FindStringSubmatch(col.Slug); m != nil {
		slugField = m[1]
	}
	slug := ""
	if v, ok := req.Fields[slugField]; ok {
		slug = slugify(fmt.Sprint(v))
	}
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "cannot derive a slug from field '" + slugField + "' — provide a value",
		})
		return
	}

	rel := filepath.ToSlash(filepath.Join(col.Folder, slug+"."+ext))
	full, okp := ss.resolveSrc(rel)
	if !okp {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Build ordered frontmatter from the declared field order (skipping body).
	var fields []sitegen.FrontmatterField
	for _, f := range col.Fields {
		if f.Name == bodyField {
			continue
		}
		val, present := req.Fields[f.Name]
		if !present {
			val = f.Default
		}
		if val == nil {
			val = ""
		}
		fields = append(fields, sitegen.FrontmatterField{Key: f.Name, Value: val})
	}

	if err := sitegen.CreateSource(full, fields, []byte("\n"+req.Body)); err != nil {
		if errors.Is(err, os.ErrExist) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "an entry named '" + slug + "' already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": rel})
}

type cmsBlocksReq struct {
	Path   string                   `json:"path"`
	Blocks []map[string]interface{} `json:"blocks"`
}

// cmsSaveBlocks persists the block builder's structured edits, routing through
// sitegen.Source.SaveBlocks so the blocks key is written type-first and all
// other frontmatter (order, comments) is preserved.
func (ss *StaticServer) cmsSaveBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req cmsBlocksReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	full, ok := ss.resolveSrc(req.Path)
	if req.Path == "" || !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(full); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s := &sitegen.Source{Local: full}
	if err := s.SaveBlocks(req.Blocks, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

type cmsSaveReq struct {
	Path        string `json:"path"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
}

func (ss *StaticServer) cmsSaveSource(w http.ResponseWriter, r *http.Request) {
	var req cmsSaveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	full, ok := ss.resolveSrc(req.Path)
	if req.Path == "" || !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(full); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Validate the frontmatter parses as YAML before touching the file, so a
	// typo can't produce a source that breaks every subsequent build.
	fm := strings.TrimRight(req.Frontmatter, "\n")
	if strings.TrimSpace(fm) != "" {
		var probe map[string]interface{}
		if err := yaml.Unmarshal([]byte(fm), &probe); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": "frontmatter is not valid YAML: " + err.Error(),
			})
			return
		}
	}

	var buf bytes.Buffer
	if strings.TrimSpace(fm) != "" {
		buf.WriteString("---\n")
		buf.WriteString(fm)
		buf.WriteString("\n---\n")
	}
	buf.WriteString(req.Body)

	if err := os.WriteFile(full, buf.Bytes(), 0644); err != nil {
		http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".svg": true, ".avif": true,
}

// mediaFolder returns the configured upload folder (relative to src), or "img".
func (ss *StaticServer) mediaFolder() string {
	var cfg cmsConfig
	if raw, err := os.ReadFile(ss.configPath()); err == nil {
		_ = yaml.Unmarshal(raw, &cfg)
	}
	if cfg.MediaFolder != "" {
		return cfg.MediaFolder
	}
	return "img"
}

// sanitizeFilename slugifies the stem of an uploaded file and lowercases its
// extension, so uploads can't introduce path separators or odd characters.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(name))
	stem := slugify(strings.TrimSuffix(name, filepath.Ext(name)))
	if stem == "" {
		stem = "image"
	}
	return stem + ext
}

// uniquePath returns dir/name, appending -1, -2, … if needed so an upload never
// overwrites an existing media file.
func uniquePath(dir, name string) string {
	full := filepath.Join(dir, name)
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return full
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		c := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(c); os.IsNotExist(err) {
			return c
		}
	}
}

// cmsUpload accepts a multipart "file" image and saves it into the media folder
// under src/. It returns the site-root URL to store in a frontmatter/data field
// (e.g. /img/photo.png). The normal build (and -webp) pipeline processes it on
// the next rebuild.
func (ss *StaticServer) cmsUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	name := sanitizeFilename(header.Filename)
	if !imageExts[strings.ToLower(filepath.Ext(name))] {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "unsupported image type"})
		return
	}
	media := ss.mediaFolder()
	dir := filepath.Join(ss.SrcDir, media)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "mkdir failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	full := uniquePath(dir, name)
	out, err := os.Create(full)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	url := "/" + filepath.ToSlash(filepath.Join(media, filepath.Base(full)))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "url": url})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
