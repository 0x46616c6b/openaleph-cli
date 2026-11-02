package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0x46616c6b/openaleph-cli/internal/config"
)

type memKeyring struct{ m map[string]string }

func (k *memKeyring) Get(_, u string) (string, error) {
	if v, ok := k.m[u]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}
func (k *memKeyring) Set(_, u, p string) error { k.m[u] = p; return nil }
func (k *memKeyring) Delete(_, u string) error {
	if _, ok := k.m[u]; !ok {
		return errors.New("not found")
	}
	delete(k.m, u)
	return nil
}

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	store  *config.Store
	env    map[string]string
	stdin  string
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func newHarness(t *testing.T, h http.HandlerFunc) *harness {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &harness{
		t:     t,
		srv:   srv,
		store: &config.Store{Dir: filepath.Join(t.TempDir(), "oa"), Keyring: &memKeyring{m: map[string]string{}}},
		env:   map[string]string{},
	}
}

func (h *harness) run(args ...string) (string, string, error) {
	h.t.Helper()
	h.stdout, h.stderr = &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{
		IO:     &IOStreams{In: strings.NewReader(h.stdin), Out: h.stdout, Err: h.stderr},
		Getenv: func(k string) string { return h.env[k] },
		Store:  h.store,
	}
	root := NewRootCmd(app)
	root.SetArgs(args)
	err := root.Execute()
	return h.stdout.String(), h.stderr.String(), err
}

func (h *harness) login() {
	h.t.Helper()
	h.env["OA_HOST"] = h.srv.URL
	h.env["OA_API_KEY"] = "key"
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestAuthLoginAndStatus(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "ApiKey good" {
			w.WriteHeader(http.StatusForbidden)
			reply(w, map[string]any{"message": "nope"})
			return
		}
		reply(w, map[string]any{"results": []any{}, "total": 0})
	})
	h.stdin = "good\n"
	if _, stderr, err := h.run("auth", "login", "--host", h.srv.URL, "--with-token"); err != nil {
		t.Fatalf("login: %v", err)
	} else if !strings.Contains(stderr, "Logged in to "+h.srv.URL) {
		t.Fatalf("stderr = %q", stderr)
	}

	stdout, _, err := h.run("auth", "status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "logged in") || !strings.Contains(stdout, "keyring") || strings.Contains(stdout, "good") {
		t.Fatalf("stdout = %q", stdout)
	}

	if _, _, err := h.run("auth", "logout"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run("auth", "status"); err == nil {
		t.Fatal("status must fail after logout")
	}
}

func TestAuthLoginRejectsBadKey(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		reply(w, map[string]any{"message": "nope"})
	})
	h.stdin = "bad\n"
	_, _, err := h.run("auth", "login", "--host", h.srv.URL, "--with-token")
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
}

func TestNotConfigured(t *testing.T) {
	h := newHarness(t, func(http.ResponseWriter, *http.Request) {})
	_, _, err := h.run("collection", "list")
	if err == nil || !strings.Contains(err.Error(), "oa auth login") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectionListTable(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 2, "results": []any{
			map[string]any{"id": "1", "foreign_id": "leak_a", "label": "Leak A", "category": "leak", "count": 10},
			map[string]any{"id": "2", "foreign_id": "leak_b", "label": "Leak B", "category": "other", "count": 0},
		}})
	})
	h.login()
	stdout, _, err := h.run("collection", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "FOREIGN ID", "leak_a", "Leak B", "10"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestCollectionListJSONFields(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": "1", "foreign_id": "leak_a", "label": "Leak A", "count": 10},
		}})
	})
	h.login()
	stdout, _, err := h.run("collection", "list", "--json", "id,label")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v: %q", err, stdout)
	}
	if len(got) != 1 || got[0]["label"] != "Leak A" || got[0]["foreign_id"] != nil {
		t.Fatalf("got %v", got)
	}
}

func TestCollectionViewByForeignID(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter:foreign_id") != "leak_a" {
			t.Errorf("query = %v", r.URL.Query())
		}
		reply(w, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": "1", "foreign_id": "leak_a", "label": "Leak A", "category": "leak", "languages": []any{"de", "en"}},
		}})
	})
	h.login()
	stdout, _, err := h.run("collection", "view", "leak_a")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Leak A", "foreign_id:", "de, en"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestSearchJQ(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "acme corp" || q.Get("filter:schema") != "Company" {
			t.Errorf("query = %v", q)
		}
		reply(w, map[string]any{"total": 2, "results": []any{
			map[string]any{"id": "e1", "schema": "Company", "caption": "Acme Corp", "collection": map[string]any{"label": "Leak A"}},
			map[string]any{"id": "e2", "schema": "Company", "caption": "Acme Corp Ltd"},
		}})
	})
	h.login()
	stdout, _, err := h.run("search", "acme", "corp", "--schema", "Company", "--jq", ".[].id")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "e1\ne2\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSearchTable(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": "e1", "schema": "Person", "caption": "Jane Doe", "collection": map[string]any{"label": "Leak A"}},
		}})
	})
	h.login()
	stdout, _, err := h.run("search", "jane")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Jane Doe") || !strings.Contains(stdout, "Leak A") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestStatusTable(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"total": 1, "results": []any{
			map[string]any{"todo": 4, "doing": 1, "failed": 2, "finished": 9, "remaining_time": "PT5H12M42.885S", "collection": map[string]any{"id": "7", "label": "Leak A"}},
		}})
	})
	h.login()
	stdout, _, err := h.run("status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TODO", "REMAINING", "Leak A", "5h12m", "9"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestStatusWatch(t *testing.T) {
	calls := 0
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		results := []any{}
		if calls < 3 {
			results = append(results, map[string]any{"todo": float64(4 - calls), "doing": 1, "failed": 2, "collection": map[string]any{"id": "7", "label": "Leak A"}})
		}
		reply(w, map[string]any{"results": results})
	})
	h.login()
	stdout, stderr, err := h.run("status", "watch", "-i", "1ms", "--exit-status")
	if err == nil {
		t.Fatal("want error for failed tasks with --exit-status")
	}
	if calls != 3 || strings.Count(stdout, "Leak A") != 2 {
		t.Fatalf("calls = %d, stdout:\n%s", calls, stdout)
	}
	if !strings.Contains(stderr, "All tasks finished, 2 failed") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestEntityView(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2/entities/e1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		reply(w, map[string]any{"id": "e1", "schema": "Person", "properties": map[string]any{"name": []any{"Jane"}}})
	})
	h.login()
	stdout, _, err := h.run("entity", "view", "e1", "--jq", ".properties.alephUrl[0]")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(stdout), "/api/2/entities/e1") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestAPIGetWithFields(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2/collections" || r.URL.Query().Get("limit") != "5" {
			t.Errorf("url = %s", r.URL)
		}
		if r.Header.Get("Authorization") != "ApiKey key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		reply(w, map[string]any{"results": []any{map[string]any{"label": "A"}}})
	})
	h.login()
	stdout, _, err := h.run("api", "collections", "-f", "limit=5", "--jq", ".results[].label")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "A\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestAPIPostBody(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "POST" || r.URL.Path != "/api/2/collections/1/reindex" || body["flush"] != "true" {
			t.Errorf("%s %s %v", r.Method, r.URL, body)
		}
		reply(w, map[string]any{"status": "ok"})
	})
	h.login()
	if _, _, err := h.run("api", "/api/2/collections/1/reindex", "-X", "POST", "-f", "flush=true"); err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrorSurfaced(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		reply(w, map[string]any{"message": "Not found."})
	})
	h.login()
	_, _, err := h.run("api", "entities/zzz")
	if err == nil || err.Error() != "Not found." {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderStatusTUI(t *testing.T) {
	rows := []any{map[string]any{"todo": float64(2), "doing": float64(1), "failed": float64(1), "finished": float64(6), "remaining_time": "PT5H12M42S", "collection": map[string]any{"id": "7", "label": "Leak A"}}}
	out := renderStatusTUI(rows, time.Unix(0, 0), 3*time.Second, false)
	for _, want := range []string{"┌", "Leak A", "70%", "5h12m", "Press q to quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[3") {
		t.Error("colors must be off")
	}
}
