package mapping

import (
	"context"
	"crypto/sha1" //nolint:gosec // entity IDs, not security
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entity is a generated FollowTheMoney entity.
type Entity struct {
	ID         string              `json:"id"`
	Schema     string              `json:"schema"`
	Properties map[string][]string `json:"properties"`
}

// Run reads every CSV of the mapping and calls emit once per distinct
// entity. Rows that produce the same ID are merged, so an address shared by
// many people is emitted once. Entities are held in memory until the end.
//
// Relative csv_url paths are resolved against baseDir.
func (f *File) Run(ctx context.Context, baseDir string, emit func(Entity) error) error {
	merged := map[string]*Entity{}
	var order []string

	for i, q := range f.Queries {
		err := f.runQuery(ctx, q, baseDir, func(e Entity) {
			if have, ok := merged[e.ID]; ok {
				for p, vals := range e.Properties {
					have.Properties[p] = appendUnique(have.Properties[p], vals...)
				}
				return
			}
			merged[e.ID] = &e
			order = append(order, e.ID)
		})
		if err != nil {
			return fmt.Errorf("query %d (%s): %w", i+1, q.CSVURL, err)
		}
	}
	for _, id := range order {
		if err := emit(*merged[id]); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) runQuery(ctx context.Context, q Query, baseDir string, add func(Entity)) error {
	rc, err := openSource(ctx, q.CSVURL, baseDir)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	r := csv.NewReader(rc)
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return errors.New("file is empty")
	}
	if err != nil {
		return err
	}
	cols := map[string]int{}
	for i, h := range header {
		h = strings.TrimSpace(h)
		if _, dup := cols[h]; !dup {
			cols[h] = i
		}
	}
	if err := q.checkColumns(cols); err != nil {
		return err
	}

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		row := func(col string) string {
			if i, ok := cols[col]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		if !q.matches(row) {
			continue
		}
		for _, e := range f.mapRow(q, row) {
			add(e)
		}
	}
}

// mapRow builds the entities of one row.
func (f *File) mapRow(q Query, row func(string) string) []Entity {
	names := q.entityNames()
	ids := map[string]string{}
	built := map[string]*Entity{}

	for _, name := range names {
		def := q.Entities[name]
		id := f.entityID(def, row)
		if id == "" {
			continue
		}
		props, ok := def.values(row)
		if !ok {
			continue
		}
		built[name] = &Entity{ID: id, Schema: def.Schema, Properties: props}
		ids[name] = id
	}
	// references need all IDs, so they are resolved in a second step
	for _, name := range names {
		e, live := built[name]
		if !live {
			continue
		}
		for prop, p := range q.Entities[name].Properties {
			if p.Entity == "" {
				continue
			}
			if id, ok := ids[p.Entity]; ok {
				e.Properties[prop] = []string{id}
			} else if p.Required {
				delete(built, name)
				break
			}
		}
	}
	var out []Entity
	for _, name := range names {
		if e, ok := built[name]; ok && len(e.Properties) > 0 {
			out = append(out, *e)
		}
	}
	return out
}

// values collects the non-reference properties. It reports false when a
// required property has no value.
func (e *EntityDef) values(row func(string) string) (map[string][]string, bool) {
	props := map[string][]string{}
	for prop, p := range e.Properties {
		if p.Entity != "" {
			continue
		}
		vals := p.collect(row)
		if len(vals) == 0 {
			if p.Required {
				return nil, false
			}
			continue
		}
		props[prop] = vals
	}
	return props, true
}

func (p *Property) collect(row func(string) string) []string {
	var raw []string
	switch {
	case p.Column != "":
		raw = []string{row(p.Column)}
	case len(p.Columns) > 0:
		for _, c := range p.Columns {
			raw = append(raw, row(c))
		}
	case p.Literal != "":
		raw = []string{p.Literal}
	case len(p.Literals) > 0:
		raw = append(raw, p.Literals...)
	}
	var vals []string
	for _, v := range raw {
		if r, ok := p.Replace[v]; ok {
			v = r
		}
		parts := []string{v}
		for _, sep := range p.Split {
			var next []string
			for _, part := range parts {
				next = append(next, strings.Split(part, sep)...)
			}
			parts = next
		}
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				vals = append(vals, part)
			}
		}
	}
	if p.Join != "" && len(vals) > 1 {
		vals = []string{strings.Join(vals, p.Join)}
	}
	return appendUnique(nil, vals...)
}

// entityID is stable for equal keys. It returns "" when every key is empty.
func (f *File) entityID(def *EntityDef, row func(string) string) string {
	if def.IDColumn != "" {
		return row(def.IDColumn)
	}
	h := sha1.New() //nolint:gosec // entity IDs, not security
	h.Write([]byte(f.Dataset + "\x00"))
	empty := true
	for _, k := range def.Keys {
		v := row(k)
		if v != "" {
			empty = false
		}
		h.Write([]byte(v + "\x00"))
	}
	for _, k := range def.KeyLiteral {
		empty = false
		h.Write([]byte(k + "\x00"))
	}
	if empty {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (q Query) matches(row func(string) string) bool {
	for col, want := range q.Filters {
		if row(col) != want {
			return false
		}
	}
	for col, not := range q.FiltersNot {
		if row(col) == not {
			return false
		}
	}
	return true
}

// checkColumns fails early on a column that is not in the CSV header, which
// is almost always a typo.
func (q Query) checkColumns(cols map[string]int) error {
	var missing []string
	need := func(c string) {
		if _, ok := cols[c]; c != "" && !ok {
			missing = append(missing, c)
		}
	}
	for c := range q.Filters {
		need(c)
	}
	for c := range q.FiltersNot {
		need(c)
	}
	for _, name := range q.entityNames() {
		e := q.Entities[name]
		need(e.IDColumn)
		for _, k := range e.Keys {
			need(k)
		}
		for _, p := range e.Properties {
			need(p.Column)
			for _, c := range p.Columns {
				need(c)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	missing = appendUnique(nil, missing...)
	have := make([]string, 0, len(cols))
	for c := range cols {
		have = append(have, c)
	}
	sort.Strings(have)
	return fmt.Errorf("column(s) not in the CSV header: %s (header has: %s)",
		strings.Join(missing, ", "), strings.Join(have, ", "))
}

func openSource(ctx context.Context, ref, baseDir string) (io.ReadCloser, error) {
	u, err := url.Parse(ref)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GET %s: %s", ref, resp.Status)
		}
		return resp.Body, nil
	}
	path := ref
	if err == nil && u.Scheme == "file" {
		path = u.Path
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return os.Open(path)
}

func appendUnique(dst []string, vals ...string) []string {
	for _, v := range vals {
		seen := false
		for _, d := range dst {
			if d == v {
				seen = true
				break
			}
		}
		if !seen {
			dst = append(dst, v)
		}
	}
	return dst
}
