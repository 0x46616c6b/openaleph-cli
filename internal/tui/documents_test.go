package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type downloadCall struct {
	id, dir   string
	overwrite bool
}

// fakeDocs is a backend with a folder tree and downloads:
//
//	Leak A: report.pdf, Mails (Folder) > mail.eml (Email) > attachment.doc
type fakeDocs struct {
	*fakeBackend
	mu        sync.Mutex
	downloads []downloadCall
	fail      error
}

func (f *fakeDocs) Download(_ context.Context, id, dir string, overwrite bool, progress func(DownloadResult, string)) (DownloadResult, error) {
	f.mu.Lock()
	f.downloads = append(f.downloads, downloadCall{id, dir, overwrite})
	f.mu.Unlock()
	progress(DownloadResult{Fetched: 1}, "a.txt")
	if f.fail != nil {
		return DownloadResult{Fetched: 1}, f.fail
	}
	return DownloadResult{Fetched: 2, Skipped: 1}, nil
}

func doc(id, schema, name, size string) api.Object {
	return api.Object{"id": id, "schema": schema, "caption": name,
		"collection": map[string]any{"id": "12", "label": "Leak A"},
		"properties": map[string]any{"fileName": []any{name}, "fileSize": []any{size}}}
}

func startDocs(t *testing.T) (*session, *fakeDocs) {
	t.Helper()
	f := &fakeDocs{fakeBackend: newFake()}
	f.docs = func(q EntityQuery) Page {
		var items []api.Object
		switch {
		case q.TopLevel && q.CollectionID == "12":
			items = []api.Object{doc("d1", "Pages", "report.pdf", "2048000"), doc("d2", "Folder", "Mails", "")}
		case q.Parent == "d2":
			items = []api.Object{doc("d3", "Email", "mail.eml", "1200")}
		case q.Parent == "d3":
			items = []api.Object{doc("d4", "Document", "attachment.doc", "300")}
		}
		return Page{Items: items, Total: len(items)}
	}
	for _, d := range []api.Object{doc("d1", "Pages", "report.pdf", "2048000"), doc("d3", "Email", "mail.eml", "1200")} {
		f.entity[str(d, "id")] = d
	}
	return start(t, Options{Backend: f, Collection: f.collections[0], DownloadDir: "/data"}), f
}

func TestDocumentsTabAndFolders(t *testing.T) {
	s, f := startDocs(t)
	s.contains("tab documents")
	s.keys("tab")
	s.depth(2)
	if c := f.lastCall(); !c.topLevel || c.collection != "12" {
		t.Fatalf("documents call = %+v", c)
	}
	s.contains("Datasets ❯ Leak A documents", "report.pdf", "2.0 MB", "Folder ›", "Mails", "tab entities")

	// enter on a folder opens its contents.
	s.keys("j", "enter")
	s.depth(3)
	if c := f.lastCall(); c.parent != "d2" || c.topLevel {
		t.Fatalf("folder call = %+v", c)
	}
	s.contains("Leak A documents ❯ Mails", "mail.eml", "Email")

	// A search in a folder keeps the folder (the backend searches below it).
	s.keys("/")
	s.typeText("invoice")
	s.keys("enter")
	if c := f.lastCall(); c.parent != "d2" || c.query != "invoice" {
		t.Fatalf("folder search = %+v", c)
	}
	s.keys("/", "esc")

	// enter on an email shows its details, with its attachments as contents.
	s.keys("/")
	for range len("invoice") {
		s.keys("backspace")
	}
	s.keys("enter", "enter")
	s.depth(4)
	s.contains("mail.eml", "contents", "files and attachments in it ›")
	s.keys("j", "j", "j", "enter")
	s.depth(5)
	if c := f.lastCall(); c.parent != "d3" {
		t.Fatalf("contents call = %+v", c)
	}
	s.contains("attachment.doc")

	// Back to the top and tab back to the entities, which kept their state.
	s.keys("esc", "esc", "esc")
	s.depth(2)
	calls := len(f.calls)
	s.keys("tab")
	s.contains("Datasets ❯ Leak A", "Acme Corp")
	s.keys("tab")
	s.contains("report.pdf")
	if len(f.calls) != calls {
		t.Fatalf("tab reloaded a list: %d calls, want %d", len(f.calls), calls)
	}
}

func TestDownloadDocument(t *testing.T) {
	s, f := startDocs(t)
	s.keys("tab", "d")
	s.contains("Download report.pdf", "/data", "Saves the file here", "keep")
	s.keys("ctrl+s")
	s.depth(2)
	if len(f.downloads) != 1 || f.downloads[0] != (downloadCall{"d1", "/data", false}) {
		t.Fatalf("downloads = %+v", f.downloads)
	}
	s.contains("Downloaded 2 files to /data (1 already there, kept)")

	// Folders download with everything in them; replace overwrites.
	dir := t.TempDir()
	s.keys("j", "d")
	s.contains("Download Mails", "Saves the folder with everything in it here")
	for range len("/data") {
		s.keys("backspace")
	}
	s.typeText(dir)
	s.keys("tab", "right", "ctrl+s")
	if got := f.downloads[1]; got != (downloadCall{"d2", dir, true}) {
		t.Fatalf("folder download = %+v", got)
	}
	// The next download offers the last directory.
	s.keys("k", "d")
	if got := s.model().top().(*formView).fields[0].input.Value(); got != dir {
		t.Fatalf("offered directory = %q, want %q", got, dir)
	}
}

func TestDownloadErrors(t *testing.T) {
	s, f := startDocs(t)
	// Acme Corp is a company, not a document.
	s.keys("d")
	s.depth(2)
	s.contains("Acme Corp is not a document")

	// A file as target directory is refused in the form.
	file := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.keys("tab", "d")
	for range len("/data") {
		s.keys("backspace")
	}
	s.typeText(file)
	s.keys("ctrl+s")
	s.contains("is not a directory")
	if len(f.downloads) != 0 {
		t.Fatal("downloaded into a file")
	}
	s.keys("esc")

	f.fail = os.ErrPermission
	s.keys("d", "ctrl+s")
	s.contains("Download of report.pdf failed after 1 file: permission denied")
}

func TestDownloadFromEntityView(t *testing.T) {
	s, f := startDocs(t)
	s.keys("tab", "enter")
	s.contains("report.pdf", "d download")
	s.keys("d", "ctrl+s")
	if len(f.downloads) != 1 || f.downloads[0].id != "d1" {
		t.Fatalf("downloads = %+v", f.downloads)
	}
	if !strings.Contains(s.screen(), "Downloaded") {
		t.Fatal("no download status")
	}
}

func TestListFootersFit80(t *testing.T) {
	s, _ := startDocs(t)
	s.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	check := func(name string, want ...string) {
		t.Helper()
		f := ansi.Strip(s.model().footer())
		if w := ansi.StringWidth(f); w > 80 || strings.HasSuffix(strings.TrimSpace(f), "…") {
			t.Errorf("%s footer is cut (%d wide): %q", name, w, f)
		}
		for _, k := range append(want, "? help", "q quit") {
			if !strings.Contains(f, k) {
				t.Errorf("%s footer misses %q: %q", name, k, f)
			}
		}
	}
	check("entities", "tab documents", "f schema", "p status")
	s.keys("tab")
	check("documents", "tab entities", "d download", "p status")
	s.keys("enter")
	check("document", "d download")
}
