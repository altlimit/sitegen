// Package share exposes a local http.Handler on a public URL through the
// sitegen relay (a Cloudflare Worker with one Durable Object per tunnel).
//
// The client holds a single WebSocket to the relay. Visitor requests arrive
// as JSON "req" messages, are served by the configured Handler in-process,
// and responses go back as a JSON "res" head followed by binary body frames.
// See docs/SHARE_PROTOCOL.md for the wire format.
package share

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ProtocolVersion is the relay wire protocol this client speaks.
const ProtocolVersion = "1"

const (
	// chunkSize is the largest body payload carried in one binary frame.
	chunkSize = 64 << 10
	// window is how many body frames per request may be unacknowledged.
	// The relay returns a credit for every frame its visitor has consumed.
	window = 8

	frameData  = 1
	frameEnd   = 2
	frameHdrSz = 5

	writeTimeout = 15 * time.Second
	pingInterval = 25 * time.Second
	pingTimeout  = 15 * time.Second
	readLimit    = 1 << 20
)

var (
	// ErrUpgradeRequired means the relay no longer speaks this client's
	// protocol version. Reconnecting will not help; sitegen must be updated.
	ErrUpgradeRequired = errors.New("the share relay requires a newer sitegen; please upgrade")

	errResumeRejected = errors.New("resume rejected")
)

// Client tunnels an http.Handler through the relay.
type Client struct {
	RelayURL  string
	Handler   http.Handler // handler to serve requests (e.g. server.ShareHandler)
	BasicAuth string       // "user:pass" or "" for no auth

	mu      sync.Mutex
	sess    *session
	tunnel  string // assigned tunnel ID, kept across reconnects
	token   string // resume token for tunnel
	pubURL  string
	version string
}

// New creates a share client. relayURL is a ws://, wss://, http:// or
// https:// URL; a bare host is treated as wss://host.
func New(relayURL string, handler http.Handler, basicAuth string) *Client {
	return &Client{RelayURL: relayURL, Handler: handler, BasicAuth: basicAuth}
}

// ConnectURL normalises a -share-server value into the relay's connect URL.
func ConnectURL(relay string) (string, error) {
	if !strings.Contains(relay, "://") {
		if _, _, err := splitHostPortLoose(relay); err == nil {
			return "", fmt.Errorf("share server %q looks like an old host:port relay address; use a URL such as wss://relay.sitegen.dev", relay)
		}
		relay = "wss://" + relay
	}
	u, err := url.Parse(relay)
	if err != nil {
		return "", fmt.Errorf("invalid share server URL: %w", err)
	}
	switch u.Scheme {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported share server scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("share server URL %q has no host", relay)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/connect"
	}
	return u.String(), nil
}

func splitHostPortLoose(s string) (string, string, error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", errors.New("no port")
	}
	for _, r := range s[i+1:] {
		if r < '0' || r > '9' {
			return "", "", errors.New("not a port")
		}
	}
	return s[:i], s[i+1:], nil
}

// Connect dials the relay and registers the tunnel, resuming the previous
// tunnel ID when possible. It returns the public URL.
func (c *Client) Connect(ctx context.Context, version string) (string, error) {
	connectURL, err := ConnectURL(c.RelayURL)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.version = version
	tunnel, token := c.tunnel, c.token
	c.mu.Unlock()

	pubURL, err := c.dial(ctx, connectURL, version, tunnel, token)
	if errors.Is(err, errResumeRejected) {
		// The relay forgot us (expired or redeployed storage): start fresh.
		c.mu.Lock()
		c.tunnel, c.token = "", ""
		c.mu.Unlock()
		pubURL, err = c.dial(ctx, connectURL, version, "", "")
	}
	return pubURL, err
}

type wireMsg struct {
	Type    string      `json:"type"`
	ID      uint32      `json:"id,omitempty"`
	Tunnel  string      `json:"tunnel,omitempty"`
	URL     string      `json:"url,omitempty"`
	Token   string      `json:"token,omitempty"`
	Method  string      `json:"method,omitempty"`
	Path    string      `json:"path,omitempty"`
	Status  int         `json:"status,omitempty"`
	Headers [][2]string `json:"headers,omitempty"`
	N       int         `json:"n,omitempty"`
}

func (c *Client) dial(ctx context.Context, connectURL, version, tunnel, token string) (string, error) {
	h := http.Header{}
	h.Set("Sitegen-Protocol", ProtocolVersion)
	h.Set("Sitegen-Version", version)
	if c.BasicAuth != "" {
		h.Set("Sitegen-Share-Auth", base64.StdEncoding.EncodeToString([]byte(c.BasicAuth)))
	}
	if tunnel != "" && token != "" {
		h.Set("Sitegen-Resume", tunnel+":"+token)
	}

	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(dctx, connectURL, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		if resp != nil {
			var msg string
			if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
				msg = strings.TrimSpace(string(body))
			}
			switch resp.StatusCode {
			case http.StatusUpgradeRequired:
				return "", ErrUpgradeRequired
			case http.StatusUnauthorized, http.StatusConflict, http.StatusGone:
				if tunnel != "" {
					return "", errResumeRejected
				}
			}
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			return "", fmt.Errorf("relay refused connection: %d %s", resp.StatusCode, msg)
		}
		return "", fmt.Errorf("dial relay: %w", err)
	}
	conn.SetReadLimit(readLimit)

	// The relay's first message is always "ready".
	typ, data, err := conn.Read(dctx)
	if err != nil {
		conn.CloseNow()
		return "", fmt.Errorf("read ready: %w", err)
	}
	var m wireMsg
	if typ != websocket.MessageText || json.Unmarshal(data, &m) != nil || m.Type != "ready" || m.URL == "" {
		conn.Close(websocket.StatusProtocolError, "expected ready")
		return "", errors.New("relay sent an unexpected greeting")
	}

	s := newSession(conn, c.Handler)
	c.mu.Lock()
	c.sess = s
	c.tunnel, c.token, c.pubURL = m.Tunnel, m.Token, m.URL
	c.mu.Unlock()
	return m.URL, nil
}

// Run serves relay requests until the context is cancelled or the
// connection drops.
func (c *Client) Run(ctx context.Context) error {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s == nil {
		return errors.New("not connected")
	}
	return s.run(ctx)
}

// Close shuts down the current connection.
func (c *Client) Close() error {
	c.mu.Lock()
	s := c.sess
	c.sess = nil
	c.mu.Unlock()
	if s != nil {
		return s.conn.Close(websocket.StatusNormalClosure, "")
	}
	return nil
}

// Notify asks the relay to tell connected viewers to reload. It is a no-op
// while disconnected and never blocks for long.
func (c *Client) Notify() {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s != nil {
		s.sendJSON(wireMsg{Type: "reload"})
	}
}

// URL returns the current public URL, or "" before the first connect.
func (c *Client) URL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pubURL
}

// RunWithReconnect keeps the tunnel up, reconnecting with backoff. onConnect
// receives the public URL each time a connection is established (it is
// normally the same URL after a reconnect). onDisconnect receives the reason
// whenever the tunnel goes down. It returns when ctx is cancelled or the
// relay reports ErrUpgradeRequired (which is passed to onDisconnect first).
func (c *Client) RunWithReconnect(ctx context.Context, version string, onConnect func(url string), onDisconnect func(err error)) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for ctx.Err() == nil {
		pubURL, err := c.Connect(ctx, version)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			onDisconnect(err)
			if errors.Is(err, ErrUpgradeRequired) {
				return
			}
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		onConnect(pubURL)

		err = c.Run(ctx)
		c.Close()
		if ctx.Err() != nil {
			return
		}
		onDisconnect(err)
		if !sleepCtx(ctx, time.Second) {
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// SplitBasicAuth splits a "user:pass" string.
func SplitBasicAuth(auth string) (user, pass string, ok bool) {
	parts := strings.SplitN(auth, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// session is one live WebSocket to the relay.
type session struct {
	conn    *websocket.Conn
	handler http.Handler
	ctx     context.Context
	cancel  context.CancelFunc

	mu   sync.Mutex
	reqs map[uint32]*inflight
}

type inflight struct {
	cancel  context.CancelFunc
	credits chan struct{}
}

func newSession(conn *websocket.Conn, h http.Handler) *session {
	ctx, cancel := context.WithCancel(context.Background())
	return &session{conn: conn, handler: h, ctx: ctx, cancel: cancel, reqs: map[uint32]*inflight{}}
}

func (s *session) run(ctx context.Context) error {
	defer s.cancel()
	stop := context.AfterFunc(ctx, func() { s.conn.Close(websocket.StatusGoingAway, "client shutting down") })
	defer stop()
	go s.keepalive()

	for {
		typ, data, err := s.conn.Read(s.ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("relay connection lost: %w", err)
		}
		if typ != websocket.MessageText {
			continue // the relay sends no binary frames to the client
		}
		var m wireMsg
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch m.Type {
		case "req":
			s.startRequest(m)
		case "credit":
			s.mu.Lock()
			f := s.reqs[m.ID]
			s.mu.Unlock()
			if f != nil {
				for i := 0; i < m.N; i++ {
					select {
					case f.credits <- struct{}{}:
					default:
					}
				}
			}
		case "cancel":
			s.mu.Lock()
			f := s.reqs[m.ID]
			s.mu.Unlock()
			if f != nil {
				f.cancel()
			}
		}
	}
}

// keepalive sends WebSocket pings. The relay platform answers them without
// waking the Durable Object, so an idle tunnel costs nothing.
func (s *session) keepalive() {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(s.ctx, pingTimeout)
			err := s.conn.Ping(pctx)
			cancel()
			if err != nil {
				s.conn.CloseNow()
				return
			}
		}
	}
}

func (s *session) sendJSON(m wireMsg) error {
	b, _ := json.Marshal(m)
	return s.write(websocket.MessageText, b)
}

// write uses the session context: in coder/websocket any write error
// (including a context expiry) closes the whole connection, so per-request
// contexts must never reach here.
func (s *session) write(typ websocket.MessageType, b []byte) error {
	ctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
	defer cancel()
	return s.conn.Write(ctx, typ, b)
}

func (s *session) startRequest(m wireMsg) {
	s.mu.Lock()
	if _, dup := s.reqs[m.ID]; dup {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	f := &inflight{cancel: cancel, credits: make(chan struct{}, 4*window)}
	for i := 0; i < window; i++ {
		f.credits <- struct{}{}
	}
	s.reqs[m.ID] = f
	s.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.reqs, m.ID)
			s.mu.Unlock()
		}()
		s.serve(ctx, m, f)
	}()
}

func (s *session) serve(ctx context.Context, m wireMsg, f *inflight) {
	w := &tunnelWriter{s: s, id: m.ID, f: f, ctx: ctx, header: http.Header{}, head: m.Method == http.MethodHead}

	req, err := buildRequest(ctx, m)
	if err != nil {
		w.header.Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request\n"))
		w.finish()
		return
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				if !w.wroteHeader {
					w.header = http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("internal server error\n"))
				} else {
					w.failed = true
				}
			}
		}()
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handler.ServeHTTP(w, req)
	}()
	w.finish()
}

func buildRequest(ctx context.Context, m wireMsg) (*http.Request, error) {
	if !strings.HasPrefix(m.Path, "/") {
		return nil, errors.New("path must be absolute")
	}
	u, err := url.ParseRequestURI(m.Path)
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	for _, kv := range m.Headers {
		h.Add(kv[0], kv[1])
	}
	host := h.Get("Host")
	h.Del("Host")
	remote := "relay"
	if xff := h.Get("X-Forwarded-For"); xff != "" {
		remote = strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	r := &http.Request{
		Method:     m.Method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     h,
		Host:       host,
		RequestURI: m.Path,
		Body:       http.NoBody,
		RemoteAddr: remote,
	}
	return r.WithContext(ctx), nil
}

// tunnelWriter is the http.ResponseWriter handed to the local handler.
type tunnelWriter struct {
	s      *session
	id     uint32
	f      *inflight
	ctx    context.Context
	header http.Header
	head   bool

	wroteHeader bool
	status      int
	buf         []byte
	err         error
	failed      bool
}

func (w *tunnelWriter) Header() http.Header { return w.header }

func (w *tunnelWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	var hs [][2]string
	for k, vv := range w.header {
		for _, v := range vv {
			hs = append(hs, [2]string{k, v})
		}
	}
	if err := w.s.sendJSON(wireMsg{Type: "res", ID: w.id, Status: status, Headers: hs}); err != nil {
		w.err = err
	}
}

func (w *tunnelWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		if w.header.Get("Content-Type") == "" {
			w.header.Set("Content-Type", http.DetectContentType(p))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.err != nil {
		return 0, w.err
	}
	if w.head {
		return len(p), nil
	}
	n := len(p)
	w.buf = append(w.buf, p...)
	for len(w.buf) >= chunkSize {
		if err := w.sendChunk(w.buf[:chunkSize]); err != nil {
			return 0, err
		}
		w.buf = w.buf[chunkSize:]
	}
	return n, nil
}

// Flush sends buffered body bytes immediately.
func (w *tunnelWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.err == nil && len(w.buf) > 0 {
		if err := w.sendChunk(w.buf); err == nil {
			w.buf = w.buf[:0]
		}
	}
}

func (w *tunnelWriter) sendChunk(p []byte) error {
	select {
	case <-w.f.credits:
	case <-w.ctx.Done():
		w.err = w.ctx.Err()
		return w.err
	}
	frame := make([]byte, frameHdrSz+len(p))
	frame[0] = frameData
	binary.BigEndian.PutUint32(frame[1:5], w.id)
	copy(frame[frameHdrSz:], p)
	if err := w.s.write(websocket.MessageBinary, frame); err != nil {
		w.err = err
		return err
	}
	return nil
}

func (w *tunnelWriter) finish() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.err == nil && !w.failed && len(w.buf) > 0 {
		w.sendChunk(w.buf)
		w.buf = nil
	}
	if w.ctx.Err() != nil && w.s.ctx.Err() == nil {
		// Cancelled by the relay: it has already discarded this request.
		return
	}
	if w.failed || w.err != nil {
		w.s.sendJSON(wireMsg{Type: "cancel", ID: w.id})
		return
	}
	end := make([]byte, frameHdrSz)
	end[0] = frameEnd
	binary.BigEndian.PutUint32(end[1:5], w.id)
	w.s.write(websocket.MessageBinary, end)
}
