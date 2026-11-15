package cmd

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// permServer fakes the permissions API of collection 5 ("leak_a"). Like the
// server, it lists groups without access, drops users without access and
// skips unknown roles. With admin unset, it hides the email of users.
type permServer struct {
	t       *testing.T
	admin   bool
	roles   map[string]map[string]any
	access  map[string]string // role ID -> read or write
	updates [][]map[string]any
}

func newPermServer(t *testing.T) *permServer {
	return &permServer{
		t: t,
		roles: map[string]map[string]any{
			"1":  {"id": "1", "type": "system", "name": "Guest", "foreign_id": "guest"},
			"7":  {"id": "7", "type": "group", "name": "Research desk", "foreign_id": "group:research"},
			"8":  {"id": "8", "type": "group", "name": "Legal", "foreign_id": "group:legal"},
			"42": {"id": "42", "type": "user", "name": "Jane Doe", "email": "jane@example.org"},
			"43": {"id": "43", "type": "user", "name": "Legal", "email": "legal@example.org"},
		},
		access: map[string]string{"42": "read"},
	}
}

func (s *permServer) list() map[string]any {
	var ids []string
	for id, r := range s.roles {
		if s.access[id] != "" || r["type"] != "user" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var results []any
	for _, id := range ids {
		role := map[string]any{}
		for k, v := range s.roles[id] {
			if k == "email" && !s.admin {
				continue
			}
			role[k] = v
		}
		results = append(results, map[string]any{
			"role": role, "read": s.access[id] != "", "write": s.access[id] == "write",
		})
	}
	return map[string]any{"total": len(results), "results": results}
}

func (s *permServer) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/2/collections" && r.Method == "GET":
		reply(w, map[string]any{"total": 1, "results": []any{map[string]any{"id": "5", "foreign_id": "leak_a"}}})
	case r.URL.Path == "/api/2/collections/5":
		reply(w, map[string]any{"id": "5", "foreign_id": "leak_a"})
	case r.URL.Path == "/api/2/collections/5/permissions" && r.Method == "GET":
		reply(w, s.list())
	case r.URL.Path == "/api/2/collections/5/permissions" && r.Method == "PUT":
		var body []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Fatal(err)
		}
		s.updates = append(s.updates, body)
		for _, u := range body {
			id, _ := u["role_id"].(string)
			if email, ok := u["email"].(string); ok {
				id = ""
				for rid, role := range s.roles {
					if role["email"] == email {
						id = rid
					}
				}
			}
			if s.roles[id] == nil {
				continue
			}
			switch {
			case u["write"] == true:
				s.access[id] = "write"
			case u["read"] == true:
				s.access[id] = "read"
			default:
				delete(s.access, id)
			}
		}
		reply(w, s.list())
	default:
		w.WriteHeader(http.StatusNotFound)
		reply(w, map[string]any{"message": "not found"})
	}
}

func TestCollectionAccessList(t *testing.T) {
	s := newPermServer(t)
	s.admin = true
	h := newHarness(t, s.handler)
	h.login()

	out, _, err := h.run("collection", "access", "list", "leak_a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "jane@example.org") || !strings.Contains(out, "read") || strings.Contains(out, "Research desk") {
		t.Fatalf("list shows only roles with access:\n%s", out)
	}

	out, _, err = h.run("collection", "access", "list", "leak_a", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Research desk") || !strings.Contains(out, "none") {
		t.Fatalf("--all lists groups without access:\n%s", out)
	}

	out, _, err = h.run("collection", "access", "list", "5", "--jq", ".[].role.id")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "42" {
		t.Fatalf("jq output = %q", out)
	}
}

func TestCollectionAccessGrantAndRevoke(t *testing.T) {
	s := newPermServer(t)
	h := newHarness(t, s.handler)
	h.login()

	for _, tc := range []struct {
		args []string
		role string
		want string
	}{
		{[]string{"grant", "leak_a", "group:research", "--write"}, "7", "write"},
		{[]string{"grant", "leak_a", "research desk"}, "7", "read"},
		{[]string{"grant", "leak_a", "43"}, "43", "read"},
		{[]string{"revoke", "leak_a", "43"}, "43", ""},
		{[]string{"revoke", "leak_a", "group:research"}, "7", ""},
		{[]string{"revoke", "leak_a", "jane@example.org"}, "42", ""},
		{[]string{"grant", "leak_a", "jane@example.org", "-w"}, "42", "write"},
		{[]string{"grant", "leak_a", "guest"}, "1", "read"},
	} {
		_, stderr, err := h.run(append([]string{"collection", "access"}, tc.args...)...)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got := s.access[tc.role]; got != tc.want {
			t.Fatalf("%v: access of %s = %q, want %q", tc.args, tc.role, got, tc.want)
		}
		if !strings.Contains(stderr, "leak_a") {
			t.Fatalf("%v: stderr = %q", tc.args, stderr)
		}
	}
	last := s.updates[len(s.updates)-1]
	if len(last) != 1 || last[0]["role_id"] != "1" || last[0]["read"] != true || last[0]["write"] != false {
		t.Fatalf("update body = %v", last)
	}
}

func TestCollectionAccessErrors(t *testing.T) {
	s := newPermServer(t)
	s.access["43"] = "read" // lists user "Legal" next to group "Legal"
	s.admin = true
	h := newHarness(t, s.handler)
	h.login()

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"grant", "leak_a", "nobody@example.org"}, "nothing changed"},
		{[]string{"grant", "leak_a", "999"}, "nothing changed"},
		{[]string{"revoke", "leak_a", "nobody@example.org"}, "nothing changed"},
		{[]string{"grant", "leak_a", "Unknown group"}, "no role named"},
		{[]string{"grant", "leak_a", "legal"}, "several roles"},
	} {
		_, _, err := h.run(append([]string{"collection", "access"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
	if n := len(s.updates); n != 3 {
		t.Fatalf("unresolved names must not be sent, got %d updates", n)
	}
}
