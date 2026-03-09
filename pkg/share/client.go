package share

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Client tunnels a local HTTP server through a relay to expose it publicly.
type Client struct {
	RelayAddr string
	Handler   http.Handler // handler to serve requests (e.g. StaticServer)
	BasicAuth string       // "user:pass" or "" for no auth

	mu        sync.Mutex
	session   *yamux.Session
	subdomain string
}

// Registration is the JSON message sent to the relay on connect.
type Registration struct {
	Version   string `json:"version"`
	BasicAuth string `json:"basic_auth,omitempty"`
}

// Assignment is the JSON response from the relay after registration.
type Assignment struct {
	Subdomain string `json:"subdomain,omitempty"`
	Error     string `json:"error,omitempty"`
}

// New creates a new share client.
// handler is the http.Handler to serve (typically a StaticServer).
// basicAuth should be "user:pass" or empty for no auth.
func New(relayAddr string, handler http.Handler, basicAuth string) *Client {
	return &Client{
		RelayAddr: relayAddr,
		Handler:   handler,
		BasicAuth: basicAuth,
	}
}

// Connect dials the relay, registers, and establishes a yamux session.
// Returns the assigned subdomain on success.
func (c *Client) Connect(ctx context.Context, version string) (string, error) {
	// Extract hostname for TLS ServerName
	host := c.RelayAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 10 * time.Second},
		Config: &tls.Config{
			ServerName: host,
		},
	}
	conn, err := dialer.DialContext(ctx, "tcp", c.RelayAddr)
	if err != nil {
		return "", fmt.Errorf("dial relay: %w", err)
	}

	// Send registration
	reg := Registration{
		Version:   version,
		BasicAuth: c.BasicAuth,
	}
	if err := json.NewEncoder(conn).Encode(reg); err != nil {
		conn.Close()
		return "", fmt.Errorf("send registration: %w", err)
	}

	// Read assignment
	var assign Assignment
	if err := json.NewDecoder(conn).Decode(&assign); err != nil {
		conn.Close()
		return "", fmt.Errorf("read assignment: %w", err)
	}
	if assign.Error != "" {
		conn.Close()
		return "", fmt.Errorf("relay error: %s", assign.Error)
	}

	// Establish yamux session (client side acts as yamux server to accept streams from relay)
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.StreamOpenTimeout = 10 * time.Second
	cfg.LogOutput = io.Discard

	session, err := yamux.Server(conn, cfg)
	if err != nil {
		conn.Close()
		return "", fmt.Errorf("yamux session: %w", err)
	}

	c.mu.Lock()
	c.session = session
	c.subdomain = assign.Subdomain
	c.mu.Unlock()

	return assign.Subdomain, nil
}

// Run serves HTTP requests from the relay using the configured Handler.
// It creates an http.Server backed by a yamux-based net.Listener.
// Blocks until the context is cancelled or the session dies.
func (c *Client) Run(ctx context.Context) error {
	c.mu.Lock()
	session := c.session
	c.mu.Unlock()

	if session == nil {
		return fmt.Errorf("not connected")
	}

	ln := &yamuxListener{session: session}
	srv := &http.Server{
		Handler:      methodFilter(c.Handler),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // no write timeout — needed for SSE streaming
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return ctx.Err()
	}
	return err
}

// Close shuts down the yamux session.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

// RunWithReconnect runs the client with automatic reconnection on failure.
// It calls onConnect with the subdomain each time a connection is established,
// and onDisconnect when the connection drops.
func (c *Client) RunWithReconnect(ctx context.Context, version string, onConnect func(subdomain string), onDisconnect func(err error)) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		subdomain, err := c.Connect(ctx, version)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			onDisconnect(fmt.Errorf("connect failed: %w", err))
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		// Reset backoff on successful connect
		backoff = time.Second
		onConnect(subdomain)

		err = c.Run(ctx)
		if ctx.Err() != nil {
			return
		}

		c.Close()
		onDisconnect(err)

		// Brief pause before reconnect
		time.Sleep(time.Second)
	}
}

// Subdomain returns the currently assigned subdomain.
func (c *Client) Subdomain() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subdomain
}

// yamuxListener adapts a yamux.Session into a net.Listener.
type yamuxListener struct {
	session *yamux.Session
}

func (l *yamuxListener) Accept() (net.Conn, error) {
	return l.session.AcceptStream()
}

func (l *yamuxListener) Close() error {
	return l.session.Close()
}

func (l *yamuxListener) Addr() net.Addr {
	return l.session.LocalAddr()
}

// methodFilter wraps a handler to only allow GET and HEAD requests.
func methodFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SplitBasicAuth splits a "user:pass" string.
func SplitBasicAuth(auth string) (user, pass string, ok bool) {
	parts := strings.SplitN(auth, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func init() {
	// Suppress yamux log noise
	_ = log.Default()
}
