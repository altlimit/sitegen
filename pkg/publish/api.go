package publish

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrUnauthorized is a 401: the key was revoked or has expired.
	ErrUnauthorized = errors.New("altengine refused the key (401): it was revoked or has expired")
	// ErrNotFound is a 404 from the API.
	ErrNotFound = errors.New("not found")
)

// APIError is any other non-2xx reply from api.altengine.net.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" {
		return fmt.Sprintf("altengine: %s (%d %s)", msg, e.Status, e.Code)
	}
	return fmt.Sprintf("altengine: %s (%d)", msg, e.Status)
}

// API is a minimal client for api.altengine.net, authenticated with a key.
type API struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
}

// Site is GET /v1/static/<name>/site.
type Site struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

// FileEntry is one manifest entry.
type FileEntry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// Upload is one file altengine doesn't have yet. PathHash is the content hash.
type Upload struct {
	PathHash        string            `json:"path_hash"`
	Size            int64             `json:"size"`
	UploadURL       string            `json:"upload_url"`
	Method          string            `json:"method"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

// Deployment is the reply to POST /deployments.
type Deployment struct {
	ID           flexString `json:"deployment_id"`
	FileCount    int        `json:"file_count"`
	TotalBytes   int64      `json:"total_bytes"`
	MissingCount int        `json:"missing_count"`
	Uploads      []Upload   `json:"uploads"`
	Cursor       flexString `json:"cursor"`
}

// UploadPage is the reply to GET /deployments/<id>/uploads.
type UploadPage struct {
	Uploads []Upload   `json:"uploads"`
	Cursor  flexString `json:"cursor"`
}

// Activation is the reply to POST /deployments/<id>/activate.
type Activation struct {
	ID         flexString `json:"deployment_id"`
	FileCount  int        `json:"file_count"`
	TotalBytes int64      `json:"total_bytes"`
	URL        string     `json:"url"`
}

// flexString decodes a JSON string or number; null and absent are "".
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("expected a string, number or null, got %s", b)
	}
	*f = flexString(n.String())
	return nil
}

func (c *API) staticPath(name, rest string) string {
	return "/v1/static/" + url.PathEscape(name) + rest
}

// GetSite returns the static instance, or ErrNotFound.
func (c *API) GetSite(ctx context.Context, name string) (*Site, error) {
	var s Site
	if err := c.do(ctx, http.MethodGet, c.staticPath(name, "/site"), nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateDeployment sends the manifest.
func (c *API) CreateDeployment(ctx context.Context, name string, files map[string]FileEntry, message string) (*Deployment, error) {
	body := map[string]any{"files": files}
	if message != "" {
		body["message"] = message
	}
	var d Deployment
	if err := c.do(ctx, http.MethodPost, c.staticPath(name, "/deployments"), body, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, errors.New("altengine: deployment reply has no deployment_id")
	}
	return &d, nil
}

// NextUploads fetches the next page of upload links.
func (c *API) NextUploads(ctx context.Context, name, id, cursor string) (*UploadPage, error) {
	var p UploadPage
	path := c.staticPath(name, "/deployments/"+url.PathEscape(id)+"/uploads?cursor="+url.QueryEscape(cursor))
	if err := c.do(ctx, http.MethodGet, path, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Activate makes the deployment live.
func (c *API) Activate(ctx context.Context, name, id string) (*Activation, error) {
	var a Activation
	if err := c.do(ctx, http.MethodPost, c.staticPath(name, "/deployments/"+url.PathEscape(id)+"/activate"), nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateInstance creates the static instance <name> with one stateless MCP
// tools/call.
func (c *API) CreateInstance(ctx context.Context, name string) error {
	_, err := c.CallTool(ctx, "create_instance", map[string]any{"service": "static", "name": name})
	return err
}

// MaxFileBytes reads the static instance's per-file limit.
func (c *API) MaxFileBytes(ctx context.Context, name string) (int64, error) {
	raw, err := c.CallTool(ctx, "get_instance_config", map[string]any{"service": "static", "instance": name})
	if err != nil {
		return 0, err
	}
	var cfg struct {
		Config struct {
			MaxFileBytes int64 `json:"maxFileBytes"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Config.MaxFileBytes <= 0 {
		return 0, errors.New("altengine: instance config has no maxFileBytes")
	}
	return cfg.Config.MaxFileBytes, nil
}

// CallTool runs one MCP tool and returns its structured result. A result
// with isError is a refusal and is returned as an error.
func (c *API) CallTool(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	}
	var rpc struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result *struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
			Content           []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, "/mcp", req, &rpc); err != nil {
		return nil, err
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("altengine %s: %s (%d)", tool, rpc.Error.Message, rpc.Error.Code)
	}
	if rpc.Result == nil {
		return nil, fmt.Errorf("altengine %s: reply has no result", tool)
	}
	text := ""
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	if rpc.Result.IsError {
		if text == "" {
			text = "refused"
		}
		return nil, fmt.Errorf("altengine %s: %s", tool, text)
	}
	if len(rpc.Result.StructuredContent) > 0 && string(rpc.Result.StructuredContent) != "null" {
		return rpc.Result.StructuredContent, nil
	}
	if json.Valid([]byte(text)) {
		return json.RawMessage(text), nil
	}
	return nil, fmt.Errorf("altengine %s: reply isn't JSON: %s", tool, text)
}

// maxRateLimitRetries is how often a 429 is waited out before giving up.
const maxRateLimitRetries = 3

func (c *API) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = b
	}
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Accept", "application/json, text/event-stream")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return ErrUnauthorized
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusTooManyRequests && attempt < maxRateLimitRetries:
			if err := sleepCtx(ctx, retryAfter(resp.Header.Get("Retry-After"))); err != nil {
				return err
			}
			continue
		case resp.StatusCode/100 != 2:
			return apiError(resp.StatusCode, data)
		}
		if out == nil {
			return nil
		}
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			data = lastSSEData(data)
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("altengine %s %s: bad reply: %w", method, path, err)
		}
		return nil
	}
}

func (c *API) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s >= 0 {
		return min(time.Duration(s)*time.Second, 30*time.Second)
	}
	return 2 * time.Second
}

// lastSSEData returns the last event's data from an event-stream body, which
// is how a streamable-HTTP MCP server may answer a single request.
func lastSSEData(body []byte) []byte {
	var last, cur []byte
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			if cur != nil {
				cur = append(cur, '\n')
			}
			cur = append(cur, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
		case line == "":
			if cur != nil {
				last, cur = cur, nil
			}
		}
	}
	if cur != nil {
		last = cur
	}
	return last
}

// apiError extracts a message from the common error shapes:
// {"error":{"code","message"}}, {"error":"…","message":"…"} or {"message":"…"}.
func apiError(status int, body []byte) error {
	e := &APIError{Status: status}
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		switch v := m["error"].(type) {
		case map[string]any:
			e.Code, _ = v["code"].(string)
			e.Message, _ = v["message"].(string)
		case string:
			e.Code = v
		}
		if msg, ok := m["message"].(string); ok && e.Message == "" {
			e.Message = msg
		}
		if msg, ok := m["error_description"].(string); ok && e.Message == "" {
			e.Message = msg
		}
		if c, ok := m["code"].(string); ok && e.Code == "" {
			e.Code = c
		}
	} else if s := strings.TrimSpace(string(body)); s != "" && len(s) < 300 {
		e.Message = s
	}
	return e
}
