// Package output renders command results as tables, JSON or jq results.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/itchyny/gojq"
	"golang.org/x/term"
)

// IsTerminal reports whether w is an interactive terminal.
func IsTerminal(w any) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Table prints aligned columns.
type Table struct {
	tw *tabwriter.Writer
}

// NewTable creates a table writing to w. A header row is printed first.
func NewTable(w io.Writer, header ...string) *Table {
	t := &Table{tw: tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)}
	if len(header) > 0 {
		t.Row(header...)
	}
	return t
}

// Row adds a row. Tabs and newlines inside cells are replaced by spaces.
func (t *Table) Row(cols ...string) {
	for i, c := range cols {
		cols[i] = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(c)
	}
	fmt.Fprintln(t.tw, strings.Join(cols, "\t"))
}

// Flush writes the table.
func (t *Table) Flush() error { return t.tw.Flush() }

// JSONOptions selects JSON output.
type JSONOptions struct {
	// Fields limits top-level keys; empty or "all" keeps everything.
	Fields []string
	// JQ is a jq expression applied to the result.
	JQ string
	// Pretty indents the output.
	Pretty bool
}

// WriteJSON prints v as JSON, optionally filtered by fields and a jq expression.
func WriteJSON(w io.Writer, v any, o JSONOptions) error {
	v, err := normalize(v)
	if err != nil {
		return err
	}
	if len(o.Fields) > 0 && !(len(o.Fields) == 1 && o.Fields[0] == "all") {
		v = selectFields(v, o.Fields)
	}
	if o.JQ == "" {
		return encode(w, v, o.Pretty)
	}
	q, err := gojq.Parse(o.JQ)
	if err != nil {
		return fmt.Errorf("invalid jq expression: %w", err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return fmt.Errorf("invalid jq expression: %w", err)
	}
	it := code.Run(v)
	for {
		res, ok := it.Next()
		if !ok {
			return nil
		}
		if err, isErr := res.(error); isErr {
			return fmt.Errorf("jq: %w", err)
		}
		// Like gh, print strings without quotes so results can be piped.
		if s, isStr := res.(string); isStr {
			fmt.Fprintln(w, s)
			continue
		}
		if err := encode(w, res, o.Pretty); err != nil {
			return err
		}
	}
}

// normalize converts typed values into generic JSON data for jq.
func normalize(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return convertNumbers(out), nil
}

// convertNumbers turns json.Number into int or float64, which gojq expects.
func convertNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		f, _ := t.Float64()
		return f
	case map[string]any:
		for k, x := range t {
			t[k] = convertNumbers(x)
		}
	case []any:
		for i, x := range t {
			t[i] = convertNumbers(x)
		}
	}
	return v
}

func selectFields(v any, fields []string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(fields))
		for _, f := range fields {
			if val, ok := t[f]; ok {
				out[f] = val
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = selectFields(x, fields)
		}
		return out
	}
	return v
}

func encode(w io.Writer, v any, pretty bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}
