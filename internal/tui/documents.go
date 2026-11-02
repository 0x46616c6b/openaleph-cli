package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// DownloadResult counts the files of a download.
type DownloadResult struct {
	Fetched, Skipped int
}

// Downloader saves documents. A Backend may implement it; without it the
// browser cannot download.
type Downloader interface {
	// Download saves the file of a document, or a folder with everything
	// below it, into dir. Existing files are kept unless overwrite is set.
	// progress is called after each file.
	Download(ctx context.Context, entityID, dir string, overwrite bool,
		progress func(r DownloadResult, name string)) (DownloadResult, error)
}

// documentSchemas are the FollowTheMoney schemas that are documents.
var documentSchemas = []string{
	"Document", "Article", "Audio", "Email", "Folder", "HyperText", "Image", "Message",
	"Package", "Pages", "PlainText", "Table", "Video", "Workbook",
}

// folderSchemas are folders whose contents enter opens. Emails and messages
// can have children (attachments) too, but enter shows their details.
var folderSchemas = []string{"Folder", "Package", "Workbook"}

func isDocument(o api.Object) bool { return slices.Contains(documentSchemas, str(o, "schema")) }
func isFolder(o api.Object) bool   { return slices.Contains(folderSchemas, str(o, "schema")) }

var (
	keyDownload = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download"))
	keyToDocs   = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "documents"))
	keyToEnts   = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "entities"))
)

// docName is the file name of a document, or its caption.
func docName(o api.Object) string {
	props, _ := o["properties"].(map[string]any)
	if vals, ok := props["fileName"].([]any); ok && len(vals) > 0 {
		if s, ok := vals[0].(string); ok && s != "" {
			return s
		}
	}
	if s := api.Caption(o); s != "" {
		return s
	}
	return str(o, "id")
}

func fileSize(o api.Object) string {
	props, _ := o["properties"].(map[string]any)
	if vals, ok := props["fileSize"].([]any); ok && len(vals) > 0 {
		switch v := vals[0].(type) {
		case string:
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				return humanize.Bytes(n)
			}
		case float64:
			return humanize.Bytes(uint64(v))
		}
	}
	return ""
}

// newDocumentsView lists the top-level documents of a collection (folder
// nil) or the contents of a folder. A search covers the folder and all
// folders below it, like in the web UI.
func newDocumentsView(e *env, collection, folder api.Object) *listView {
	name := str(collection, "label") + " documents"
	base := EntityQuery{CollectionID: str(collection, "id"), TopLevel: true}
	if folder != nil {
		name = docName(folder)
		base = EntityQuery{Parent: str(folder, "id")}
	}
	l := newListView(e, name, "documents", "")
	l.base = base
	l.fetch = func(ctx context.Context, q listQuery, offset int) (Page, error) {
		eq := base
		eq.Query, eq.Schemas = q.text, q.schemas
		return e.backend.Entities(ctx, eq, offset)
	}
	l.cols = []column{
		{title: "TYPE", width: 10, value: func(o api.Object) string {
			if isFolder(o) {
				return str(o, "schema") + " ›"
			}
			return str(o, "schema")
		}},
		{title: "NAME", flex: 1, value: docName},
		{title: "SIZE", width: 9, value: fileSize},
	}
	l.enter = func(o api.Object) view {
		if isFolder(o) {
			return newDocumentsView(e, collection, o)
		}
		return newEntityView(e, o)
	}
	l.actions = append(l.actions,
		action{key: keySchema, anyItem: true, run: func(l *listView, _ api.Object) tea.Cmd { return push(newSchemaPicker(e, l)) }},
		action{key: keyClearSchema, anyItem: true, run: func(l *listView, _ api.Object) tea.Cmd { return l.setSchemas(nil) }},
		action{key: keyDownload, run: func(_ *listView, o api.Object) tea.Cmd { return downloadDialog(e, o) }},
		action{key: keyYank, run: yankID},
		action{key: keyWeb, run: openUI},
	)
	l.footerHide = map[string]bool{"enter": true, "f": true}
	return l
}

// pairViews lets tab switch between the entities and the documents of a
// collection. The documents list is created and loaded on first use.
func pairViews(e *env, entities *listView, collection api.Object) {
	var docs *listView
	entities.actions = append([]action{{key: keyToDocs, anyItem: true, run: func(l *listView, _ api.Object) tea.Cmd {
		if docs == nil {
			docs = newDocumentsView(e, collection, nil)
			docs.actions = append([]action{{key: keyToEnts, anyItem: true, run: func(d *listView, _ api.Object) tea.Cmd {
				return replace(d.vid, entities, false)
			}}}, docs.actions...)
			return replace(l.vid, docs, true)
		}
		return replace(l.vid, docs, false)
	}}}, entities.actions...)
}

// downloadDialog asks where to save a document or folder, then downloads it
// in the background.
func downloadDialog(e *env, o api.Object) tea.Cmd {
	dl, ok := e.backend.(Downloader)
	switch {
	case !ok:
		return statusErr(errors.New("downloads are not supported"))
	case !isDocument(o):
		return statusErr(fmt.Errorf("%s is not a document", api.Caption(o)))
	}
	what := "file"
	if isFolder(o) {
		what = "folder with everything in it"
	}
	dir := textInput("dir", "Directory", e.downloadDir, "")
	dir.required = true
	overwrite := choiceInput("overwrite", "Existing files", []string{"keep", "replace"}, "keep")
	f := newFormView(e, "Download "+docName(o), []*formField{dir, overwrite})
	dir.hint = "Saves the " + what + " here"
	f.submit = func(v map[string]any) tea.Cmd {
		to := f.vid
		target, err := expandHome(v["dir"].(string))
		if err == nil {
			if st, statErr := os.Stat(target); statErr == nil && !st.IsDir() {
				err = fmt.Errorf("%s is not a directory", target)
			}
		}
		return func() tea.Msg {
			return formDoneMsg{to: to, err: err, result: api.Object{"dir": target, "overwrite": v["overwrite"] == "replace"}}
		}
	}
	f.done = func(res api.Object) tea.Cmd {
		dir := res["dir"].(string)
		e.downloadDir = dir // offer the same directory next time
		return tea.Batch(pop(f.vid), startDownload(e, dl, o, dir, res["overwrite"].(bool)))
	}
	return push(f)
}

func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Abs(p)
}

// downloadMsg reports the progress or the end of a background download.
type downloadMsg struct {
	name, dir string
	file      string
	res       DownloadResult
	done      bool
	err       error
	ch        <-chan downloadMsg
}

// startDownload runs the download in a goroutine and feeds its progress
// into the status line, one message at a time.
func startDownload(e *env, dl Downloader, o api.Object, dir string, overwrite bool) tea.Cmd {
	ch := make(chan downloadMsg, 1)
	name, id, ctx := docName(o), str(o, "id"), e.ctx
	go func() {
		defer close(ch)
		res, err := dl.Download(ctx, id, dir, overwrite, func(r DownloadResult, file string) {
			// Drop progress updates the UI has not caught up with.
			select {
			case ch <- downloadMsg{name: name, dir: dir, file: file, res: r}:
			default:
			}
		})
		ch <- downloadMsg{name: name, dir: dir, res: res, done: true, err: err}
	}()
	return tea.Batch(status("Downloading "+name+" …"), waitDownload(ch))
}

func waitDownload(ch <-chan downloadMsg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-ch
		if !ok {
			return nil
		}
		m.ch = ch
		return m
	}
}

func (m downloadMsg) status() (string, bool) {
	files := func(n int) string {
		if n == 1 {
			return "1 file"
		}
		return fmt.Sprintf("%d files", n)
	}
	switch {
	case m.err != nil:
		return fmt.Sprintf("Download of %s failed after %s: %v", m.name, files(m.res.Fetched), m.err), true
	case !m.done:
		return fmt.Sprintf("Downloading %s: %s · %s", m.name, files(m.res.Fetched+m.res.Skipped), m.file), false
	}
	s := fmt.Sprintf("Downloaded %s to %s", files(m.res.Fetched), m.dir)
	if m.res.Skipped > 0 {
		s += fmt.Sprintf(" (%d already there, kept)", m.res.Skipped)
	}
	return s, false
}
