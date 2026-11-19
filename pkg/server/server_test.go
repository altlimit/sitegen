package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *StaticServer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>hi</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	ss := NewStaticServer(dir, "/")
	ss.CMSEnabled = true
	ss.SrcDir = dir
	ss.DataDir = dir
	return ss
}

func TestShareHandlerBlocksDevEndpoints(t *testing.T) {
	h := ShareHandler(newTestServer(t))
	for _, p := range []string{"/__cms", "/__cms/api/sources", "/__cms/api/source?path=index.html", "/__hotreload"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s through share: status %d, want 404", p, rec.Code)
		}
	}
}

func TestShareHandlerInjectsWebSocketReload(t *testing.T) {
	ss := newTestServer(t)

	rec := httptest.NewRecorder()
	ShareHandler(ss).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `new WebSocket(`) || strings.Contains(body, "EventSource") {
		t.Fatalf("shared page should use the WebSocket reload script:\n%s", body)
	}

	rec = httptest.NewRecorder()
	ss.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body = rec.Body.String()
	if !strings.Contains(body, "EventSource") || strings.Contains(body, "new WebSocket(") {
		t.Fatalf("local page should keep the SSE reload script:\n%s", body)
	}
}

func TestLocalCMSStillServed(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer(t).ServeHTTP(rec, httptest.NewRequest("GET", "/__cms", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("local /__cms status %d", rec.Code)
	}
}

func TestSubscribeReceivesRebuilds(t *testing.T) {
	ss := newTestServer(t)
	ch, unsubscribe := ss.Subscribe()
	defer unsubscribe()
	// Registration is processed asynchronously by Listen, so keep notifying
	// until the subscriber is in the fan-out.
	deadline := time.After(2 * time.Second)
	for got := false; !got; {
		select {
		case ss.Notifier <- []byte("updated"):
		default:
		}
		select {
		case msg := <-ch:
			if string(msg) != "updated" {
				t.Fatalf("got %q", msg)
			}
			got = true
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("no rebuild notification")
		}
	}
	unsubscribe()
	unsubscribe() // must be safe to call twice
}
