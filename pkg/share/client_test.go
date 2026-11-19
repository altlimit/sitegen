package share

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeRelay is a minimal in-process relay that speaks protocol v1.
type fakeRelay struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	headers []http.Header
	reject  func(n int, h http.Header) int // optional: status to refuse the n-th dial with
	conns   chan *websocket.Conn
}

func newFakeRelay(t *testing.T) *fakeRelay {
	f := &fakeRelay{t: t, conns: make(chan *websocket.Conn, 4)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		n := len(f.headers)
		reject := f.reject
		f.mu.Unlock()
		if reject != nil {
			if code := reject(n, r.Header); code != 0 {
				http.Error(w, "refused", code)
				return
			}
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(32 << 20) // matches Cloudflare's inbound message limit
		ready, _ := json.Marshal(wireMsg{Type: "ready", Tunnel: "abcdefghijkl", URL: "https://abcdefghijkl.example.test", Token: "tok" + string(rune('0'+n))})
		c.Write(r.Context(), websocket.MessageText, ready)
		f.conns <- c
		<-r.Context().Done()
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelay) url() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

func (f *fakeRelay) header(i int) http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headers[i]
}

func (f *fakeRelay) accept(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-f.conns:
		t.Cleanup(func() { c.CloseNow() })
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("client never connected")
		return nil
	}
}

func send(t *testing.T, c *websocket.Conn, m wireMsg) {
	t.Helper()
	b, _ := json.Marshal(m)
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatalf("relay write: %v", err)
	}
}

type frame struct {
	text *wireMsg
	kind byte
	id   uint32
	data []byte
}

func recv(t *testing.T, c *websocket.Conn, timeout time.Duration) (frame, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil {
		return frame{}, err
	}
	if typ == websocket.MessageText {
		var m wireMsg
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("bad json from client: %s", data)
		}
		return frame{text: &m}, nil
	}
	if len(data) < frameHdrSz {
		t.Fatalf("short binary frame")
	}
	return frame{kind: data[0], id: binary.BigEndian.Uint32(data[1:5]), data: data[5:]}, nil
}

func startClient(t *testing.T, relay string, h http.Handler, auth string) (*Client, string) {
	t.Helper()
	c := New(relay, h, auth)
	ctx, cancel := context.WithCancel(context.Background())
	urls := make(chan string, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.RunWithReconnect(ctx, "test", func(u string) { urls <- u }, func(error) {})
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case u := <-urls:
		return c, u
	case <-time.After(5 * time.Second):
		t.Fatal("no onConnect")
		return nil, ""
	}
}

// readResponse collects res + data frames + end for request id, granting
// credits as it goes.
func readResponse(t *testing.T, c *websocket.Conn, id uint32) (int, http.Header, []byte) {
	t.Helper()
	var status int
	h := http.Header{}
	var body bytes.Buffer
	for {
		f, err := recv(t, c, 5*time.Second)
		if err != nil {
			t.Fatalf("waiting for response: %v", err)
		}
		switch {
		case f.text != nil && f.text.Type == "res" && f.text.ID == id:
			status = f.text.Status
			for _, kv := range f.text.Headers {
				h.Add(kv[0], kv[1])
			}
		case f.kind == frameData && f.id == id:
			body.Write(f.data)
			send(t, c, wireMsg{Type: "credit", ID: id, N: 1})
		case f.kind == frameEnd && f.id == id:
			return status, h, body.Bytes()
		case f.text != nil && f.text.Type == "cancel":
			t.Fatalf("client cancelled request %d", f.text.ID)
		}
	}
}

func TestConnectHandshake(t *testing.T) {
	relay := newFakeRelay(t)
	_, u := startClient(t, relay.url(), http.NotFoundHandler(), "user:pa:ss")
	relay.accept(t)
	if u != "https://abcdefghijkl.example.test" {
		t.Fatalf("url = %q", u)
	}
	h := relay.header(0)
	if h.Get("Sitegen-Protocol") != ProtocolVersion || h.Get("Sitegen-Version") != "test" {
		t.Fatalf("missing protocol headers: %v", h)
	}
	auth, _ := base64.StdEncoding.DecodeString(h.Get("Sitegen-Share-Auth"))
	if string(auth) != "user:pa:ss" {
		t.Fatalf("share auth = %q", auth)
	}
	if h.Get("Sitegen-Resume") != "" {
		t.Fatal("first connect must not resume")
	}
}

func TestRequestRoundTrip(t *testing.T) {
	relay := newFakeRelay(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" || r.URL.RawQuery != "x=1" || r.Host != "abc.example.test" || r.Header.Get("Accept") != "text/html" {
			t.Errorf("unexpected request: %s %s host=%s %v", r.Method, r.URL, r.Host, r.Header)
		}
		if r.RemoteAddr != "203.0.113.9" {
			t.Errorf("RemoteAddr = %q", r.RemoteAddr)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(201)
		w.Write([]byte("hi there"))
	})
	startClient(t, relay.url(), h, "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 7, Method: "GET", Path: "/hello?x=1", Headers: [][2]string{
		{"Host", "abc.example.test"}, {"Accept", "text/html"}, {"X-Forwarded-For", "203.0.113.9"},
	}})
	status, hdr, body := readResponse(t, c, 7)
	if status != 201 || string(body) != "hi there" || hdr.Get("Content-Type") != "text/plain" {
		t.Fatalf("got %d %v %q", status, hdr, body)
	}
	if got := hdr.Values("Set-Cookie"); len(got) != 2 {
		t.Fatalf("Set-Cookie values = %v", got)
	}
}

func TestHeadSendsNoBody(t *testing.T) {
	relay := newFakeRelay(t)
	startClient(t, relay.url(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body that must not be sent"))
	}), "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 1, Method: "HEAD", Path: "/"})
	status, _, body := readResponse(t, c, 1)
	if status != 200 || len(body) != 0 {
		t.Fatalf("HEAD got %d with %d body bytes", status, len(body))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	relay := newFakeRelay(t)
	startClient(t, relay.url(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler must not run for POST")
	}), "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 2, Method: "POST", Path: "/"})
	if status, _, _ := readResponse(t, c, 2); status != 405 {
		t.Fatalf("status = %d", status)
	}
}

func TestPanicBecomes500(t *testing.T) {
	relay := newFakeRelay(t)
	startClient(t, relay.url(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}), "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 3, Method: "GET", Path: "/"})
	if status, _, _ := readResponse(t, c, 3); status != 500 {
		t.Fatalf("status = %d", status)
	}
}

func TestFlowControl(t *testing.T) {
	relay := newFakeRelay(t)
	payload := bytes.Repeat([]byte("0123456789abcdef"), (chunkSize*(window+4))/16)
	startClient(t, relay.url(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}), "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 9, Method: "GET", Path: "/big"})

	var body bytes.Buffer
	chunks := 0
	for chunks < window {
		f, err := recv(t, c, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if f.kind == frameData {
			if len(f.data) > chunkSize {
				t.Fatalf("chunk of %d bytes exceeds %d", len(f.data), chunkSize)
			}
			body.Write(f.data)
			chunks++
		}
	}
	// Without credit the client must stop after `window` chunks.
	if _, err := recv(t, c, 300*time.Millisecond); err == nil {
		t.Fatal("client sent more than the window without credit")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFlowControlCompletes(t *testing.T) {
	relay := newFakeRelay(t)
	payload := bytes.Repeat([]byte("x"), chunkSize*(window*3)+123)
	startClient(t, relay.url(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}), "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 4, Method: "GET", Path: "/big"})
	_, _, body := readResponse(t, c, 4)
	if !bytes.Equal(body, payload) {
		t.Fatalf("body length %d, want %d", len(body), len(payload))
	}
}

func TestCancelStopsHandler(t *testing.T) {
	relay := newFakeRelay(t)
	cancelled := make(chan struct{})
	startClient(t, relay.url(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}), "")
	c := relay.accept(t)
	send(t, c, wireMsg{Type: "req", ID: 5, Method: "GET", Path: "/slow"})
	if f, err := recv(t, c, 5*time.Second); err != nil || f.text == nil || f.text.Type != "res" {
		t.Fatalf("expected res head, got %+v %v", f, err)
	}
	send(t, c, wireMsg{Type: "cancel", ID: 5})
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("handler context was not cancelled")
	}
	// The connection must survive a cancelled request.
	send(t, c, wireMsg{Type: "req", ID: 6, Method: "HEAD", Path: "/"})
	go func() { <-cancelled }()
}

func TestNotifySendsReload(t *testing.T) {
	relay := newFakeRelay(t)
	client, _ := startClient(t, relay.url(), http.NotFoundHandler(), "")
	c := relay.accept(t)
	client.Notify()
	f, err := recv(t, c, 5*time.Second)
	if err != nil || f.text == nil || f.text.Type != "reload" {
		t.Fatalf("expected reload, got %+v %v", f, err)
	}
}

func TestReconnectResumes(t *testing.T) {
	relay := newFakeRelay(t)
	startClient(t, relay.url(), http.NotFoundHandler(), "")
	c := relay.accept(t)
	c.Close(websocket.StatusGoingAway, "relay restart")
	relay.accept(t)
	if got := relay.header(1).Get("Sitegen-Resume"); got != "abcdefghijkl:tok1" {
		t.Fatalf("Sitegen-Resume = %q", got)
	}
}

func TestResumeRejectedStartsFresh(t *testing.T) {
	relay := newFakeRelay(t)
	relay.reject = func(n int, h http.Header) int {
		if h.Get("Sitegen-Resume") != "" {
			return http.StatusUnauthorized
		}
		return 0
	}
	startClient(t, relay.url(), http.NotFoundHandler(), "")
	c := relay.accept(t)
	c.Close(websocket.StatusGoingAway, "")
	relay.accept(t)
	if relay.header(1).Get("Sitegen-Resume") == "" {
		t.Fatal("second dial should have tried to resume")
	}
	if relay.header(2).Get("Sitegen-Resume") != "" {
		t.Fatal("third dial should start fresh")
	}
}

func TestUpgradeRequiredIsFatal(t *testing.T) {
	relay := newFakeRelay(t)
	relay.reject = func(int, http.Header) int { return http.StatusUpgradeRequired }
	c := New(relay.url(), http.NotFoundHandler(), "")
	var got error
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.RunWithReconnect(context.Background(), "test", func(string) { t.Error("should not connect") }, func(err error) { got = err })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithReconnect kept retrying after 426")
	}
	if !errors.Is(got, ErrUpgradeRequired) {
		t.Fatalf("err = %v", got)
	}
}

func TestConnectURL(t *testing.T) {
	cases := map[string]string{
		"wss://relay.sitegen.dev":    "wss://relay.sitegen.dev/connect",
		"https://relay.sitegen.dev/": "wss://relay.sitegen.dev/connect",
		"http://localhost:8787":      "ws://localhost:8787/connect",
		"ws://localhost:8787/custom": "ws://localhost:8787/custom",
		"relay.example.com":          "wss://relay.example.com/connect",
	}
	for in, want := range cases {
		got, err := ConnectURL(in)
		if err != nil || got != want {
			t.Errorf("ConnectURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"sitegen.dev:9443", "ftp://x", "wss://"} {
		if _, err := ConnectURL(bad); err == nil {
			t.Errorf("ConnectURL(%q) should fail", bad)
		}
	}
}
