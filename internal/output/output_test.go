package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteJSONFields(t *testing.T) {
	var buf bytes.Buffer
	v := []map[string]any{{"id": "1", "label": "A", "x": 1}}
	if err := WriteJSON(&buf, v, JSONOptions{Fields: []string{"id", "label"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != `[{"id":"1","label":"A"}]` {
		t.Fatalf("got %s", got)
	}
}

func TestWriteJSONAllKeepsEverything(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]any{"a": 1, "b": 2}, JSONOptions{Fields: []string{"all"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != `{"a":1,"b":2}` {
		t.Fatalf("got %s", got)
	}
}

func TestWriteJSONJQ(t *testing.T) {
	var buf bytes.Buffer
	v := map[string]any{"results": []any{map[string]any{"n": 3, "s": "x"}, map[string]any{"n": 4, "s": "y"}}}
	if err := WriteJSON(&buf, v, JSONOptions{JQ: ".results[] | .n + 1"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "4\n5\n" {
		t.Fatalf("numbers: %q", buf.String())
	}
	buf.Reset()
	if err := WriteJSON(&buf, v, JSONOptions{JQ: ".results[].s"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "x\ny\n" {
		t.Fatalf("strings must be unquoted: %q", buf.String())
	}
}

func TestWriteJSONInvalidJQ(t *testing.T) {
	if err := WriteJSON(&bytes.Buffer{}, 1, JSONOptions{JQ: ".["}); err == nil {
		t.Fatal("expected error")
	}
}

func TestTableAlignsAndSanitizes(t *testing.T) {
	var buf bytes.Buffer
	tb := NewTable(&buf, "ID", "NAME")
	tb.Row("1", "a\nb")
	tb.Row("22", "c")
	if err := tb.Flush(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "a b") {
		t.Fatalf("lines = %q", lines)
	}
}
