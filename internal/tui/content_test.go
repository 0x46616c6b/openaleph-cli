package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// fakeReader serves pages and CSV files.
type fakeReader struct {
	*fakeBackend
	pages     int
	pageCalls []int
	csv       [][]string
	csvURL    string

	counts     map[string]int
	countCalls [][]string
	thread     []api.Object
	threadOf   string
}

func (f *fakeReader) Pages(_ context.Context, doc string, offset int) (Page, error) {
	f.pageCalls = append(f.pageCalls, offset)
	var items []api.Object
	for i := offset; i < min(offset+20, f.pages); i++ {
		items = append(items, api.Object{"schema": "Page", "properties": map[string]any{
			"index": []any{fmt.Sprint(i + 1)}, "bodyText": []any{fmt.Sprintf("text of page %d\n%s", i+1, strings.Repeat("line\n", 10))},
		}})
	}
	return Page{Items: items, Total: f.pages, More: offset+20 < f.pages}, nil
}

func (f *fakeReader) AttachmentCounts(_ context.Context, ids []string) (map[string]int, error) {
	f.countCalls = append(f.countCalls, ids)
	out := map[string]int{}
	for _, id := range ids {
		if n, ok := f.counts[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (f *fakeReader) Thread(_ context.Context, email api.Object) ([]api.Object, error) {
	f.threadOf = str(email, "id")
	return f.thread, nil
}

func (f *fakeReader) CSV(_ context.Context, u string, maxRows int) ([][]string, bool, error) {
	f.csvURL = u
	return f.csv, false, nil
}

func openDoc(t *testing.T, f *fakeReader, d api.Object) *session {
	t.Helper()
	f.entity[str(d, "id")] = d
	f.entities = func(string, string, int) Page { return Page{Items: []api.Object{d}, Total: 1} }
	s := start(t, Options{Backend: f, Collection: f.collections[0]})
	s.keys("enter")
	return s
}

func newReader() *fakeReader { return &fakeReader{fakeBackend: newFake()} }

func TestEmailContent(t *testing.T) {
	f := newReader()
	s := openDoc(t, f, api.Object{"id": "m1", "schema": "Email", "caption": "Offer", "properties": map[string]any{
		"from": []any{"Jane <jane@acme.example>"}, "to": []any{"bob@example.org", "eve@example.org"},
		"subject": []any{"Offer"}, "date": []any{"2024-03-01"},
		"bodyHtml": []any{"<p>Dear Bob,</p><p>the <b>offer</b> stands.</p>"},
	}, "safeHtml": []any{"<p>Dear Bob,</p><p>the <b>offer</b> stands.</p>"}})
	s.contains("Content", "Properties", "From:     Jane <jane@acme.example>", "To:       bob@example.org, eve@example.org",
		"Subject:  Offer", "Dear Bob,", "the offer stands.")
	s.keys("tab")
	s.contains("PROPERTY", "subject")
	s.keys("tab")
	s.contains("Dear Bob,")
}

func TestTextSearch(t *testing.T) {
	f := newReader()
	body := "intro\n" + strings.Repeat("filler\n", 60) + "the Invoice is here\nand another invoice"
	s := openDoc(t, f, api.Object{"id": "t1", "schema": "PlainText", "caption": "notes.txt",
		"properties": map[string]any{"bodyText": []any{body}}})
	s.contains("intro")
	s.keys("/")
	// q in the find input is text, not quit.
	s.typeText("invoice q")
	if s.quit {
		t.Fatal("q quit while typing")
	}
	s.keys("enter")
	s.contains(`"invoice q" not found`)
	s.keys("/")
	s.typeText("invoice")
	s.keys("enter")
	s.contains(`2 hits for "invoice"`, "the Invoice is here")
	s.keys("n")
	s.contains("and another invoice")
	s.keys("Y")
	if len(s.clips) != 1 || s.clips[0] != body {
		t.Fatalf("copied %d texts", len(s.clips))
	}
}

func TestPagesLoadLazily(t *testing.T) {
	f := newReader()
	f.pages = 45
	s := openDoc(t, f, api.Object{"id": "p1", "schema": "Pages", "caption": "report.pdf", "properties": map[string]any{}})
	s.contains("─── page 1 ───", "text of page 1", "20 of 45 pages loaded")
	if len(f.pageCalls) != 1 {
		t.Fatalf("page calls = %v", f.pageCalls)
	}
	// G jumps to the end of what is loaded and loads the next pages.
	s.keys("G", "G", "G")
	if strings.Join(strings.Fields(fmt.Sprint(f.pageCalls)), " ") != "[0 20 40]" {
		t.Fatalf("page calls = %v", f.pageCalls)
	}
	s.contains("45 pages", "text of page 45")
}

func TestPagesFallBackToText(t *testing.T) {
	f := newReader()
	s := openDoc(t, f, api.Object{"id": "p2", "schema": "Pages", "caption": "scan.pdf",
		"properties": map[string]any{"bodyText": []any{"OCR text"}}})
	s.contains("OCR text", "no pages")
}

func TestTableContent(t *testing.T) {
	f := newReader()
	f.csv = [][]string{{"1", "Acme", "Panama"}, {"2", "Globex\x1b]0;x\x07", "Springfield"}}
	s := openDoc(t, f, api.Object{"id": "x1", "schema": "Table", "caption": "companies.csv",
		"links":      map[string]any{"csv": "https://aleph.example.org/api/2/archive?t=1"},
		"properties": map[string]any{"columns": []any{`["id","name","country"]`}, "rowCount": []any{"2"}}})
	if f.csvURL != "https://aleph.example.org/api/2/archive?t=1" {
		t.Fatalf("csv url = %q", f.csvURL)
	}
	s.contains("id", "name", "country", "Acme", "Panama", "Globex]0;x", "row 1 of 2")
	if strings.Contains(s.m.View().Content, "\x1b]0;") {
		t.Fatal("control sequence from the file reached the terminal")
	}
}

func TestWideTableScrolls(t *testing.T) {
	f := newReader()
	var header, row []string
	for i := range 12 {
		header = append(header, fmt.Sprintf("column%02d", i))
		row = append(row, fmt.Sprintf("value%02d-%s", i, strings.Repeat("x", 10)))
	}
	f.csv = [][]string{header, row} // no columns property: first row is the header
	s := openDoc(t, f, api.Object{"id": "x2", "schema": "Table", "caption": "wide.csv",
		"links": map[string]any{"csv": "u"}, "properties": map[string]any{}})
	s.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	s.contains("column00", "row 1 of 1", "columns 1–")
	if strings.Contains(s.screen(), "column11") {
		t.Fatal("all columns visible at 80 columns")
	}
	for range 11 {
		s.keys(">")
	}
	s.contains("column11", "value11")
	if strings.Contains(s.screen(), "column00") {
		t.Fatal("did not scroll")
	}
	s.keys("<")
	s.contains("column10")
}

func TestNoContentForThings(t *testing.T) {
	f := newReader()
	s := start(t, Options{Backend: f, Collection: f.collections[0]})
	s.keys("enter") // Acme Corp, a company
	if strings.Contains(s.screen(), "Content") {
		t.Fatal("content tab for a company")
	}
	s.contains("PROPERTY", "jurisdiction")
}

func TestControlCharactersNeverReachTheTerminal(t *testing.T) {
	f := newReader()
	evil := "before\x1b]52;c;cHduZWQ=\x07\x1b[2J\x9bafter"
	s := openDoc(t, f, api.Object{"id": "e1", "schema": "PlainText", "caption": "evil\x1b]0;title\x07.txt",
		"properties": map[string]any{"bodyText": []any{evil}}})
	out := s.m.View().Content
	for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x1b]0;", "\x07", "\x9b"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q reached the terminal", bad)
		}
	}
	s.contains("before]52;c;cHduZWQ=[2Jafter")
}

func TestContentFootersFit80(t *testing.T) {
	for name, d := range map[string]api.Object{
		"text":  {"id": "t", "schema": "PlainText", "caption": "a.txt", "properties": map[string]any{"bodyText": []any{"x"}}},
		"table": {"id": "x", "schema": "Table", "caption": "a.csv", "links": map[string]any{"csv": "u"}, "properties": map[string]any{}},
	} {
		f := newReader()
		f.csv = [][]string{{"a"}, {"1"}}
		s := openDoc(t, f, d)
		s.send(tea.WindowSizeMsg{Width: 80, Height: 24})
		foot := ansi.Strip(s.model().footer())
		if w := ansi.StringWidth(foot); w > 80 || strings.HasSuffix(strings.TrimSpace(foot), "…") {
			t.Errorf("%s footer cut (%d): %q", name, w, foot)
		}
		for _, k := range []string{"tab properties", "? help", "q quit"} {
			if !strings.Contains(foot, k) {
				t.Errorf("%s footer misses %q: %q", name, k, foot)
			}
		}
	}
}

func TestContentFitsScreen(t *testing.T) {
	long := strings.Repeat("word ", 400)
	docs := map[string]api.Object{
		"email": {"id": "m", "schema": "Email", "caption": "m", "properties": map[string]any{"from": []any{long}, "bodyText": []any{long}}},
		"table": {"id": "x", "schema": "Table", "caption": "x", "links": map[string]any{"csv": "u"}, "properties": map[string]any{}},
		"pages": {"id": "p", "schema": "Pages", "caption": "p", "properties": map[string]any{}},
	}
	for name, d := range docs {
		f := newReader()
		f.pages = 30
		f.csv = [][]string{{"a", "b", strings.Repeat("c", 80)}, {"1", "2", long}}
		s := openDoc(t, f, d)
		for _, size := range [][2]int{{80, 24}, {50, 12}} {
			s.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for _, keys := range [][]string{nil, {"/"}, {"esc", "tab"}, {"tab"}} {
				s.keys(keys...)
				lines := strings.Split(s.m.View().Content, "\n")
				if len(lines) != size[1] {
					t.Errorf("%s %dx%d after %v: %d lines", name, size[0], size[1], keys, len(lines))
				}
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > size[0] {
						t.Errorf("%s %dx%d after %v: line %d wide", name, size[0], size[1], keys, w)
					}
				}
			}
		}
	}
}

func TestEmailAttachments(t *testing.T) {
	f := newReader()
	var parents []string
	f.docs = func(q EntityQuery) Page {
		parents = append(parents, q.Parent)
		if q.Parent != "m2" {
			return Page{}
		}
		items := []api.Object{doc("a1", "Document", "contract.docx", "48000"), doc("a2", "Pages", "invoice.pdf", "1200000")}
		return Page{Items: items, Total: 2}
	}
	f.entity["a2"] = doc("a2", "Pages", "invoice.pdf", "1200000")
	s := openDoc(t, f, api.Object{"id": "m2", "schema": "Email", "caption": "Offer",
		"collection": map[string]any{"id": "12", "label": "Leak A"},
		"properties": map[string]any{"subject": []any{"Offer"}, "bodyText": []any{"see attached"}}})
	s.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	s.contains("Attach:   contract.docx (48 kB), invoice.pdf (1.2 MB)  · A opens them", "see attached")

	foot := ansi.Strip(s.model().footer())
	if !strings.Contains(foot, "A attach") || !strings.Contains(foot, "T thread") || !strings.Contains(foot, "q quit") || ansi.StringWidth(foot) > 80 {
		t.Fatalf("footer = %q", foot)
	}

	s.keys("A")
	s.contains("Offer ❯ Attachments", "contract.docx", "invoice.pdf")
	if parents[len(parents)-1] != "m2" {
		t.Fatalf("attachments query parents = %v", parents)
	}
	// An attachment opens on its content like any document.
	s.keys("j", "enter")
	s.contains("invoice.pdf", "Content")
}

func TestEmailWithoutAttachments(t *testing.T) {
	f := newReader()
	s := openDoc(t, f, api.Object{"id": "m3", "schema": "Email", "caption": "Hi",
		"properties": map[string]any{"bodyText": []any{"hello"}}})
	if strings.Contains(s.screen(), "Attach:") || strings.Contains(s.screen(), "A attach") {
		t.Fatal("attachments shown for an email without any")
	}
	s.keys("A")
	s.contains("No attachments")
}
