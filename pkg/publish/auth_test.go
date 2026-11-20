package publish

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPKCE(t *testing.T) {
	// RFC 7636 appendix B.
	if got := challengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge = %s", got)
	}
	v, c, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(v) {
		t.Fatalf("verifier %q is not 32 bytes of unpadded base64url", v)
	}
	if c != challengeS256(v) || strings.Contains(c, "=") {
		t.Fatalf("challenge %q doesn't match verifier", c)
	}
	v2, _, _ := NewPKCE()
	if v == v2 {
		t.Fatal("verifiers repeat")
	}
}

func TestScope(t *testing.T) {
	if Scope != "org:read instances:write static:full:sitegen-*" {
		t.Fatalf("scope = %q", Scope)
	}
	for granted, want := range map[string]bool{
		"org:read instances:write static:full:sitegen-*":    true,
		"instances:write static:full:sitegen-*":             true,
		"instances:write static:full":                       true,
		"org:read static:full:sitegen-*":                    false, // can't create
		"org:read instances:write static:read:sitegen-*":    false, // can't deploy
		"org:read instances:write static:full:sitegen-blog": false,
		"": false,
	} {
		if got := CheckScope(granted); got != want {
			t.Errorf("CheckScope(%q) = %v, want %v", granted, got, want)
		}
	}
}

func callback(t *testing.T, h http.Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/callback?"+query, nil))
	return rec
}

func TestCallbackSuccess(t *testing.T) {
	var gotCode string
	h := newCallbackHandler("s1", func(code string) (*Token, error) {
		gotCode = code
		tok := &Token{AccessToken: "ae_x"}
		tok.AltEngine.OrgName = "Acme <Inc>"
		return tok, nil
	})
	rec := callback(t, h, "code=c1&state=s1")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Connected to Acme &lt;Inc&gt;. You can close this tab.") {
		t.Fatalf("page = %d %s", rec.Code, rec.Body)
	}
	res := <-h.done
	if res.err != nil || res.tok.AccessToken != "ae_x" || gotCode != "c1" {
		t.Fatalf("result = %+v, code %q", res, gotCode)
	}
	// A reload afterwards doesn't exchange again.
	if rec := callback(t, h, "code=c1&state=s1"); rec.Code != 200 || len(h.done) != 0 {
		t.Fatal("second callback produced another result")
	}
}

func TestCallbackCancelled(t *testing.T) {
	h := newCallbackHandler("s1", func(string) (*Token, error) { t.Fatal("exchanged"); return nil, nil })
	rec := callback(t, h, "error=access_denied&state=s1")
	if !strings.Contains(rec.Body.String(), "Cancelled") {
		t.Fatalf("page = %s", rec.Body)
	}
	if res := <-h.done; !errors.Is(res.err, ErrCancelled) {
		t.Fatalf("err = %v", res.err)
	}
}

func TestCallbackError(t *testing.T) {
	h := newCallbackHandler("s1", nil)
	rec := callback(t, h, "error=invalid_scope&error_description=no+such+service&state=s1")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "no such service") {
		t.Fatalf("page = %d %s", rec.Code, rec.Body)
	}
	var oe *OAuthError
	if res := <-h.done; !errors.As(res.err, &oe) || oe.Code != "invalid_scope" {
		t.Fatalf("err = %v", res.err)
	}
}

func TestCallbackWrongState(t *testing.T) {
	h := newCallbackHandler("s1", func(string) (*Token, error) { t.Fatal("exchanged"); return nil, nil })
	for _, q := range []string{"code=c1&state=other", "code=c1", "error=access_denied&state=nope"} {
		if rec := callback(t, h, q); rec.Code != 400 {
			t.Fatalf("%s: status %d", q, rec.Code)
		}
	}
	if len(h.done) != 0 {
		t.Fatal("a wrong state ended the wait")
	}
	// The real callback still works afterwards.
	callback(t, h, "error=access_denied&state=s1")
	if res := <-h.done; !errors.Is(res.err, ErrCancelled) {
		t.Fatalf("err = %v", res.err)
	}
}

// fakeConsole is console.altengine.net's OAuth endpoints.
type fakeConsole struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	forms   map[string][]url.Values // path → requests
	token   func(form url.Values) (int, any)
	device  func(form url.Values) (int, any)
	revoked []string
}

func newFakeConsole(t *testing.T) *fakeConsole {
	f := &fakeConsole{t: t, forms: map[string][]url.Values{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("%s %s with Content-Type %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		r.ParseForm()
		f.mu.Lock()
		f.forms[r.URL.Path] = append(f.forms[r.URL.Path], r.PostForm)
		f.mu.Unlock()
		status, body := 404, any(map[string]string{"error": "not_found"})
		switch r.URL.Path {
		case "/api/oauth/token":
			status, body = f.token(r.PostForm)
		case "/api/oauth/device_authorization":
			status, body = f.device(r.PostForm)
		case "/api/oauth/revoke":
			f.mu.Lock()
			f.revoked = append(f.revoked, r.PostForm.Get("token"))
			f.mu.Unlock()
			status, body = 200, map[string]string{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeConsole) requests(path string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forms[path]
}

func tokenReply(key, scope, org string) map[string]any {
	return map[string]any{
		"access_token": key, "token_type": "Bearer", "scope": scope,
		"altengine": map[string]any{"key_id": "k1", "key_name": "sitegen (host)", "org_id": "o1", "org_name": org, "replaced": false},
	}
}

func oauthErr(code string) (int, any) {
	return 400, map[string]string{"error": code, "error_description": code + " happened"}
}

func TestExchangeCode(t *testing.T) {
	c := newFakeConsole(t)
	c.token = func(form url.Values) (int, any) {
		if form.Get("code") == "used" {
			return oauthErr("invalid_grant")
		}
		return 200, tokenReply("ae_abc", Scope, "Acme")
	}
	a := &Auth{ConsoleURL: c.srv.URL, ClientID: altengineClientID}
	tok, err := a.ExchangeCode(context.Background(), "c1", "http://127.0.0.1:5555/callback", "verif")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "ae_abc" || tok.Scope != Scope || tok.AltEngine.OrgName != "Acme" {
		t.Fatalf("token = %+v", tok)
	}
	got := c.requests("/api/oauth/token")[0]
	want := url.Values{
		"grant_type": {"authorization_code"}, "code": {"c1"}, "client_id": {altengineClientID},
		"redirect_uri": {"http://127.0.0.1:5555/callback"}, "code_verifier": {"verif"},
	}
	if got.Encode() != want.Encode() {
		t.Fatalf("form = %v\nwant  %v", got, want)
	}

	_, err = a.ExchangeCode(context.Background(), "used", "r", "v")
	var oe *OAuthError
	if !errors.As(err, &oe) || oe.Code != "invalid_grant" {
		t.Fatalf("err = %v", err)
	}
}

// TestBrowserSignIn drives the whole flow through a real loopback listener:
// the "browser" follows the authorize URL straight to the redirect.
func TestBrowserSignIn(t *testing.T) {
	c := newFakeConsole(t)
	c.token = func(form url.Values) (int, any) {
		return 200, tokenReply("ae_abc", Scope, "Acme")
	}
	pageCh := make(chan string, 1)
	a := &Auth{
		ConsoleURL: c.srv.URL, ClientID: altengineClientID, DeviceName: "laptop", Out: io.Discard,
		OpenBrowser: func(link string) error {
			u, _ := url.Parse(link)
			q := u.Query()
			if u.Path != "/api/oauth/authorize" || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
				q.Get("scope") != Scope || q.Get("device_name") != "laptop" || q.Get("client_id") != altengineClientID {
				t.Errorf("authorize URL = %s", link)
			}
			if !regexp.MustCompile(`^http://127\.0\.0\.1:\d+/callback$`).MatchString(q.Get("redirect_uri")) {
				t.Errorf("redirect_uri = %s", q.Get("redirect_uri"))
			}
			go func() {
				resp, err := http.Get(q.Get("redirect_uri") + "?code=c1&state=" + url.QueryEscape(q.Get("state")))
				if err != nil {
					t.Error(err)
					return
				}
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				pageCh <- string(b)
			}()
			return nil
		},
	}
	tok, err := a.BrowserSignIn(context.Background(), Scope)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "ae_abc" {
		t.Fatalf("token = %+v", tok)
	}
	form := c.requests("/api/oauth/token")[0]
	if challengeS256(form.Get("code_verifier")) == "" || form.Get("code") != "c1" {
		t.Fatalf("exchange form = %v", form)
	}
	if page := <-pageCh; !strings.Contains(page, "Connected to Acme") {
		t.Fatalf("browser page = %q", page)
	}
}

func TestBrowserSignInCancelled(t *testing.T) {
	a := &Auth{
		ConsoleURL: "http://127.0.0.1:1", ClientID: altengineClientID, Out: io.Discard,
		OpenBrowser: func(link string) error {
			u, _ := url.Parse(link)
			q := u.Query()
			go http.Get(q.Get("redirect_uri") + "?error=access_denied&state=" + url.QueryEscape(q.Get("state")))
			return nil
		},
	}
	start := time.Now()
	_, err := a.BrowserSignIn(context.Background(), Scope)
	if !errors.Is(err, ErrCancelled) || err.Error() != "Cancelled" {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("cancel didn't return at once")
	}
}

func deviceReply(code string) map[string]any {
	return map[string]any{
		"device_code": code, "user_code": "BDWP-HQKT",
		"verification_uri": "https://console.altengine.net/device", "expires_in": 900, "interval": 5,
	}
}

func TestDevicePoll(t *testing.T) {
	c := newFakeConsole(t)
	c.device = func(url.Values) (int, any) { return 200, deviceReply("d1") }
	polls := 0
	c.token = func(form url.Values) (int, any) {
		if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || form.Get("device_code") != "d1" || form.Get("client_id") != altengineClientID {
			t.Errorf("poll form = %v", form)
		}
		polls++
		switch polls {
		case 1:
			return oauthErr("authorization_pending")
		case 2:
			return oauthErr("slow_down")
		case 3:
			return oauthErr("authorization_pending")
		}
		return 200, tokenReply("ae_dev", Scope, "Acme")
	}
	var waits []time.Duration
	var out strings.Builder
	a := &Auth{
		ConsoleURL: c.srv.URL, ClientID: altengineClientID, DeviceName: "box", Out: &out,
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}
	tok, err := a.DeviceSignIn(context.Background(), Scope)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "ae_dev" {
		t.Fatalf("token = %+v", tok)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
	if !strings.Contains(out.String(), "Open https://console.altengine.net/device and enter BDWP-HQKT") {
		t.Fatalf("output = %q", out.String())
	}
	da := c.requests("/api/oauth/device_authorization")[0]
	if da.Get("scope") != Scope || da.Get("device_name") != "box" || da.Get("client_id") != altengineClientID {
		t.Fatalf("device_authorization form = %v", da)
	}
}

func TestDeviceExpiredStartsAgain(t *testing.T) {
	c := newFakeConsole(t)
	n := 0
	c.device = func(url.Values) (int, any) {
		n++
		return 200, deviceReply("d" + string(rune('0'+n)))
	}
	c.token = func(form url.Values) (int, any) {
		if form.Get("device_code") == "d1" {
			return oauthErr("expired_token")
		}
		return 200, tokenReply("ae_dev", Scope, "Acme")
	}
	a := &Auth{ConsoleURL: c.srv.URL, ClientID: altengineClientID, Out: io.Discard,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	tok, err := a.DeviceSignIn(context.Background(), Scope)
	if err != nil || tok.AccessToken != "ae_dev" || n != 2 {
		t.Fatalf("tok=%v err=%v device authorizations=%d", tok, err, n)
	}

	// Always expiring: give up after a few codes.
	c.token = func(url.Values) (int, any) { return oauthErr("expired_token") }
	if _, err := a.DeviceSignIn(context.Background(), Scope); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceDeniedAndUnsupported(t *testing.T) {
	c := newFakeConsole(t)
	c.device = func(url.Values) (int, any) { return 200, deviceReply("d1") }
	c.token = func(url.Values) (int, any) { return oauthErr("access_denied") }
	a := &Auth{ConsoleURL: c.srv.URL, ClientID: altengineClientID, Out: io.Discard,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	if _, err := a.DeviceSignIn(context.Background(), Scope); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v", err)
	}

	c.device = func(url.Values) (int, any) { return oauthErr("unauthorized_client") }
	_, err := a.DeviceSignIn(context.Background(), Scope)
	if err == nil || !strings.Contains(err.Error(), "use the browser sign-in, or set ALTENGINE_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}
