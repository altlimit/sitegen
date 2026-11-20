package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// ErrNoKey means no key is stored for the instance.
var ErrNoKey = errors.New("no stored key")

// KeyStore keeps altengine keys on this machine, by entry name.
type KeyStore interface {
	Get(name string) (string, error) // ErrNoKey when absent
	Set(name, key string) error
	Delete(name string) error
}

const keyringService = "sitegen-altengine"

// keyEntry is the store entry for the machine's key, which covers every
// sitegen-* site in the organization it was issued for.
const keyEntry = "default"

// SystemStore uses the OS keyring, and a 0600 JSON file where there is none
// (headless Linux).
type SystemStore struct {
	// File is the fallback, os.UserConfigDir()/sitegen/altengine.json.
	File string
	// Keyring can be turned off (tests, or a keyring that misbehaves).
	NoKeyring bool

	mu sync.Mutex
}

// NewSystemStore returns the default store.
func NewSystemStore() *SystemStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return &SystemStore{File: filepath.Join(dir, "sitegen", "altengine.json")}
}

func (s *SystemStore) Get(name string) (string, error) {
	if !s.NoKeyring {
		if key, err := keyring.Get(keyringService, name); err == nil && key != "" {
			return key, nil
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readFile()
	if err != nil {
		return "", err
	}
	if key := keys[name]; key != "" {
		return key, nil
	}
	return "", ErrNoKey
}

func (s *SystemStore) Set(name, key string) error {
	if !s.NoKeyring {
		if err := keyring.Set(keyringService, name, key); err == nil {
			// Don't leave an older copy in the file.
			s.mu.Lock()
			defer s.mu.Unlock()
			if keys, err := s.readFile(); err == nil {
				if _, ok := keys[name]; ok {
					delete(keys, name)
					return s.writeFile(keys)
				}
			}
			return nil
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readFile()
	if err != nil {
		return err
	}
	keys[name] = key
	return s.writeFile(keys)
}

func (s *SystemStore) Delete(name string) error {
	if !s.NoKeyring {
		// Not found, or no keyring at all: either way the file is checked next.
		_ = keyring.Delete(keyringService, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readFile()
	if err != nil {
		return err
	}
	if _, ok := keys[name]; !ok {
		return nil
	}
	delete(keys, name)
	return s.writeFile(keys)
}

func (s *SystemStore) readFile() (map[string]string, error) {
	keys := map[string]string{}
	b, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		return keys, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return keys, nil
	}
	if err := json.Unmarshal(b, &keys); err != nil {
		return nil, fmt.Errorf("%s: %w", s.File, err)
	}
	return keys, nil
}

func (s *SystemStore) writeFile(keys map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.File), 0o700); err != nil {
		return err
	}
	if len(keys) == 0 {
		err := os.Remove(s.File)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.File), ".altengine-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.File)
}

// ConfigFile is site/publish.json. It holds no secret and is meant to be
// committed, so teammates and CI publish to the same site.
const ConfigFile = "publish.json"

// Config is the content of publish.json.
type Config struct {
	Instance string `json:"instance"`
}

// ReadConfig reads <sitePath>/publish.json; a missing file is an empty Config.
func ReadConfig(sitePath string) (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(sitePath, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", filepath.Join(sitePath, ConfigFile), err)
	}
	return c, nil
}

// WriteConfig writes <sitePath>/publish.json.
func WriteConfig(sitePath string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sitePath, ConfigFile), append(b, '\n'), 0o644)
}

var (
	invalidNameChars = regexp.MustCompile(`[^a-z0-9-]`)
	repeatedDashes   = regexp.MustCompile(`-{2,}`)
	validName        = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// SanitizeName lowercases s and turns anything outside a-z 0-9 - into "-".
// Runs of "-" are collapsed (altengine hostnames may not contain "--") and
// trimmed from the ends.
func SanitizeName(s string) string {
	s = invalidNameChars.ReplaceAllString(strings.ToLower(s), "-")
	s = repeatedDashes.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// InstancePrefix starts the name of every instance sitegen creates, which is
// what its sign-in scope covers (see Scope).
const InstancePrefix = "sitegen-"

// InstanceName is the altengine instance for a site name: sitegen-<name>.
func InstanceName(name string) string {
	if strings.HasPrefix(name, InstancePrefix) {
		return name
	}
	return InstancePrefix + name
}

// DefaultName is the site name for a site with no publish.json: the site
// folder's name, or the project folder's when the site folder is the
// conventional "site".
func DefaultName(sitePath string) string {
	abs, err := filepath.Abs(sitePath)
	if err != nil {
		abs = sitePath
	}
	base := filepath.Base(abs)
	if strings.EqualFold(base, "site") {
		if parent := filepath.Base(filepath.Dir(abs)); parent != "." && parent != string(filepath.Separator) {
			base = parent
		}
	}
	return SanitizeName(base)
}

// ValidateName checks a site or instance name.
func ValidateName(name string) error {
	if len(name) < 3 {
		return fmt.Errorf("site name %q is too short: use at least 3 characters", name)
	}
	if !validName.MatchString(name) || strings.Contains(name, "--") {
		return fmt.Errorf("site name %q may only use a-z, 0-9 and single dashes, and can't start or end with a dash", name)
	}
	return nil
}

// ResolveName picks the instance: sitegen-<-publish-name>, then publish.json's
// instance as written, then sitegen-<folder name>. confirmed is false only for
// the folder name, which is a guess: an existing instance of that name may be
// another project's.
func ResolveName(flagName, sitePath string) (instance string, confirmed bool, err error) {
	if flagName != "" {
		short := strings.TrimPrefix(flagName, InstancePrefix)
		if err := ValidateName(short); err != nil {
			return "", false, fmt.Errorf("-publish-name: %w", err)
		}
		return InstanceName(short), true, nil
	}
	cfg, err := ReadConfig(sitePath)
	if err != nil {
		return "", false, err
	}
	if cfg.Instance != "" {
		if err := ValidateName(cfg.Instance); err != nil {
			return "", false, fmt.Errorf("%s: %w", filepath.Join(sitePath, ConfigFile), err)
		}
		return cfg.Instance, true, nil
	}
	short := DefaultName(sitePath)
	if err := ValidateName(short); err != nil {
		return "", false, fmt.Errorf("%w; choose one with -publish-name", err)
	}
	return InstanceName(short), false, nil
}
