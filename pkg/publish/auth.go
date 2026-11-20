package publish

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrCancelled is returned when the person declines the sign-in.
var ErrCancelled = errors.New("Cancelled")

// signInTimeout bounds the browser flow: the authorization code is only valid
// for 10 minutes anyway.
const signInTimeout = 10 * time.Minute

// Token is the token endpoint's reply. AccessToken is an ordinary altengine API
// key (ae_…) with no expiry and no refresh token.
type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	AltEngine   struct {
		KeyName string `json:"key_name"`
		OrgName string `json:"org_name"`
	} `json:"altengine"`
}

// OAuthError is an RFC 6749 error reply ({error, error_description}).
type OAuthError struct {
	Status      int
	Code        string
	Description string
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return e.Code
}

// Auth runs "Sign in with altengine" against the console.
type Auth struct {
	ConsoleURL string
	ClientID   string
	DeviceName string
	HTTP       *http.Client
	Out        io.Writer
	// OpenBrowser opens a URL in the system browser. Failures are ignored: the
	// link is always printed too.
	OpenBrowser func(string) error
	// Sleep waits between device-flow polls; tests replace it.
	Sleep func(context.Context, time.Duration) error
}

// Scope is what sitegen asks for: create static instances named sitegen-…
// and deploy to them, and nothing else in the organization. One key per
// machine then covers every sitegen site in the org. (A key per site would not
// work: signing in again from the same device replaces the earlier key.)
const Scope = "org:read instances:write static:full:" + InstancePrefix + "*"

// CheckScope verifies the granted scope still lets the key create and deploy
// sitegen sites; the person's role can cut what was requested.
func CheckScope(granted string) bool {
	has := map[string]bool{}
	for _, s := range strings.Fields(granted) {
		has[s] = true
	}
	static := has["static:full:"+InstancePrefix+"*"] || has["static:full"]
	return has["instances:write"] && static
}

// NewPKCE returns an S256 code verifier and its challenge.
func NewPKCE() (verifier, challenge string, err error) {
	verifier, err = randomString(32)
	if err != nil {
		return "", "", err
	}
	return verifier, challengeS256(verifier), nil
}

func challengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// randomString is n random bytes, base64url without padding.
func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthorizeURL is the consent page the browser is sent to.
func (a *Auth) AuthorizeURL(redirect, challenge, state, scope string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {a.ClientID},
		"redirect_uri":          {redirect},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"scope":                 {scope},
	}
	if a.DeviceName != "" {
		q.Set("device_name", a.DeviceName)
	}
	return a.ConsoleURL + "/api/oauth/authorize?" + q.Encode()
}

// ExchangeCode trades an authorization code for a key. redirect must be
// identical to the one sent to the authorize endpoint.
func (a *Auth) ExchangeCode(ctx context.Context, code, redirect, verifier string) (*Token, error) {
	var tok Token
	err := a.postForm(ctx, "/api/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {a.ClientID},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	}, &tok)
	if err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, errors.New("altengine sign-in: token reply has no access_token")
	}
	return &tok, nil
}

// Revoke revokes a key issued to this client. altengine always answers 200.
func (a *Auth) Revoke(ctx context.Context, key string) error {
	return a.postForm(ctx, "/api/oauth/revoke", url.Values{
		"token":     {key},
		"client_id": {a.ClientID},
	}, nil)
}

func (a *Auth) postForm(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.ConsoleURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		oe := &OAuthError{Status: resp.StatusCode}
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			oe.Code, oe.Description = e.Error, e.Description
		} else {
			oe.Code = resp.Status
			oe.Description = strings.TrimSpace(string(body))
		}
		return oe
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("altengine %s: bad reply: %w", path, err)
	}
	return nil
}

func (a *Auth) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (a *Auth) printf(format string, args ...any) {
	if a.Out != nil {
		fmt.Fprintf(a.Out, format, args...)
	}
}

// BrowserSignIn runs the authorization-code flow with PKCE and a loopback
// redirect, and returns the issued key.
func (a *Auth) BrowserSignIn(ctx context.Context, scope string) (*Token, error) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomString(24)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("altengine sign-in: %w", err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	ctx, cancel := context.WithTimeout(ctx, signInTimeout)
	defer cancel()

	h := newCallbackHandler(state, func(code string) (*Token, error) {
		return a.ExchangeCode(ctx, code, redirect, verifier)
	})
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer func() {
		// Let the final page reach the browser, then stop listening.
		sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer scancel()
		srv.Shutdown(sctx)
	}()

	link := a.AuthorizeURL(redirect, challenge, state, scope)
	a.printf("Opening your browser to sign in to altengine. If it doesn't open, visit:\n  %s\n", link)
	if a.OpenBrowser != nil {
		a.OpenBrowser(link)
	}

	select {
	case res := <-h.done:
		return res.tok, res.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("altengine sign-in timed out after 10 minutes")
		}
		return nil, ctx.Err()
	}
}

type callbackResult struct {
	tok *Token
	err error
}

// callbackHandler answers the loopback redirect. A wrong state is refused
// without ending the wait (it isn't our request); anything else ends it.
type callbackHandler struct {
	state    string
	exchange func(code string) (*Token, error)
	done     chan callbackResult

	mu       sync.Mutex
	finished bool
}

func newCallbackHandler(state string, exchange func(string) (*Token, error)) *callbackHandler {
	return &callbackHandler{state: state, exchange: exchange, done: make(chan callbackResult, 1)}
}

func (h *callbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/callback" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if q.Get("state") != h.state {
		writePage(w, http.StatusBadRequest, "Sign-in failed", "This sign-in link doesn't match the one sitegen started. Run sitegen -publish again.")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		writePage(w, http.StatusOK, "Already done", "You can close this tab.")
		return
	}
	h.finished = true

	var res callbackResult
	switch e := q.Get("error"); {
	case e == "access_denied":
		res.err = ErrCancelled
		writePage(w, http.StatusOK, "Cancelled", "Nothing was published. You can close this tab.")
	case e != "":
		res.err = &OAuthError{Code: e, Description: q.Get("error_description")}
		writePage(w, http.StatusBadRequest, "Sign-in failed", res.err.Error())
	case q.Get("code") == "":
		res.err = errors.New("altengine sign-in: callback without a code")
		writePage(w, http.StatusBadRequest, "Sign-in failed", "The callback carried no code.")
	default:
		res.tok, res.err = h.exchange(q.Get("code"))
		if res.err != nil {
			writePage(w, http.StatusBadGateway, "Sign-in failed", res.err.Error())
		} else {
			writePage(w, http.StatusOK, "Connected", "Connected to "+res.tok.AltEngine.OrgName+". You can close this tab.")
		}
	}
	h.done <- res
}

func writePage(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>sitegen: %[1]s</title>`+
		`<body style="font:16px system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem"><h1>%[1]s</h1><p>%[2]s</p>`,
		html.EscapeString(title), html.EscapeString(msg))
}

type deviceAuthorization struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// deviceAttempts bounds how many times an expired code is replaced.
const deviceAttempts = 3

// DeviceSignIn runs the device authorization flow: the person enters a code
// on another device.
func (a *Auth) DeviceSignIn(ctx context.Context, scope string) (*Token, error) {
	for attempt := 0; attempt < deviceAttempts; attempt++ {
		var da deviceAuthorization
		form := url.Values{"client_id": {a.ClientID}, "scope": {scope}}
		if a.DeviceName != "" {
			form.Set("device_name", a.DeviceName)
		}
		if err := a.postForm(ctx, "/api/oauth/device_authorization", form, &da); err != nil {
			var oe *OAuthError
			if errors.As(err, &oe) && oe.Code == "unauthorized_client" {
				return nil, errors.New("device sign-in isn't enabled for sitegen on altengine yet; use the browser sign-in, or set ALTENGINE_API_KEY")
			}
			return nil, fmt.Errorf("altengine device sign-in: %w", err)
		}
		if attempt > 0 {
			a.printf("That code expired; here is a new one.\n")
		}
		a.printf("Open %s and enter %s\n", da.VerificationURI, da.UserCode)

		tok, err := a.pollDevice(ctx, da)
		if errors.Is(err, errExpired) {
			continue
		}
		return tok, err
	}
	return nil, errors.New("altengine device sign-in: the code expired")
}

var errExpired = errors.New("expired_token")

func (a *Auth) pollDevice(ctx context.Context, da deviceAuthorization) (*Token, error) {
	interval := time.Duration(da.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	sleep := a.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	for {
		if err := sleep(ctx, interval); err != nil {
			return nil, err
		}
		var tok Token
		err := a.postForm(ctx, "/api/oauth/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {da.DeviceCode},
			"client_id":   {a.ClientID},
		}, &tok)
		if err == nil {
			if tok.AccessToken == "" {
				return nil, errors.New("altengine sign-in: token reply has no access_token")
			}
			return &tok, nil
		}
		var oe *OAuthError
		if !errors.As(err, &oe) {
			return nil, err
		}
		switch oe.Code {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return nil, ErrCancelled
		case "expired_token":
			return nil, errExpired
		default:
			return nil, fmt.Errorf("altengine device sign-in: %w", err)
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// OpenBrowser opens url in the system browser.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
