// Package publish deploys a built site to altengine static hosting, signing
// the person in with "Sign in with altengine" when there is no key.
//
// Contract: https://www.altengine.net/docs/sign-in-with-altengine/ and
// https://www.altengine.net/docs/static/ ("the deploy API").
package publish

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// altengineClientID is sitegen's client metadata document. altengine fetches
// it without following redirects, so it must be this exact final URL, and the
// document's client_id must equal it character for character. It is served
// from site/src/altengine-client.json.
const altengineClientID = "https://sitegen.dev/altengine-client.json"

const (
	DefaultConsoleURL = "https://console.altengine.net"
	DefaultAPIURL     = "https://api.altengine.net"
	// EnvAPIKey, when set, is used as the key; sitegen never prompts and
	// never stores it.
	EnvAPIKey = "ALTENGINE_API_KEY"
)

// Options is one publish.
type Options struct {
	Name      string // the static instance, normally sitegen-<site name>
	Confirmed bool   // Name came from -publish-name or publish.json, not a guess
	SitePath  string // where publish.json lives
	PublicDir string // the built site
	Message   string // optional note on the deployment
	Device    bool   // sign in with the device flow
}

// Result is a finished publish.
type Result struct {
	FileCount int
	Uploaded  int
	URL       string
}

// Publisher holds the endpoints and machine-local state a publish uses.
type Publisher struct {
	ConsoleURL  string
	APIURL      string
	ClientID    string
	DeviceName  string
	HTTP        *http.Client // API and console calls; nil uses defaults
	UploadHTTP  *http.Client // signed upload PUTs; nil uses a client without a timeout
	Store       KeyStore
	Getenv      func(string) string
	Out         io.Writer
	OpenBrowser func(string) error

	// SignIn replaces the interactive sign-in (tests).
	SignIn func(ctx context.Context, scope string, device bool) (*Token, error)
}

// New returns a Publisher for altengine's production hosts.
func New(out io.Writer) *Publisher {
	host, _ := os.Hostname()
	if host == "" {
		host = "sitegen"
	}
	if len(host) > 64 {
		host = host[:64]
	}
	return &Publisher{
		ConsoleURL:  DefaultConsoleURL,
		APIURL:      DefaultAPIURL,
		ClientID:    altengineClientID,
		DeviceName:  host,
		Store:       NewSystemStore(),
		Getenv:      os.Getenv,
		Out:         out,
		OpenBrowser: OpenBrowser,
	}
}

type keySource int

const (
	keyFromEnv keySource = iota
	keyStored
	keySignedIn
)

func (p *Publisher) printf(format string, args ...any) {
	if p.Out != nil {
		fmt.Fprintf(p.Out, format, args...)
	}
}

func (p *Publisher) auth() *Auth {
	return &Auth{
		ConsoleURL:  p.ConsoleURL,
		ClientID:    p.ClientID,
		DeviceName:  p.DeviceName,
		HTTP:        p.HTTP,
		Out:         p.Out,
		OpenBrowser: p.OpenBrowser,
	}
}

// resolveKey picks the key: ALTENGINE_API_KEY, then the one stored on this
// machine, then a new sign-in.
func (p *Publisher) resolveKey(ctx context.Context, name string, device bool) (string, keySource, error) {
	if key := strings.TrimSpace(p.Getenv(EnvAPIKey)); key != "" {
		return key, keyFromEnv, nil
	}
	key, err := p.Store.Get(keyEntry)
	if err == nil {
		return key, keyStored, nil
	}
	if !errors.Is(err, ErrNoKey) {
		p.printf("Warning: couldn't read the stored altengine key: %v\n", err)
	}
	key, err = p.signIn(ctx, name, device)
	return key, keySignedIn, err
}

// signIn gets a new key, checks what was granted, and stores it.
func (p *Publisher) signIn(ctx context.Context, name string, device bool) (string, error) {
	if ci := p.Getenv("CI"); ci != "" && ci != "false" && ci != "0" {
		return "", fmt.Errorf("no altengine key: set %s (sitegen can't open a sign-in in CI)", EnvAPIKey)
	}
	if !strings.HasPrefix(name, InstancePrefix) {
		return "", fmt.Errorf("signing in covers sites named %s…, and %q isn't one; publish it with %s", InstancePrefix, name, EnvAPIKey)
	}
	scope := Scope
	var (
		tok *Token
		err error
	)
	switch {
	case p.SignIn != nil:
		tok, err = p.SignIn(ctx, scope, device)
	case device:
		tok, err = p.auth().DeviceSignIn(ctx, scope)
	default:
		tok, err = p.auth().BrowserSignIn(ctx, scope)
	}
	if err != nil {
		return "", err
	}
	org := tok.AltEngine.OrgName
	if org == "" {
		org = "your organization"
	}
	if !CheckScope(tok.Scope) {
		// Useless key: don't leave it behind.
		_ = p.auth().Revoke(ctx, tok.AccessToken)
		return "", fmt.Errorf("Your role in %s can't create or deploy this site; ask an owner, or use %s.", org, EnvAPIKey)
	}
	p.printf("Signed in to altengine: %s\n", org)
	if err := p.Store.Set(keyEntry, tok.AccessToken); err != nil {
		p.printf("Warning: couldn't save the altengine key (%v); you'll be asked to sign in next time.\n", err)
	}
	return tok.AccessToken, nil
}

// Publish uploads opts.PublicDir to the static instance opts.Name and makes
// it live, creating the instance first if it doesn't exist.
func (p *Publisher) Publish(ctx context.Context, opts Options) (*Result, error) {
	if len([]rune(opts.Message)) > MaxMessageLen {
		return nil, fmt.Errorf("-publish-message is longer than %d characters", MaxMessageLen)
	}
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}
	// Check the limits before anyone is asked to sign in.
	m, err := BuildManifest(opts.PublicDir)
	if err != nil {
		return nil, err
	}
	key, src, err := p.resolveKey(ctx, opts.Name, opts.Device)
	if err != nil {
		return nil, err
	}
	retried := false
	for {
		res, err := p.publishWith(ctx, key, opts, m)
		if !errors.Is(err, ErrUnauthorized) {
			return res, err
		}
		switch {
		case src == keyFromEnv:
			return nil, fmt.Errorf("%s: %w", EnvAPIKey, err)
		case src == keyStored && !retried:
			retried = true
			p.printf("The stored altengine key no longer works; signing in again.\n")
			if derr := p.Store.Delete(keyEntry); derr != nil {
				p.printf("Warning: couldn't remove the old key: %v\n", derr)
			}
			if key, err = p.signIn(ctx, opts.Name, opts.Device); err != nil {
				return nil, err
			}
			src = keySignedIn
		default:
			return nil, err
		}
	}
}

func (p *Publisher) publishWith(ctx context.Context, key string, opts Options, m *Manifest) (*Result, error) {
	api := &API{BaseURL: p.APIURL, Key: key, HTTP: p.HTTP}
	name := opts.Name

	cfg, _ := ReadConfig(opts.SitePath)
	_, err := api.GetSite(ctx, name)
	switch {
	case err == nil && !opts.Confirmed:
		// The name is only a guess from the folder; don't publish over a site
		// that may be another project's.
		return nil, fmt.Errorf("%s already exists on altengine. If it's this site, publish with -publish-name %s; otherwise pick another name with -publish-name",
			name, strings.TrimPrefix(name, InstancePrefix))
	case err == nil && opts.SitePath != "" && cfg.Instance != name:
		p.printf("Publishing to the existing site %s.\n", name)
	case errors.Is(err, ErrNotFound):
		p.printf("Creating the static site %q on altengine...\n", name)
		if err := api.CreateInstance(ctx, name); err != nil {
			return nil, err
		}
		if _, err = api.GetSite(ctx, name); errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("altengine created %q but doesn't find it yet; try again in a moment", name)
		}
	}
	if err != nil {
		return nil, err
	}
	if opts.SitePath != "" && cfg.Instance != name {
		if err := WriteConfig(opts.SitePath, Config{Instance: name}); err != nil {
			p.printf("Warning: couldn't write %s: %v\n", ConfigFile, err)
		}
	}

	// Files over the default limit are fine if the site raised its own.
	if m.Files[m.Largest].Size > DefaultMaxFileSize {
		limit, err := api.MaxFileBytes(ctx, name)
		if errors.Is(err, ErrUnauthorized) {
			return nil, err
		}
		if err == nil {
			if err := m.CheckFileSize(limit); err != nil {
				return nil, err
			}
		}
		// Unknown limit: altengine enforces it on the deployment.
	}

	d, err := api.CreateDeployment(ctx, name, m.Files, opts.Message)
	if err != nil {
		return nil, deployErr(err)
	}
	if d.MissingCount > 0 {
		p.printf("Uploading %d of %d files...\n", d.MissingCount, len(m.Files))
	}
	up := &uploader{HTTP: p.uploadHTTP(), Local: m.Local}
	uploaded, err := up.upload(ctx, d.Uploads)
	if err != nil {
		return nil, err
	}
	// Never assume the first reply listed every missing file.
	for cursor := string(d.Cursor); cursor != ""; {
		page, err := api.NextUploads(ctx, name, string(d.ID), cursor)
		if err != nil {
			return nil, deployErr(err)
		}
		n, err := up.upload(ctx, page.Uploads)
		uploaded += n
		if err != nil {
			return nil, err
		}
		cursor = string(page.Cursor)
	}

	act, err := api.Activate(ctx, name, string(d.ID))
	if err != nil {
		return nil, deployErr(err)
	}
	res := &Result{FileCount: act.FileCount, Uploaded: uploaded, URL: act.URL}
	if res.FileCount == 0 {
		res.FileCount = len(m.Files)
	}
	if uploaded == 0 {
		p.printf("Nothing changed; published again → %s\n", res.URL)
	} else {
		p.printf("Published %d files (%d uploaded) → %s\n", res.FileCount, uploaded, res.URL)
	}
	return res, nil
}

func deployErr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return errors.New("altengine: the deployment or site disappeared while publishing; run sitegen -publish again")
	}
	return err
}

func (p *Publisher) uploadHTTP() *http.Client {
	if p.UploadHTTP != nil {
		return p.UploadHTTP
	}
	return &http.Client{}
}

// SignOut revokes this machine's stored key and forgets it. A key from
// ALTENGINE_API_KEY is never touched.
func (p *Publisher) SignOut(ctx context.Context) error {
	key, err := p.Store.Get(keyEntry)
	if errors.Is(err, ErrNoKey) {
		p.printf("sitegen isn't signed in to altengine on this machine.\n")
		if p.Getenv(EnvAPIKey) != "" {
			p.printf("%s is set; sitegen never stores or revokes it.\n", EnvAPIKey)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err := p.auth().Revoke(ctx, key); err != nil {
		return fmt.Errorf("couldn't revoke the key (it is still stored): %w", err)
	}
	if err := p.Store.Delete(keyEntry); err != nil {
		return fmt.Errorf("revoked the key, but couldn't remove it from this machine: %w", err)
	}
	p.printf("Signed out of altengine: the key is revoked and removed from this machine.\n")
	return nil
}
