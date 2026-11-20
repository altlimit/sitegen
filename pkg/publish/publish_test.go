package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() { uploadRetryDelay = func(int) time.Duration { return 0 } }

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildManifest(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"index.html":          "<h1>hi</h1>",
		"css/styles.css":      "body{}",
		"docs/a/b/index.html": "deep",
		".well-known/x.txt":   "dot",
		"copy.html":           "<h1>hi</h1>",
	})
	m, err := BuildManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FileEntry{
		"/index.html":          {sha("<h1>hi</h1>"), 11},
		"/css/styles.css":      {sha("body{}"), 6},
		"/docs/a/b/index.html": {sha("deep"), 4},
		"/.well-known/x.txt":   {sha("dot"), 3},
		"/copy.html":           {sha("<h1>hi</h1>"), 11},
	}
	if len(m.Files) != len(want) {
		t.Fatalf("files = %v", m.Files)
	}
	for p, e := range want {
		if m.Files[p] != e {
			t.Errorf("%s = %+v, want %+v", p, m.Files[p], e)
		}
	}
	if m.TotalBytes != 35 || m.Local[sha("deep")] != filepath.Join(dir, "docs", "a", "b", "index.html") {
		t.Fatalf("total %d, local %v", m.TotalBytes, m.Local)
	}
	if err := m.CheckFileSize(10); err == nil || !strings.Contains(err.Error(), "/copy.html (11 bytes), /index.html (11 bytes)") {
		t.Fatalf("CheckFileSize = %v", err)
	}
	if err := m.CheckFileSize(DefaultMaxFileSize); err != nil {
		t.Fatal(err)
	}
}

func TestBuildManifestLimits(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"index.html": "x"})
	f, err := os.Create(filepath.Join(dir, "video.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(HardMaxFileSize + 1) // sparse
	f.Close()
	if _, err := BuildManifest(dir); err == nil || !strings.Contains(err.Error(), "/video.mp4 (100.0 MB)") {
		t.Fatalf("err = %v", err)
	}

	if _, err := BuildManifest(t.TempDir()); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty dir: %v", err)
	}

	many := t.TempDir()
	for i := 0; i <= MaxFiles; i++ {
		os.WriteFile(filepath.Join(many, strconv.Itoa(i)), nil, 0o644)
	}
	if _, err := BuildManifest(many); err == nil || !strings.Contains(err.Error(), "5001 files") {
		t.Fatalf("too many: %v", err)
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"My Site":        "my-site",
		"blog_2024!!":    "blog-2024",
		"--Ünïcode--":    "n-code",
		"already-ok-123": "already-ok-123",
	} {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	root := t.TempDir()
	if got := DefaultName(filepath.Join(root, "Cool Project", "site")); got != "cool-project" {
		t.Errorf("DefaultName(.../Cool Project/site) = %q", got)
	}
	if got := DefaultName(filepath.Join(root, "My_Blog")); got != "my-blog" {
		t.Errorf("DefaultName(.../My_Blog) = %q", got)
	}
	for _, bad := range []string{"ab", "-abc", "abc-", "a--b", "ABC", "a_b"} {
		if ValidateName(bad) == nil {
			t.Errorf("ValidateName(%q) passed", bad)
		}
	}

	if InstanceName("blog") != "sitegen-blog" || InstanceName("sitegen-blog") != "sitegen-blog" {
		t.Error("InstanceName")
	}

	site := filepath.Join(root, "proj", "site")
	os.MkdirAll(site, 0o755)
	check := func(flagName, wantName string, wantConfirmed bool) {
		t.Helper()
		name, confirmed, err := ResolveName(flagName, site)
		if err != nil || name != wantName || confirmed != wantConfirmed {
			t.Errorf("ResolveName(%q) = %q, %v, %v; want %q, %v", flagName, name, confirmed, err, wantName, wantConfirmed)
		}
	}
	check("", "sitegen-proj", false) // a guess from the folder
	WriteConfig(site, Config{Instance: "sitegen-from-config"})
	check("", "sitegen-from-config", true)
	check("from-flag", "sitegen-from-flag", true)
	check("sitegen-from-flag", "sitegen-from-flag", true)
	WriteConfig(site, Config{Instance: "legacy"}) // written as is, not prefixed
	check("", "legacy", true)
	if _, _, err := ResolveName("ab", site); err == nil {
		t.Error("short -publish-name accepted")
	}
}

func TestSystemStoreFile(t *testing.T) {
	s := &SystemStore{File: filepath.Join(t.TempDir(), "sitegen", "altengine.json"), NoKeyring: true}
	if _, err := s.Get("blog"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("Get on empty = %v", err)
	}
	if err := s.Set("blog", "ae_1"); err != nil {
		t.Fatal(err)
	}
	s.Set("shop", "ae_2")
	if k, _ := s.Get("blog"); k != "ae_1" {
		t.Fatalf("Get = %q", k)
	}
	info, err := os.Stat(s.File)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v (%v)", info.Mode(), err)
	}
	s.Delete("blog")
	if _, err := s.Get("blog"); !errors.Is(err, ErrNoKey) {
		t.Fatal("blog still stored")
	}
	if k, _ := s.Get("shop"); k != "ae_2" {
		t.Fatal("delete removed another site's key")
	}
}

// memStore is a KeyStore in memory.
type memStore struct {
	mu   sync.Mutex
	keys map[string]string
}

func (m *memStore) Get(n string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.keys[n]; ok {
		return k, nil
	}
	return "", ErrNoKey
}
func (m *memStore) Set(n, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = map[string]string{}
	}
	m.keys[n] = k
	return nil
}
func (m *memStore) Delete(n string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, n)
	return nil
}

// fakeAPI is api.altengine.net plus the signed-upload storage it hands out.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	keys        map[string]bool // valid bearer keys
	sites       map[string]bool
	stored      map[string]bool // content hashes the platform holds
	pageSize    int
	manifest    map[string]FileEntry
	pending     []string // missing hashes when the deployment was created
	messages    []string
	puts        []string // hashes PUT, in order
	putFailures map[string]int
	activations int
	mcp         func(params map[string]any) any // overrides the tools/call reply
	maxFile     int64
}

func newFakeAPI(t *testing.T, keys ...string) *fakeAPI {
	f := &fakeAPI{t: t, keys: map[string]bool{}, sites: map[string]bool{}, stored: map[string]bool{},
		pageSize: 100, putFailures: map[string]int{}}
	for _, k := range keys {
		f.keys[k] = true
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/upload/") {
		f.upload(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.keys[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		f.json(w, 401, map[string]any{"error": map[string]string{"code": "UNAUTHENTICATED", "message": "bad key"}})
		return
	}
	if r.URL.Path == "/mcp" {
		f.serveMCP(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/static/"), "/")
	name := parts[0]
	if !f.sites[name] {
		f.json(w, 404, map[string]any{"error": map[string]string{"code": "NOT_FOUND", "message": "no such instance"}})
		return
	}
	switch {
	case r.Method == "GET" && len(parts) == 2 && parts[1] == "site":
		f.json(w, 200, map[string]any{"name": name, "slug": name, "url": "https://" + name + "-web.altengine.app/", "active_deployment": nil})
	case r.Method == "POST" && len(parts) == 2 && parts[1] == "deployments":
		var body struct {
			Files   map[string]FileEntry `json:"files"`
			Message string               `json:"message"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.manifest = body.Files
		f.messages = append(f.messages, body.Message)
		f.pending = f.missing()
		uploads, cursor := f.page(0)
		var total int64
		for _, e := range body.Files {
			total += e.Size
		}
		f.json(w, 201, map[string]any{"deployment_id": "dep-1", "file_count": len(body.Files), "total_bytes": total,
			"missing_count": len(f.pending), "uploads": uploads, "cursor": cursor})
	case r.Method == "GET" && len(parts) == 4 && parts[1] == "deployments" && parts[3] == "uploads":
		start, err := strconv.Atoi(r.URL.Query().Get("cursor"))
		if err != nil {
			f.json(w, 400, map[string]string{"error": "bad cursor"})
			return
		}
		uploads, cursor := f.page(start)
		f.json(w, 200, map[string]any{"uploads": uploads, "cursor": cursor})
	case r.Method == "POST" && len(parts) == 4 && parts[1] == "deployments" && parts[3] == "activate":
		if missing := f.missing(); len(missing) > 0 {
			f.json(w, 409, map[string]any{"error": map[string]string{"code": "MISSING_FILES", "message": fmt.Sprintf("%d files never arrived", len(missing))}})
			return
		}
		f.activations++
		f.json(w, 200, map[string]any{"deployment_id": "dep-1", "file_count": len(f.manifest), "url": "https://" + name + "-web.altengine.app/"})
	default:
		f.json(w, 404, map[string]string{"error": "no route"})
	}
}

// missing is the manifest's hashes the platform lacks, sorted. Caller holds mu.
func (f *fakeAPI) missing() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range f.manifest {
		if !f.stored[e.Hash] && !seen[e.Hash] {
			seen[e.Hash] = true
			out = append(out, e.Hash)
		}
	}
	sort.Strings(out)
	return out
}

// page returns the uploads from index start, and the next cursor.
func (f *fakeAPI) page(start int) ([]map[string]any, any) {
	missing := f.pending
	sizes := map[string]int64{}
	for _, e := range f.manifest {
		sizes[e.Hash] = e.Size
	}
	end := min(start+f.pageSize, len(missing))
	uploads := []map[string]any{}
	for _, h := range missing[min(start, end):end] {
		uploads = append(uploads, map[string]any{
			"path_hash": h, "size": sizes[h], "upload_url": f.srv.URL + "/upload/" + h + "?sig=x", "method": "PUT",
			"required_headers": map[string]string{"Content-Length": strconv.FormatInt(sizes[h], 10), "X-Upload-Sig": "sig-" + h[:8]},
		})
	}
	if end < len(missing) {
		return uploads, strconv.Itoa(end)
	}
	return uploads, nil
}

func (f *fakeAPI) upload(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimPrefix(r.URL.Path, "/upload/")
	if r.Method != http.MethodPut || len(r.TransferEncoding) > 0 || r.ContentLength < 0 {
		f.t.Errorf("upload %s: %s, TE %v, length %d", hash[:8], r.Method, r.TransferEncoding, r.ContentLength)
	}
	if r.Header.Get("X-Upload-Sig") != "sig-"+hash[:8] {
		f.t.Errorf("upload %s: required header missing", hash[:8])
	}
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putFailures[hash] > 0 {
		f.putFailures[hash]--
		w.WriteHeader(503)
		return
	}
	if sha(string(body)) != hash || int64(len(body)) != r.ContentLength {
		w.WriteHeader(403)
		return
	}
	f.stored[hash] = true
	f.puts = append(f.puts, hash)
	w.WriteHeader(200)
}

func (f *fakeAPI) serveMCP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Method != "tools/call" {
		f.json(w, 200, map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "method not found"}})
		return
	}
	if f.mcp != nil {
		f.json(w, 200, map[string]any{"jsonrpc": "2.0", "id": 1, "result": f.mcp(req.Params)})
		return
	}
	args, _ := req.Params["arguments"].(map[string]any)
	switch req.Params["name"] {
	case "create_instance":
		if args["service"] != "static" {
			f.t.Errorf("create_instance args = %v", args)
		}
		name := args["name"].(string)
		f.sites[name] = true
		out, _ := json.Marshal(map[string]any{"service": "static", "name": name, "id": "i-1"})
		f.json(w, 200, map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(out)}}}})
	case "get_instance_config":
		f.json(w, 200, map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{
			"structuredContent": map[string]any{"config": map[string]any{"maxFileBytes": f.maxFile}},
			"content":           []map[string]any{{"type": "text", "text": "{}"}}}})
	default:
		f.json(w, 200, map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"isError": true,
			"content": []map[string]any{{"type": "text", "text": "unknown tool"}}}})
	}
}

// env is a fake Getenv.
type env map[string]string

func (e env) get(k string) string { return e[k] }

type harness struct {
	api     *fakeAPI
	console *fakeConsole
	store   *memStore
	env     env
	signIns int
	grant   string // scope the fake sign-in grants; "" means what was asked
	key     string // key the fake sign-in issues
	out     strings.Builder
	pub     *Publisher
}

func newHarness(t *testing.T) *harness {
	h := &harness{api: newFakeAPI(t), console: newFakeConsole(t), store: &memStore{}, env: env{}, key: "ae_new"}
	h.pub = &Publisher{
		ConsoleURL: h.console.srv.URL, APIURL: h.api.srv.URL, ClientID: altengineClientID,
		Store: h.store, Getenv: h.env.get, Out: &h.out,
		SignIn: func(_ context.Context, scope string, _ bool) (*Token, error) {
			h.signIns++
			granted := scope
			if h.grant != "" {
				granted = h.grant
			}
			h.api.mu.Lock()
			h.api.keys[h.key] = true
			h.api.mu.Unlock()
			tok := &Token{AccessToken: h.key, Scope: granted}
			tok.AltEngine.OrgName = "Acme"
			return tok, nil
		},
	}
	return h
}

func (h *harness) opts(t *testing.T, files map[string]string) Options {
	pub := filepath.Join(t.TempDir(), "public")
	writeFiles(t, pub, files)
	site := filepath.Join(t.TempDir(), "site")
	os.MkdirAll(site, 0o755)
	return Options{Name: "sitegen-blog", Confirmed: true, SitePath: site, PublicDir: pub, Message: "first"}
}

func TestPublishFreshSite(t *testing.T) {
	h := newHarness(t)
	opts := h.opts(t, map[string]string{"index.html": "home", "about/index.html": "about", "css/styles.css": "css"})

	res, err := h.pub.Publish(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if h.signIns != 1 || h.store.keys[keyEntry] != "ae_new" || len(h.store.keys) != 1 {
		t.Fatalf("signIns=%d stored=%v", h.signIns, h.store.keys)
	}
	if !h.api.sites["sitegen-blog"] {
		t.Fatal("instance not created")
	}
	if res.FileCount != 3 || res.Uploaded != 3 || res.URL != "https://sitegen-blog-web.altengine.app/" {
		t.Fatalf("result = %+v", res)
	}
	if cfg, _ := ReadConfig(opts.SitePath); cfg.Instance != "sitegen-blog" {
		t.Fatalf("publish.json = %+v", cfg)
	}
	b, _ := os.ReadFile(filepath.Join(opts.SitePath, ConfigFile))
	if strings.Contains(string(b), "ae_") {
		t.Fatal("key written to publish.json")
	}
	if h.api.messages[0] != "first" {
		t.Fatalf("message = %q", h.api.messages[0])
	}
	if !strings.Contains(h.out.String(), "Signed in to altengine: Acme") ||
		!strings.Contains(h.out.String(), "Published 3 files (3 uploaded) → https://sitegen-blog-web.altengine.app/") {
		t.Fatalf("output:\n%s", h.out.String())
	}

	// Again, unchanged: no prompt, no uploads.
	h.out.Reset()
	opts.Message = ""
	res, err = h.pub.Publish(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if h.signIns != 1 || res.Uploaded != 0 || len(h.api.puts) != 3 || h.api.activations != 2 {
		t.Fatalf("second run: signIns=%d result=%+v puts=%d", h.signIns, res, len(h.api.puts))
	}
	if !strings.Contains(h.out.String(), "Nothing changed; published again") {
		t.Fatalf("output:\n%s", h.out.String())
	}

	// One page changes: exactly one upload.
	os.WriteFile(filepath.Join(opts.PublicDir, "about", "index.html"), []byte("about v2"), 0o644)
	res, err = h.pub.Publish(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || h.api.puts[3] != sha("about v2") {
		t.Fatalf("third run: %+v, puts %v", res, h.api.puts)
	}
}

func TestPublishUploadPaging(t *testing.T) {
	h := newHarness(t)
	h.store.Set(keyEntry, "ae_old")
	h.api.keys["ae_old"] = true
	h.api.sites["sitegen-blog"] = true
	h.api.pageSize = 2
	files := map[string]string{}
	for i := 0; i < 5; i++ {
		files[fmt.Sprintf("p%d.html", i)] = fmt.Sprintf("page %d", i)
	}
	files["dup.html"] = "page 0" // same content: one upload
	opts := h.opts(t, files)
	// A transient storage failure is retried.
	h.api.putFailures[sha("page 3")] = 2

	res, err := h.pub.Publish(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 5 || len(h.api.puts) != 5 || res.FileCount != 6 {
		t.Fatalf("result = %+v, puts = %d", res, len(h.api.puts))
	}
	if h.signIns != 0 {
		t.Fatal("signed in with a stored key")
	}
}

func TestUploadGivesUpAfterRetries(t *testing.T) {
	h := newHarness(t)
	h.env[EnvAPIKey] = "ae_env"
	h.api.keys["ae_env"] = true
	h.api.sites["sitegen-blog"] = true
	opts := h.opts(t, map[string]string{"index.html": "x"})
	h.api.putFailures[sha("x")] = 3
	if _, err := h.pub.Publish(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v", err)
	}
	if h.api.activations != 0 {
		t.Fatal("activated with a missing file")
	}
}

func TestCreateInstanceRefused(t *testing.T) {
	h := newHarness(t)
	h.api.mcp = func(map[string]any) any {
		return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "instance quota reached (10 of 10)"}}}
	}
	opts := h.opts(t, map[string]string{"index.html": "x"})
	_, err := h.pub.Publish(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "instance quota reached") {
		t.Fatalf("err = %v", err)
	}
	if h.api.manifest != nil {
		t.Fatal("deployed after a refusal")
	}
	if _, err := os.Stat(filepath.Join(opts.SitePath, ConfigFile)); err == nil {
		t.Fatal("publish.json written after a refusal")
	}
}

func TestCallToolReplies(t *testing.T) {
	api := newFakeAPI(t, "k")
	c := &API{BaseURL: api.srv.URL, Key: "k"}
	api.mcp = func(map[string]any) any {
		return map[string]any{"structuredContent": map[string]any{"ok": true}, "content": []map[string]any{{"type": "text", "text": "not json"}}}
	}
	if raw, err := c.CallTool(context.Background(), "x", nil); err != nil || string(raw) != `{"ok":true}` {
		t.Fatalf("structured: %s %v", raw, err)
	}

	// JSON-RPC error.
	rpcErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad arguments"}}`))
	}))
	defer rpcErr.Close()
	if _, err := (&API{BaseURL: rpcErr.URL}).CallTool(context.Background(), "create_instance", nil); err == nil || !strings.Contains(err.Error(), "bad arguments") {
		t.Fatalf("rpc error: %v", err)
	}

	// An event-stream answer.
	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"name taken\"}]}}\n\n"))
	}))
	defer sse.Close()
	if _, err := (&API{BaseURL: sse.URL}).CallTool(context.Background(), "create_instance", nil); err == nil || !strings.Contains(err.Error(), "name taken") {
		t.Fatalf("sse isError: %v", err)
	}
}

func TestKeyResolutionOrder(t *testing.T) {
	t.Run("env var wins and is never stored", func(t *testing.T) {
		h := newHarness(t)
		h.env[EnvAPIKey] = "ae_env"
		h.store.Set(keyEntry, "ae_stored")
		key, src, err := h.pub.resolveKey(context.Background(), "sitegen-blog", false)
		if err != nil || key != "ae_env" || src != keyFromEnv || h.signIns != 0 {
			t.Fatalf("key=%q src=%v err=%v signIns=%d", key, src, err, h.signIns)
		}
	})
	t.Run("stored key next", func(t *testing.T) {
		h := newHarness(t)
		h.store.Set(keyEntry, "ae_stored")
		key, src, err := h.pub.resolveKey(context.Background(), "sitegen-blog", false)
		if err != nil || key != "ae_stored" || src != keyStored || h.signIns != 0 {
			t.Fatalf("key=%q src=%v err=%v", key, src, err)
		}
	})
	t.Run("then sign in", func(t *testing.T) {
		h := newHarness(t)
		key, src, err := h.pub.resolveKey(context.Background(), "sitegen-blog", false)
		if err != nil || key != "ae_new" || src != keySignedIn || h.signIns != 1 || h.store.keys[keyEntry] != "ae_new" {
			t.Fatalf("key=%q src=%v err=%v stored=%v", key, src, err, h.store.keys)
		}
	})
	t.Run("never prompts in CI", func(t *testing.T) {
		h := newHarness(t)
		h.env["CI"] = "true"
		if _, _, err := h.pub.resolveKey(context.Background(), "sitegen-blog", false); err == nil || !strings.Contains(err.Error(), EnvAPIKey) || h.signIns != 0 {
			t.Fatalf("err=%v signIns=%d", err, h.signIns)
		}
	})
}

func TestEnvKeyPublishStoresNothing(t *testing.T) {
	h := newHarness(t)
	h.env[EnvAPIKey] = "ae_env"
	h.api.keys["ae_env"] = true
	opts := h.opts(t, map[string]string{"index.html": "x"})
	if _, err := h.pub.Publish(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(h.store.keys) != 0 || h.signIns != 0 {
		t.Fatalf("stored=%v signIns=%d", h.store.keys, h.signIns)
	}
}

func TestRevokedStoredKeySignsInOnce(t *testing.T) {
	h := newHarness(t)
	h.store.Set(keyEntry, "ae_revoked") // not valid on the API
	h.api.sites["sitegen-blog"] = true
	opts := h.opts(t, map[string]string{"index.html": "x"})
	res, err := h.pub.Publish(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if h.signIns != 1 || h.store.keys[keyEntry] != "ae_new" || res.Uploaded != 1 {
		t.Fatalf("signIns=%d stored=%v res=%+v", h.signIns, h.store.keys, res)
	}

	// A fresh key that is refused too isn't retried forever.
	h2 := newHarness(t)
	h2.store.Set(keyEntry, "ae_revoked")
	h2.pub.SignIn = func(context.Context, string, bool) (*Token, error) {
		h2.signIns++
		return &Token{AccessToken: "ae_also_bad", Scope: Scope}, nil
	}
	if _, err := h2.pub.Publish(context.Background(), h2.opts(t, map[string]string{"index.html": "x"})); !errors.Is(err, ErrUnauthorized) || h2.signIns != 1 {
		t.Fatalf("err=%v signIns=%d", err, h2.signIns)
	}
}

func TestRevokedEnvKeyFails(t *testing.T) {
	h := newHarness(t)
	h.env[EnvAPIKey] = "ae_revoked"
	_, err := h.pub.Publish(context.Background(), h.opts(t, map[string]string{"index.html": "x"}))
	if !errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), EnvAPIKey) || h.signIns != 0 {
		t.Fatalf("err=%v signIns=%d", err, h.signIns)
	}
}

func TestSignInScopeCut(t *testing.T) {
	h := newHarness(t)
	h.grant = "org:read static:read:blog*"
	_, err := h.pub.Publish(context.Background(), h.opts(t, map[string]string{"index.html": "x"}))
	if err == nil || err.Error() != "Your role in Acme can't create or deploy this site; ask an owner, or use ALTENGINE_API_KEY." {
		t.Fatalf("err = %v", err)
	}
	if len(h.console.revoked) != 1 || h.console.revoked[0] != "ae_new" || len(h.store.keys) != 0 {
		t.Fatalf("revoked=%v stored=%v", h.console.revoked, h.store.keys)
	}
	form := h.console.requests("/api/oauth/revoke")[0]
	if form.Get("client_id") != altengineClientID {
		t.Fatalf("revoke form = %v", form)
	}
}

func TestSignOut(t *testing.T) {
	h := newHarness(t)
	h.store.Set(keyEntry, "ae_mine")
	if err := h.pub.SignOut(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := url.Values{"token": {"ae_mine"}, "client_id": {altengineClientID}}
	if got := h.console.requests("/api/oauth/revoke"); len(got) != 1 || got[0].Encode() != want.Encode() {
		t.Fatalf("revoke = %v", got)
	}
	if len(h.store.keys) != 0 {
		t.Fatal("key still stored")
	}

	// Nothing stored (an env key is never revoked).
	h.env[EnvAPIKey] = "ae_env"
	if err := h.pub.SignOut(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.console.requests("/api/oauth/revoke")) != 1 {
		t.Fatal("revoked ALTENGINE_API_KEY")
	}
}

func TestPerSiteFileLimit(t *testing.T) {
	h := newHarness(t)
	h.env[EnvAPIKey] = "ae_env"
	h.api.keys["ae_env"] = true
	h.api.sites["sitegen-blog"] = true
	opts := h.opts(t, map[string]string{"index.html": "x"})
	f, _ := os.Create(filepath.Join(opts.PublicDir, "big.bin"))
	f.Truncate(DefaultMaxFileSize + 1)
	f.Close()

	h.api.maxFile = DefaultMaxFileSize
	_, err := h.pub.Publish(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "/big.bin") || !strings.Contains(err.Error(), "raised to 100 MB") {
		t.Fatalf("err = %v", err)
	}
	if h.api.manifest != nil {
		t.Fatal("sent a manifest over the limit")
	}
}

func TestPublishValidation(t *testing.T) {
	h := newHarness(t)
	opts := h.opts(t, map[string]string{"index.html": "x"})
	opts.Message = strings.Repeat("é", MaxMessageLen+1)
	if _, err := h.pub.Publish(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "200 characters") {
		t.Fatalf("err = %v", err)
	}
	opts.Message = strings.Repeat("é", MaxMessageLen) // runes, not bytes
	opts.Name = "ab"
	if _, err := h.pub.Publish(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "at least 3") {
		t.Fatalf("err = %v", err)
	}
	if h.signIns != 0 {
		t.Fatal("signed in before validating")
	}
}

// The client metadata document sitegen.dev serves must name itself exactly.
func TestClientMetadataDocument(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "site", "src", "altengine-client.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ClientID != altengineClientID {
		t.Fatalf("client_id %q != altengineClientID %q", doc.ClientID, altengineClientID)
	}
	if len(doc.RedirectURIs) != 1 || doc.RedirectURIs[0] != "http://127.0.0.1/callback" || doc.AuthMethod != "none" {
		t.Fatalf("document = %+v", doc)
	}
}

func TestExistingSiteNeedsConfirmation(t *testing.T) {
	h := newHarness(t)
	h.env[EnvAPIKey] = "ae_env"
	h.api.keys["ae_env"] = true
	h.api.sites["sitegen-blog"] = true // another project's, perhaps
	opts := h.opts(t, map[string]string{"index.html": "x"})
	opts.Confirmed = false // the name came from the folder

	_, err := h.pub.Publish(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "sitegen-blog already exists") || !strings.Contains(err.Error(), "-publish-name blog") {
		t.Fatalf("err = %v", err)
	}
	if h.api.manifest != nil {
		t.Fatal("published over an existing site")
	}
	if _, err := os.Stat(filepath.Join(opts.SitePath, ConfigFile)); err == nil {
		t.Fatal("publish.json written")
	}

	// Named explicitly: publish, and remember it.
	opts.Confirmed = true
	if _, err := h.pub.Publish(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "Publishing to the existing site sitegen-blog.") {
		t.Fatalf("output:\n%s", h.out.String())
	}
	if cfg, _ := ReadConfig(opts.SitePath); cfg.Instance != "sitegen-blog" {
		t.Fatalf("publish.json = %+v", cfg)
	}
}

// One sign-in covers every sitegen site on the machine.
func TestOneKeyForManySites(t *testing.T) {
	h := newHarness(t)
	a := h.opts(t, map[string]string{"index.html": "a"})
	a.Name, a.Confirmed = "sitegen-alpha", false
	b := h.opts(t, map[string]string{"index.html": "b"})
	b.Name, b.Confirmed = "sitegen-beta", false
	for _, o := range []Options{a, b} {
		if _, err := h.pub.Publish(context.Background(), o); err != nil {
			t.Fatalf("%s: %v", o.Name, err)
		}
	}
	// Back to the first project: its name now comes from publish.json.
	name, confirmed, err := ResolveName("", a.SitePath)
	if err != nil || name != "sitegen-alpha" || !confirmed {
		t.Fatalf("ResolveName = %q, %v, %v", name, confirmed, err)
	}
	a.Confirmed = confirmed
	if _, err := h.pub.Publish(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if h.signIns != 1 || !h.api.sites["sitegen-alpha"] || !h.api.sites["sitegen-beta"] {
		t.Fatalf("signIns=%d sites=%v", h.signIns, h.api.sites)
	}
}

func TestSignInOnlyForSitegenInstances(t *testing.T) {
	h := newHarness(t)
	opts := h.opts(t, map[string]string{"index.html": "x"})
	opts.Name = "calendar" // e.g. from a hand-edited publish.json
	_, err := h.pub.Publish(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "sitegen-") || !strings.Contains(err.Error(), EnvAPIKey) || h.signIns != 0 {
		t.Fatalf("err=%v signIns=%d", err, h.signIns)
	}
	// With ALTENGINE_API_KEY it's fine.
	h.env[EnvAPIKey] = "ae_env"
	h.api.keys["ae_env"] = true
	h.api.sites["calendar"] = true
	if _, err := h.pub.Publish(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
}
