// Package config resolves the OpenAleph host and API key from flags,
// environment variables and stored logins.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zalando/go-keyring"
)

const keyringService = "oa"

// Environment variable names in order of precedence. The OPAL_*, OPENALEPH_*
// and MEMORIOUS_ALEPH_* names are the ones read by the Python client.
var (
	hostVars = []string{"OA_HOST", "OPAL_HOST", "OPENALEPH_HOST", "MEMORIOUS_ALEPH_HOST"}
	keyVars  = []string{"OA_API_KEY", "OPAL_API_KEY", "OPENALEPH_API_KEY", "MEMORIOUS_ALEPH_API_KEY"}
)

// Settings is the resolved connection configuration.
type Settings struct {
	Host    string
	APIKey  string
	Retries int
	// HostSource and KeySource describe where a value came from, for `auth status`.
	HostSource string
	KeySource  string
}

// Keyring stores secrets in the OS keychain.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

type osKeyring struct{}

func (osKeyring) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (osKeyring) Set(s, u, p string) error        { return keyring.Set(s, u, p) }
func (osKeyring) Delete(s, u string) error        { return keyring.Delete(s, u) }

type hostEntry struct {
	// APIKey is only set when no keyring was available at login.
	APIKey string `json:"api_key,omitempty"`
}

type fileData struct {
	DefaultHost string               `json:"default_host,omitempty"`
	Hosts       map[string]hostEntry `json:"hosts,omitempty"`
}

// Store reads and writes saved logins.
type Store struct {
	Dir     string
	Keyring Keyring
}

// DefaultStore uses $OA_CONFIG_DIR or the user config directory.
func DefaultStore() (*Store, error) {
	dir := os.Getenv("OA_CONFIG_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "oa")
	}
	return &Store{Dir: dir, Keyring: osKeyring{}}, nil
}

func (s *Store) path() string { return filepath.Join(s.Dir, "hosts.json") }

func (s *Store) load() (fileData, error) {
	var d fileData
	b, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("parse %s: %w", s.path(), err)
	}
	return d, nil
}

func (s *Store) save(d fileData) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".hosts-*")
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
	return os.Rename(tmp.Name(), s.path())
}

// NormalizeHost trims whitespace and trailing slashes.
func NormalizeHost(h string) string {
	return strings.TrimRight(strings.TrimSpace(h), "/")
}

// SaveLogin stores the key for host and makes it the default host. The key
// goes into the OS keyring; if that is unavailable it falls back to the
// config file (mode 0600). It returns where the key was stored.
func (s *Store) SaveLogin(host, apiKey string) (string, error) {
	host = NormalizeHost(host)
	d, err := s.load()
	if err != nil {
		return "", err
	}
	if d.Hosts == nil {
		d.Hosts = map[string]hostEntry{}
	}
	where := "keyring"
	entry := hostEntry{}
	if err := s.Keyring.Set(keyringService, host, apiKey); err != nil {
		where = "config file"
		entry.APIKey = apiKey
	}
	d.Hosts[host] = entry
	d.DefaultHost = host
	return where, s.save(d)
}

// Logout removes the stored key for host and returns whether one existed.
func (s *Store) Logout(host string) (bool, error) {
	host = NormalizeHost(host)
	d, err := s.load()
	if err != nil {
		return false, err
	}
	_, had := d.Hosts[host]
	delete(d.Hosts, host)
	if err := s.Keyring.Delete(keyringService, host); err == nil {
		had = true
	}
	if d.DefaultHost == host {
		d.DefaultHost = ""
		for h := range d.Hosts {
			d.DefaultHost = h
			break
		}
	}
	return had, s.save(d)
}

// Resolve merges flags, environment and the stored login. Precedence:
// flags > OA_* > OPAL_* > OPENALEPH_* > MEMORIOUS_ALEPH_* > stored login.
func (s *Store) Resolve(getenv func(string) string, flagHost, flagKey string, flagRetries int) (Settings, error) {
	var st Settings
	d, err := s.load()
	if err != nil {
		return st, err
	}

	st.Host, st.HostSource = pick(getenv, flagHost, "--host", hostVars)
	if st.Host == "" && d.DefaultHost != "" {
		st.Host, st.HostSource = d.DefaultHost, "config file"
	}
	st.Host = NormalizeHost(st.Host)

	st.APIKey, st.KeySource = pick(getenv, flagKey, "--api-key", keyVars)
	if st.APIKey == "" && st.Host != "" {
		if k, err := s.Keyring.Get(keyringService, st.Host); err == nil && k != "" {
			st.APIKey, st.KeySource = k, "keyring"
		} else if e := d.Hosts[st.Host]; e.APIKey != "" {
			st.APIKey, st.KeySource = e.APIKey, "config file"
		}
	}

	st.Retries = flagRetries
	if st.Retries <= 0 {
		if n, err := strconv.Atoi(getenv("OPENALEPH_MAX_TRIES")); err == nil && n > 0 {
			st.Retries = n
		}
	}
	return st, nil
}

func pick(getenv func(string) string, flag, flagName string, vars []string) (string, string) {
	if flag != "" {
		return flag, flagName
	}
	for _, v := range vars {
		if val := getenv(v); val != "" {
			return val, v
		}
	}
	return "", ""
}
