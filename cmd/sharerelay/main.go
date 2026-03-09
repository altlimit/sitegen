package main

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"golang.org/x/time/rate"
)

const (
	maxLogSize  = 10 * 1024 * 1024 // 10MB
	maxLogFiles = 5
)

// rotatingWriter wraps a log file and rotates it when it exceeds
// maxLogSize, keeping at most maxLogFiles old copies (.1 through .5).
type rotatingWriter struct {
	mu      sync.Mutex
	f       *os.File
	path    string
	written int64
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.written+int64(len(p)) > maxLogSize {
		w.rotate()
	}
	n, err := w.f.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() {
	w.f.Close()
	// Shift existing rotated files: .5 is deleted, .4->.5, ... .1->.2
	for i := maxLogFiles; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		if i == maxLogFiles {
			os.Remove(src)
		} else {
			dst := fmt.Sprintf("%s.%d", w.path, i+1)
			os.Rename(src, dst)
		}
	}
	// Current file becomes .1
	os.Rename(w.path, w.path+".1")
	// Open a fresh file
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		// fallback: reopen append
		f, _ = os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	}
	w.f = f
	w.written = 0
}

// tunnel represents a connected client tunnel.
type tunnel struct {
	session   *yamux.Session
	basicAuth string // "user:pass" or "" for no auth
	limiter   *rate.Limiter
	clientIP  string
}

// relay holds all state for the relay server.
type relay struct {
	mu      sync.RWMutex
	tunnels map[string]*tunnel // subdomain -> tunnel

	ipMu       sync.Mutex
	ipSessions map[string]int // client IP -> active session count

	maxSessionsPerIP int
	domain           string
}

func newRelay(domain string, maxSessionsPerIP int) *relay {
	r := &relay{
		tunnels:          make(map[string]*tunnel),
		ipSessions:       make(map[string]int),
		maxSessionsPerIP: maxSessionsPerIP,
		domain:           domain,
	}
	go r.reapIdleSessions()
	return r
}

func (r *relay) reapIdleSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		for sub, t := range r.tunnels {
			if t.session.IsClosed() {
				r.removeTunnelLocked(sub)
			}
		}
		r.mu.Unlock()
	}
}

func (r *relay) removeTunnelLocked(subdomain string) {
	t, ok := r.tunnels[subdomain]
	if !ok {
		return
	}
	t.session.Close()
	delete(r.tunnels, subdomain)

	if r.maxSessionsPerIP > 0 {
		r.ipMu.Lock()
		r.ipSessions[t.clientIP]--
		if r.ipSessions[t.clientIP] <= 0 {
			delete(r.ipSessions, t.clientIP)
		}
		r.ipMu.Unlock()
	}

	log.Printf("[relay] removed tunnel %s (client: %s)", subdomain, t.clientIP)
}

func (r *relay) removeTunnel(subdomain string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeTunnelLocked(subdomain)
}

func generateSubdomain() string {
	b := make([]byte, 4) // 8 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}

// handleClient handles a new TCP connection from a sitegen client.
func (r *relay) handleClient(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[relay] panic in handleClient: %v", r)
		}
	}()

	clientIP := conn.RemoteAddr().(*net.TCPAddr).IP.String()

	// Set read deadline for registration
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Read registration
	var reg struct {
		Version   string `json:"version"`
		BasicAuth string `json:"basic_auth,omitempty"`
	}

	decoder := json.NewDecoder(io.LimitReader(conn, 8192)) // Max 8KB registration
	if err := decoder.Decode(&reg); err != nil {
		sendAssignment(conn, "", fmt.Sprintf("invalid registration: %v", err))
		conn.Close()
		return
	}

	// Clear deadline
	conn.SetReadDeadline(time.Time{})

	// Check per-IP session limit
	if r.maxSessionsPerIP > 0 {
		r.ipMu.Lock()
		count := r.ipSessions[clientIP]
		if count >= r.maxSessionsPerIP {
			r.ipMu.Unlock()
			sendAssignment(conn, "", fmt.Sprintf("too many sessions from this IP (max %d)", r.maxSessionsPerIP))
			conn.Close()
			return
		}
		r.ipSessions[clientIP]++
		r.ipMu.Unlock()
	}

	// Generate unique subdomain
	var subdomain string
	for i := 0; i < 10; i++ {
		candidate := generateSubdomain()
		r.mu.RLock()
		_, exists := r.tunnels[candidate]
		r.mu.RUnlock()
		if !exists {
			subdomain = candidate
			break
		}
	}
	if subdomain == "" {
		if r.maxSessionsPerIP > 0 {
			r.ipMu.Lock()
			r.ipSessions[clientIP]--
			r.ipMu.Unlock()
		}
		sendAssignment(conn, "", "failed to generate subdomain")
		conn.Close()
		return
	}

	// Send assignment
	if err := sendAssignment(conn, subdomain, ""); err != nil {
		if r.maxSessionsPerIP > 0 {
			r.ipMu.Lock()
			r.ipSessions[clientIP]--
			r.ipMu.Unlock()
		}
		conn.Close()
		return
	}

	// Setup yamux (relay acts as client to open streams toward sitegen)
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.StreamOpenTimeout = 10 * time.Second
	cfg.LogOutput = io.Discard

	session, err := yamux.Client(conn, cfg)
	if err != nil {
		if r.maxSessionsPerIP > 0 {
			r.ipMu.Lock()
			r.ipSessions[clientIP]--
			r.ipMu.Unlock()
		}
		log.Printf("[relay] yamux session failed for %s: %v", clientIP, err)
		conn.Close()
		return
	}

	t := &tunnel{
		session:   session,
		basicAuth: reg.BasicAuth,
		limiter:   rate.NewLimiter(50, 100), // 50 req/s, burst 100
		clientIP:  clientIP,
	}

	r.mu.Lock()
	r.tunnels[subdomain] = t
	r.mu.Unlock()

	log.Printf("[relay] new tunnel %s.%s (client: %s, auth: %v)", subdomain, r.domain, clientIP, reg.BasicAuth != "")

	// Wait for session to close
	go func() {
		<-session.CloseChan()
		r.removeTunnel(subdomain)
	}()
}

func sendAssignment(conn net.Conn, subdomain, errMsg string) error {
	resp := struct {
		Subdomain string `json:"subdomain,omitempty"`
		Error     string `json:"error,omitempty"`
	}{
		Subdomain: subdomain,
		Error:     errMsg,
	}
	return json.NewEncoder(conn).Encode(resp)
}

// ServeHTTP handles public HTTP requests and routes them through tunnels.
func (r *relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Only allow GET and HEAD
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// Extract subdomain from Host
	host := req.Host
	if i := strings.LastIndex(host, ":"); i != -1 {
		host = host[:i]
	}
	subdomain := ""
	if r.domain != "" {
		suffix := "." + r.domain
		if strings.HasSuffix(host, suffix) {
			subdomain = strings.TrimSuffix(host, suffix)
		}
	}
	// Fallback: first segment
	if subdomain == "" {
		parts := strings.SplitN(host, ".", 2)
		subdomain = parts[0]
	}

	if subdomain == "" {
		slog.Error("tunnel missing subdomain", "host", host)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// Lookup tunnel
	r.mu.RLock()
	t, ok := r.tunnels[subdomain]
	r.mu.RUnlock()

	if !ok || t.session.IsClosed() {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Rate limiting
	if !t.limiter.Allow() {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}

	// Basic auth check
	if t.basicAuth != "" {
		user, pass, hasAuth := req.BasicAuth()
		if !hasAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="sitegen share"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		parts := strings.SplitN(t.basicAuth, ":", 2)
		if len(parts) != 2 || subtle.ConstantTimeCompare([]byte(user), []byte(parts[0])) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(parts[1])) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="sitegen share"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	}

	// Open yamux stream to client
	stream, err := t.session.OpenStream()
	if err != nil {
		slog.Error("tunnel open stream error", "error", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	// Set stream deadline (will be cleared for SSE)
	stream.SetDeadline(time.Now().Add(30 * time.Second))

	// Forward the HTTP request through the stream
	rawReq, err := httputil.DumpRequest(req, false) // No body for GET
	if err != nil {
		slog.Error("tunnel dump request error", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Check request size
	if len(rawReq) > 8192 {
		http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
		return
	}

	if _, err := stream.Write(rawReq); err != nil {
		slog.Error("tunnel write error", "error", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}

	// Read HTTP response from stream
	resp, err := http.ReadResponse(bufio.NewReader(stream), req)
	if err != nil {
		slog.Error("tunnel read error", "error", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	isSSE := resp.Header.Get("Content-Type") == "text/event-stream"
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// For SSE, clear deadline and flush chunks as they arrive
	if isSSE {
		stream.SetDeadline(time.Time{})
		flusher, ok := w.(http.Flusher)
		if !ok {
			io.Copy(w, resp.Body)
			return
		}
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				flusher.Flush()
			}
			if err != nil {
				return
			}
		}
	} else {
		io.Copy(w, io.LimitReader(resp.Body, 50*1024*1024)) // 50MB max
	}
}

func main() {
	var (
		clientPort       int
		httpPort         int
		domain           string
		maxSessionsPerIP int
		tlsCert          string
		tlsKey           string
		logPath          string
	)

	flag.IntVar(&clientPort, "client-port", 9443, "TCP port for client tunnel connections")
	flag.IntVar(&httpPort, "http-port", 8080, "HTTP port for public requests")
	flag.StringVar(&domain, "domain", "sitegen.dev", "Base domain for subdomains")
	flag.IntVar(&maxSessionsPerIP, "max-sessions-per-ip", 5, "Max concurrent sessions per client IP (0 to disable)")
	flag.StringVar(&tlsCert, "tls-cert", "", "TLS certificate file for client tunnel listener")
	flag.StringVar(&tlsKey, "tls-key", "", "TLS private key file for client tunnel listener")
	flag.StringVar(&logPath, "log", "", "Path to log file (max 10MB, truncates when full)")
	flag.Parse()

	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("failed to open log file: %v", err)
		}
		defer f.Close()
		info, _ := f.Stat()
		lw := &rotatingWriter{f: f, path: logPath, written: info.Size()}
		log.SetOutput(lw)
		slog.SetDefault(slog.New(slog.NewTextHandler(lw, nil)))
	}

	r := newRelay(domain, maxSessionsPerIP)

	// Start client listener
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", clientPort))
	if err != nil {
		log.Fatalf("failed to listen on :%d: %v", clientPort, err)
	}

	// Wrap in TLS if certs provided
	if tlsCert != "" && tlsKey != "" {
		cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
		if err != nil {
			log.Fatalf("failed to load TLS cert/key: %v", err)
		}
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
		ln = tls.NewListener(ln, tlsCfg)
		log.Printf("[relay] client listener on :%d (TLS)", clientPort)
	} else {
		log.Printf("[relay] client listener on :%d (no TLS - dev mode)", clientPort)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Printf("[relay] accept error: %v", err)
				continue
			}
			go r.handleClient(conn)
		}
	}()

	// Start HTTP server
	log.Printf("[relay] HTTP listener on :%d (domain: *.%s)", httpPort, domain)
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", httpPort),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8192,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}
