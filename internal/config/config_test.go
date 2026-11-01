package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memKeyring struct {
	m    map[string]string
	fail bool
}

func (k *memKeyring) Get(s, u string) (string, error) {
	if v, ok := k.m[u]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

func (k *memKeyring) Set(s, u, p string) error {
	if k.fail {
		return errors.New("no keyring")
	}
	k.m[u] = p
	return nil
}

func (k *memKeyring) Delete(s, u string) error {
	if _, ok := k.m[u]; !ok {
		return errors.New("not found")
	}
	delete(k.m, u)
	return nil
}

func newStore(t *testing.T, fail bool) (*Store, *memKeyring) {
	t.Helper()
	kr := &memKeyring{m: map[string]string{}, fail: fail}
	return &Store{Dir: filepath.Join(t.TempDir(), "oa"), Keyring: kr}, kr
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolvePrecedence(t *testing.T) {
	s, _ := newStore(t, false)
	e := env(map[string]string{
		"MEMORIOUS_ALEPH_HOST": "https://memorious",
		"OPENALEPH_HOST":       "https://openaleph",
		"OPAL_HOST":            "https://opal/",
		"OPAL_API_KEY":         "opalkey",
		"OPENALEPH_API_KEY":    "okey",
	})
	got, err := s.Resolve(e, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "https://opal" || got.HostSource != "OPAL_HOST" {
		t.Fatalf("host = %q (%s)", got.Host, got.HostSource)
	}
	if got.APIKey != "opalkey" || got.KeySource != "OPAL_API_KEY" {
		t.Fatalf("key = %q (%s)", got.APIKey, got.KeySource)
	}

	e = env(map[string]string{"OA_HOST": "https://oa", "OPAL_HOST": "https://opal"})
	got, _ = s.Resolve(e, "https://flag", "flagkey", 0)
	if got.Host != "https://flag" || got.APIKey != "flagkey" || got.KeySource != "--api-key" {
		t.Fatalf("flags must win: %+v", got)
	}
	got, _ = s.Resolve(e, "", "", 0)
	if got.Host != "https://oa" {
		t.Fatalf("OA_HOST must win over OPAL_HOST: %+v", got)
	}
}

func TestRetriesFromEnv(t *testing.T) {
	s, _ := newStore(t, false)
	got, _ := s.Resolve(env(map[string]string{"OPENALEPH_MAX_TRIES": "9"}), "", "", 0)
	if got.Retries != 9 {
		t.Fatalf("retries = %d", got.Retries)
	}
	got, _ = s.Resolve(env(map[string]string{"OPENALEPH_MAX_TRIES": "9"}), "", "", 2)
	if got.Retries != 2 {
		t.Fatalf("flag must win: %d", got.Retries)
	}
}

func TestLoginViaKeyring(t *testing.T) {
	s, kr := newStore(t, false)
	where, err := s.SaveLogin("https://aleph.example.org/", "k1")
	if err != nil || where != "keyring" {
		t.Fatalf("where=%q err=%v", where, err)
	}
	if kr.m["https://aleph.example.org"] != "k1" {
		t.Fatal("key not in keyring")
	}
	b, _ := os.ReadFile(s.path())
	if string(b) != "" && strings.Contains(string(b), "k1") {
		t.Fatal("key must not be written to the config file when a keyring works")
	}
	got, _ := s.Resolve(env(nil), "", "", 0)
	if got.Host != "https://aleph.example.org" || got.APIKey != "k1" || got.KeySource != "keyring" {
		t.Fatalf("got %+v", got)
	}
}

func TestLoginFallsBackToFile(t *testing.T) {
	s, _ := newStore(t, true)
	where, err := s.SaveLogin("https://a.example", "k2")
	if err != nil || where != "config file" {
		t.Fatalf("where=%q err=%v", where, err)
	}
	st, err := os.Stat(s.path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	got, _ := s.Resolve(env(nil), "", "", 0)
	if got.APIKey != "k2" || got.KeySource != "config file" {
		t.Fatalf("got %+v", got)
	}
}

func TestLogout(t *testing.T) {
	s, kr := newStore(t, false)
	if _, err := s.SaveLogin("https://a.example", "k"); err != nil {
		t.Fatal(err)
	}
	had, err := s.Logout("https://a.example")
	if err != nil || !had {
		t.Fatalf("had=%v err=%v", had, err)
	}
	if len(kr.m) != 0 {
		t.Fatal("keyring entry not removed")
	}
	got, _ := s.Resolve(env(nil), "", "", 0)
	if got.Host != "" || got.APIKey != "" {
		t.Fatalf("got %+v", got)
	}
	if had, _ := s.Logout("https://a.example"); had {
		t.Fatal("second logout must report nothing to remove")
	}
}
