package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// DocumentReader loads the content of documents. A Backend may implement
// it; without it, only the text stored on the entity is shown.
type DocumentReader interface {
	// Pages returns the Page entities of a PDF-like document, in order.
	Pages(ctx context.Context, documentID string, offset int) (Page, error)
	// CSV reads up to maxRows rows of the CSV file at url. more reports
	// whether the file has more rows.
	CSV(ctx context.Context, url string, maxRows int) (rows [][]string, more bool, err error)
	// AttachmentCounts counts the documents directly below each of the
	// given documents (the attachments of emails), in one request.
	AttachmentCounts(ctx context.Context, ids []string) (map[string]int, error)
	// Thread returns the emails of the thread of email, oldest first.
	Thread(ctx context.Context, email api.Object) ([]api.Object, error)
}

// maxTableRows is how many rows of a table are loaded for display.
const maxTableRows = 1000

// pane is the content area of the entity view.
type pane interface {
	init() tea.Cmd
	update(tea.Msg) tea.Cmd
	render() string
	resize(width, height int)
	keys() []key.Binding
	capturing() bool
}

var (
	keyFind     = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "find"))
	keyNextHit  = key.NewBinding(key.WithKeys("n"), key.WithHelp("n/N", "next"))
	keyPrevHit  = key.NewBinding(key.WithKeys("N"))
	keyCopyBody = key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "copy text"))
	keyColLeft  = key.NewBinding(key.WithKeys("<", ","), key.WithHelp("</>", "columns"))
	keyColRight = key.NewBinding(key.WithKeys(">", "."))
)

// newContentPane picks the content view for a document, or returns nil if
// there is nothing to show besides the properties.
func newContentPane(e *env, to int, doc api.Object) pane {
	reader, _ := e.backend.(DocumentReader)
	links, _ := doc["links"].(map[string]any)
	switch schema := str(doc, "schema"); {
	case schema == "Email" || schema == "Message":
		return newEmailPane(e, to, doc)
	case schema == "Table" && reader != nil:
		u, _ := links["csv"].(string)
		if u == "" && strings.Contains(firstProp(doc, "mimeType"), "csv") {
			u, _ = links["file"].(string)
		}
		if u != "" {
			return newTablePane(e, to, reader, doc, u)
		}
	case schema == "Pages" && reader != nil:
		p := newTextPane(e, "")
		p.pages = &pagesLoader{ctx: e.ctx, reader: reader, doc: str(doc, "id"), to: to, fallback: bodyText(doc)}
		return p
	}
	if t := bodyText(doc); t != "" {
		return newTextPane(e, t)
	}
	return nil
}

func props(o api.Object, name string) []string {
	p, _ := o["properties"].(map[string]any)
	vals, _ := p[name].([]any)
	var out []string
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstProp(o api.Object, name string) string {
	if vals := props(o, name); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// bodyText is the readable text of a document: its text, or else its HTML
// (sanitized by the server, as safeHtml) as text.
func bodyText(doc api.Object) string {
	if t := strings.Join(props(doc, "bodyText"), "\n\n"); strings.TrimSpace(t) != "" {
		return t
	}
	var html []string
	if vals, ok := doc["safeHtml"].([]any); ok {
		for _, v := range vals {
			if s, ok := v.(string); ok {
				html = append(html, s)
			}
		}
	}
	if len(html) == 0 {
		html = props(doc, "bodyHtml")
	}
	return htmlToText(strings.Join(html, "\n"))
}

// emailText renders the headers of an email like a mail client, followed by
// the body. attach is the attachments line ("" for none).
func emailText(doc api.Object, attach string) string {
	var b strings.Builder
	for _, h := range []struct{ label, prop string }{
		{"From", "from"}, {"Date", "date"}, {"Subject", "subject"}, {"To", "to"},
		{"Cc", "cc"}, {"Bcc", "bcc"}, {"Reply to", "inReplyToEmail"},
	} {
		if vals := props(doc, h.prop); len(vals) > 0 {
			fmt.Fprintf(&b, "%-9s %s\n", h.label+":", strings.Join(vals, ", "))
		}
	}
	if attach != "" {
		fmt.Fprintf(&b, "%-9s %s\n", "Attach:", attach)
	}
	b.WriteString(strings.Repeat("─", 40) + "\n\n")
	body := bodyText(doc)
	if body == "" {
		body = "(no text)"
	}
	b.WriteString(body)
	return b.String()
}

// textPane shows text with search (/, n, N). For PDFs it loads the text page
// by page as the reader scrolls down.
type textPane struct {
	env    *env
	vp     viewport.Model
	text   string
	input  textinput.Model
	query  string
	hits   int
	width  int
	height int
	pages  *pagesLoader
}

func newTextPane(e *env, text string) *textPane {
	vp := viewport.New()
	vp.SoftWrap = true
	// Indent like the table cells.
	vp.Style = lipgloss.NewStyle().PaddingLeft(1)
	vp.KeyMap.PageDown = navKeys.PageDown
	vp.KeyMap.PageUp = navKeys.PageUp
	vp.KeyMap.HalfPageDown = navKeys.HalfPageDown
	vp.KeyMap.HalfPageUp = navKeys.HalfPageUp
	vp.HighlightStyle = e.theme.highlight
	vp.SelectedHighlightStyle = e.theme.selectedHighlight
	in := textinput.New()
	in.Prompt = "find: "
	p := &textPane{env: e, vp: vp, input: in}
	p.setText(text)
	return p
}

func (p *textPane) setText(text string) {
	p.text = text
	p.vp.SetContent(text)
	p.find()
}

// find highlights all case-insensitive matches of the query.
func (p *textPane) find() {
	p.vp.ClearHighlights()
	p.hits = 0
	if p.query == "" {
		return
	}
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(p.query))
	matches := re.FindAllStringIndex(p.text, -1)
	p.hits = len(matches)
	p.vp.SetHighlights(matches)
}

func (p *textPane) init() tea.Cmd {
	if p.pages != nil {
		return p.pages.load()
	}
	return nil
}

func (p *textPane) capturing() bool { return p.input.Focused() }

func (p *textPane) keys() []key.Binding {
	return []key.Binding{keyFind, keyNextHit, keyCopyBody}
}

func (p *textPane) resize(width, height int) {
	p.width, p.height = width, height
	p.vp.SetWidth(width)
	p.vp.SetHeight(max(height-1, 1))
	p.input.SetWidth(max(width-10, 10))
}

func (p *textPane) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pagesMsg:
		if p.pages == nil {
			return nil
		}
		if text, ok := p.pages.add(msg); ok {
			p.setText(text)
		}
		return p.loadMore()
	case tea.KeyPressMsg:
		if p.input.Focused() {
			switch msg.String() {
			case "enter":
				p.input.Blur()
				p.query = p.input.Value()
				p.find()
				if p.query != "" && p.hits == 0 {
					return statusErr(fmt.Errorf("%q not found", p.query))
				}
				return nil
			case "esc":
				p.input.Blur()
				p.input.SetValue(p.query)
				return nil
			}
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return cmd
		}
		switch {
		case key.Matches(msg, keyFind):
			p.input.SetValue("")
			return p.input.Focus()
		case key.Matches(msg, keyNextHit):
			p.vp.HighlightNext()
			return nil
		case key.Matches(msg, keyPrevHit):
			p.vp.HighlightPrevious()
			return nil
		case key.Matches(msg, keyCopyBody):
			return copyText(p.text, "Copied the text")
		case key.Matches(msg, navKeys.GotoTop):
			p.vp.GotoTop()
			return nil
		case key.Matches(msg, navKeys.GotoBottom):
			p.vp.GotoBottom()
			return p.loadMore()
		}
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return tea.Batch(cmd, p.loadMore())
	}
	if p.input.Focused() {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return cmd
	}
	return nil
}

// loadMore fetches the next pages when the end of the loaded text is near.
func (p *textPane) loadMore() tea.Cmd {
	if p.pages == nil || !p.pages.more || p.pages.loading {
		return nil
	}
	if p.vp.TotalLineCount()-p.vp.YOffset() > 3*p.vp.Height() {
		return nil
	}
	return p.pages.load()
}

func (p *textPane) render() string {
	t := p.env.theme
	var info string
	switch {
	case p.input.Focused():
		info = " " + p.input.View()
	default:
		info = fmt.Sprintf(" %3.0f%%", p.vp.ScrollPercent()*100)
		if p.pages != nil {
			info += " · " + p.pages.info()
		}
		if p.query != "" {
			info += fmt.Sprintf(" · %d hits for %q", p.hits, p.query)
		}
		info = t.dim.Render(truncate(info, p.width))
	}
	return p.vp.View() + "\n" + info
}

type pagesMsg struct {
	to, offset int
	page       Page
	err        error
}

func (m pagesMsg) target() int { return m.to }

// pagesLoader collects the text of a PDF page by page.
type pagesLoader struct {
	ctx      context.Context
	reader   DocumentReader
	doc      string
	to       int
	fallback string
	text     strings.Builder
	loaded   int
	total    int
	more     bool
	loading  bool
	err      error
}

func (l *pagesLoader) load() tea.Cmd {
	l.loading = true
	ctx, r, doc, to, offset := l.ctx, l.reader, l.doc, l.to, l.loaded
	return func() tea.Msg {
		p, err := r.Pages(ctx, doc, offset)
		return pagesMsg{to: to, offset: offset, page: cleanPage(p), err: err}
	}
}

// add appends a loaded batch of pages and returns the whole text.
func (l *pagesLoader) add(m pagesMsg) (string, bool) {
	if m.offset != l.loaded {
		return "", false
	}
	l.loading = false
	if m.err != nil {
		l.err, l.more = m.err, false
		return "", false
	}
	if l.loaded == 0 && len(m.page.Items) == 0 {
		// No page entities (for example not processed yet): show the
		// document text, if any.
		l.more = false
		return l.fallback, true
	}
	for _, pg := range m.page.Items {
		if l.text.Len() > 0 {
			l.text.WriteString("\n\n")
		}
		n := firstProp(pg, "index")
		if n == "" {
			n = fmt.Sprint(l.loaded + 1)
		}
		fmt.Fprintf(&l.text, "─── page %s ───\n\n%s", n, strings.TrimSpace(firstProp(pg, "bodyText")))
		l.loaded++
	}
	l.total = m.page.Total
	l.more = m.page.More && len(m.page.Items) > 0 && l.loaded < MaxResults
	return l.text.String(), true
}

func (l *pagesLoader) info() string {
	switch {
	case l.err != nil:
		return "pages: " + l.err.Error()
	case l.loading && l.loaded == 0:
		return "loading pages…"
	case l.total == 0:
		return "no pages"
	case l.more:
		return fmt.Sprintf("%d of %d pages loaded", l.loaded, l.total)
	}
	return fmt.Sprintf("%d pages", l.total)
}

type csvMsg struct {
	to   int
	rows [][]string
	more bool
	err  error
}

func (m csvMsg) target() int { return m.to }

// tablePane shows the rows of a table document. Wide tables scroll
// sideways with < and >.
type tablePane struct {
	env      *env
	to       int
	reader   DocumentReader
	url      string
	header   []string
	rows     [][]string
	more     bool
	rowCount string
	loading  bool
	err      error
	table    table.Model
	offset   int // first visible column
	visible  int
	width    int
	height   int
}

func newTablePane(e *env, to int, r DocumentReader, doc api.Object, u string) *tablePane {
	t := table.New(table.WithKeyMap(navKeys), table.WithStyles(e.theme.table), table.WithFocused(true))
	p := &tablePane{env: e, to: to, reader: r, url: u, table: t, loading: true, rowCount: firstProp(doc, "rowCount")}
	// The column names are stored as a JSON list.
	if cols := firstProp(doc, "columns"); cols != "" {
		var names []string
		if json.Unmarshal([]byte(cols), &names) == nil {
			p.header = names
		}
	}
	return p
}

func (p *tablePane) init() tea.Cmd {
	ctx, r, u, to := p.env.ctx, p.reader, p.url, p.to
	return func() tea.Msg {
		rows, more, err := r.CSV(ctx, u, maxTableRows)
		for _, row := range rows {
			for i, c := range row {
				row[i] = cleanText(c)
			}
		}
		return csvMsg{to: to, rows: rows, more: more, err: err}
	}
}

func (p *tablePane) capturing() bool     { return false }
func (p *tablePane) keys() []key.Binding { return []key.Binding{keyColLeft} }

func (p *tablePane) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case csvMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return statusErr(msg.err)
		}
		p.rows, p.more = msg.rows, msg.more
		if len(p.header) == 0 && len(p.rows) > 0 {
			// Like the web UI: without column names, the first row is the header.
			p.header, p.rows = p.rows[0], p.rows[1:]
		}
		p.layout()
		return nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyColLeft):
			p.offset = max(p.offset-1, 0)
			p.layout()
			return nil
		case key.Matches(msg, keyColRight):
			p.offset = min(p.offset+1, max(len(p.header)-1, 0))
			p.layout()
			return nil
		}
		var cmd tea.Cmd
		p.table, cmd = p.table.Update(msg)
		return cmd
	}
	return nil
}

func (p *tablePane) resize(width, height int) {
	p.width, p.height = width, height
	p.layout()
}

// layout fits as many columns as the width allows, starting at offset.
func (p *tablePane) layout() {
	widths := make([]int, len(p.header))
	for i, h := range p.header {
		widths[i] = len([]rune(h))
	}
	for _, r := range p.rows {
		for i := range min(len(r), len(widths)) {
			widths[i] = max(widths[i], len([]rune(oneLine(r[i]))))
		}
	}
	var cols []table.Column
	used := 0
	for i := p.offset; i < len(p.header); i++ {
		w := min(max(widths[i], 3), 30)
		if len(cols) > 0 && used+w+2 > p.width {
			break
		}
		cols = append(cols, table.Column{Title: p.header[i], Width: min(w, max(p.width-2, 3))})
		used += w + 2
	}
	p.visible = len(cols)
	rows := make([]table.Row, len(p.rows))
	for i, r := range p.rows {
		row := make(table.Row, len(cols))
		for j := range cols {
			if k := p.offset + j; k < len(r) {
				row[j] = oneLine(r[k])
			}
		}
		rows[i] = row
	}
	p.table.SetRows(nil)
	p.table.SetColumns(cols)
	p.table.SetStyles(p.env.theme.table)
	p.table.SetWidth(p.width)
	p.table.SetHeight(max(p.height-1, 2))
	p.table.SetRows(rows)
	// The cursor of an empty table is -1.
	if p.table.Cursor() < 0 && len(rows) > 0 {
		p.table.SetCursor(0)
	}
}

func (p *tablePane) render() string {
	var info string
	switch {
	case p.loading:
		info = "loading table…"
	case p.err != nil:
		info = "table: " + p.err.Error()
	default:
		info = fmt.Sprintf("row %d of %s", p.table.Cursor()+1, humanize.Comma(int64(len(p.rows))))
		if p.more {
			info += " loaded"
			if p.rowCount != "" {
				info += " (" + p.rowCount + " in the file)"
			}
		}
		if len(p.header) > p.visible {
			info += fmt.Sprintf(" · columns %d–%d of %d · < > scroll", p.offset+1, p.offset+p.visible, len(p.header))
		}
	}
	return p.table.View() + "\n" + p.env.theme.dim.Render(truncate(" "+info, p.width))
}

var keyAttachments = key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "attach"))

type attachmentsMsg struct {
	to   int
	page Page
	err  error
}

func (m attachmentsMsg) target() int { return m.to }

// emailPane is the text of an email with its attachments: they are listed in
// the headers, and A opens them as a list.
type emailPane struct {
	*textPane
	to          int
	doc         api.Object
	attachments []api.Object
	total       int
}

func newEmailPane(e *env, to int, doc api.Object) *emailPane {
	return &emailPane{textPane: newTextPane(e, emailText(doc, "")), to: to, doc: doc}
}

// init loads the attachments: the documents whose parent the email is.
func (p *emailPane) init() tea.Cmd {
	ctx, b, to, q := p.env.ctx, p.env.backend, p.to, EntityQuery{Parent: str(p.doc, "id")}
	return func() tea.Msg {
		page, err := b.Entities(ctx, q, 0)
		return attachmentsMsg{to: to, page: cleanPage(page), err: err}
	}
}

func (p *emailPane) keys() []key.Binding {
	ks := p.textPane.keys()
	if _, ok := p.env.backend.(DocumentReader); ok {
		ks = append([]key.Binding{keyThread}, ks...)
	}
	if p.total > 0 {
		ks = append([]key.Binding{keyAttachments}, ks...)
	}
	return ks
}

func (p *emailPane) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case attachmentsMsg:
		if msg.err != nil {
			return statusErr(fmt.Errorf("attachments: %w", msg.err))
		}
		p.attachments, p.total = msg.page.Items, msg.page.Total
		p.setText(emailText(p.doc, p.attachLine()))
		return nil
	case tea.KeyPressMsg:
		if r, ok := p.env.backend.(DocumentReader); ok && !p.capturing() && key.Matches(msg, keyThread) {
			return push(newThreadView(p.env, r, p.doc))
		}
		if !p.capturing() && key.Matches(msg, keyAttachments) {
			if p.total == 0 {
				return status("No attachments")
			}
			c, _ := p.doc["collection"].(map[string]any)
			return push(newDocumentsView(p.env, c, p.doc))
		}
	}
	return p.textPane.update(msg)
}

// attachLine names the attachments with their sizes, like a mail client.
func (p *emailPane) attachLine() string {
	if p.total == 0 {
		return ""
	}
	names := make([]string, 0, len(p.attachments))
	for _, a := range p.attachments {
		n := docName(a)
		if size := fileSize(a); size != "" {
			n += " (" + size + ")"
		}
		names = append(names, n)
	}
	line := strings.Join(names, ", ")
	if more := p.total - len(p.attachments); more > 0 {
		line += fmt.Sprintf(" and %d more", more)
	}
	return line + "  · A opens them"
}
