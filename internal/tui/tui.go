// Package tui is the interactive terminal browser behind `oa browse`.
//
// The UI is a stack of views (collections, entity lists, entity details, text).
// Enter pushes a view, esc pops it. Data is loaded asynchronously through a
// Backend, so the package has no HTTP details and can be tested with a fake.
package tui

import (
	"context"
	"errors"
	"io"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// Page is one page of a listing.
type Page struct {
	Items []api.Object
	Total int
	// TotalAtLeast marks Total as a lower bound (the index stopped counting).
	TotalAtLeast bool
	More         bool
}

// MaxResults is how far OpenAleph pages into a result list (MAX_PAGE in
// aleph/index/util.py). Requests beyond it come back empty.
const MaxResults = 9999

// Backend loads the data shown in the browser.
type Backend interface {
	// Collections lists datasets, or with investigations set only
	// investigations (collections of the category casefile).
	Collections(ctx context.Context, query string, investigations bool, offset int) (Page, error)
	// Entities searches entities.
	Entities(ctx context.Context, q EntityQuery, offset int) (Page, error)
	// SchemaCounts counts the entities matching q per schema, over all
	// schemas (q.Schemas is ignored), for the schema filter.
	SchemaCounts(ctx context.Context, q EntityQuery) ([]api.FacetValue, error)
	Entity(ctx context.Context, id string) (api.Object, error)
	// Status returns the rows of /status: collections with pending tasks.
	Status(ctx context.Context) ([]api.Object, error)
}

// EntityQuery selects entities.
type EntityQuery struct {
	Query string
	// CollectionID limits the search to one collection; empty searches all.
	CollectionID string
	// Schemas limits the search to these schemas; empty means all Things.
	Schemas []string
}

// Options configures Run.
type Options struct {
	Backend Backend
	// Host is shown in the header.
	Host string
	// Collection, if set, starts the browser in the entities of this collection.
	Collection api.Object
	// Query, if set, starts the browser with an entity search (within
	// Collection, if that is set too).
	Query string
	// Schemas filters the start entity list (with Collection or Query).
	Schemas []string
	// Status starts the browser in the processing status.
	Status bool
	// StatusInterval is how often the status refreshes (default 3s).
	StatusInterval time.Duration
	// Open opens a URL in the web browser.
	Open func(url string) error
	In   io.Reader
	Out  io.Writer
}

// Run starts the browser and blocks until the user quits.
func Run(ctx context.Context, o Options) error {
	p := tea.NewProgram(newModel(ctx, o), tea.WithContext(ctx), tea.WithInput(o.In), tea.WithOutput(o.Out))
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

// env is shared by all views of one browser session.
type env struct {
	ctx            context.Context
	backend        Backend
	open           func(string) error
	statusInterval time.Duration
	theme          *theme
	seq            int
	// topID is the ID of the visible view.
	topID int
}

func (e *env) nextID() int {
	e.seq++
	return e.seq
}

// view is one screen of the stack. Views are pointers and update in place.
type view interface {
	id() int
	title() string
	init() tea.Cmd
	update(tea.Msg) tea.Cmd
	render() string
	resize(width, height int)
	// keys lists the view's own bindings for the help.
	keys() []key.Binding
	// capturing reports whether a text input has the focus, so that global
	// keys like q and esc go to the input instead.
	capturing() bool
}

// Messages.
type (
	pushMsg   struct{ v view }
	popMsg    struct{ id int }
	statusMsg struct {
		text string
		err  bool
	}
)

// targeted messages go to the view with the given ID, wherever it is on the
// stack. Messages for views that were popped are dropped.
type targeted interface{ target() int }

func push(v view) tea.Cmd { return func() tea.Msg { return pushMsg{v} } }

func pop(id int) tea.Cmd { return func() tea.Msg { return popMsg{id} } }

func status(text string) tea.Cmd { return func() tea.Msg { return statusMsg{text: text} } }

func statusErr(err error) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: err.Error(), err: true} }
}

type globalKeys struct {
	Back, Switch, Status, Help, Quit key.Binding
}

var global = globalKeys{
	Back:   key.NewBinding(key.WithKeys("esc", "backspace", "h", "left"), key.WithHelp("esc/h", "back")),
	Switch: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")),
	Status: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "status")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type model struct {
	env *env
	// roots are the datasets and the investigations list. The active one is
	// at the bottom of the stack; tab switches on the first screen.
	roots    [2]*listView
	root     int
	started  [2]bool
	host     string
	stack    []view
	width    int
	height   int
	help     help.Model
	showHelp bool
	status   string
	isErr    bool
}

func newModel(ctx context.Context, o Options) model {
	e := &env{ctx: ctx, backend: o.Backend, open: o.Open, statusInterval: o.StatusInterval}
	if e.statusInterval <= 0 {
		e.statusInterval = defaultStatusInterval
	}
	e.theme = newTheme()
	m := model{env: e, host: o.Host, help: help.New()}
	m.help.Styles = e.theme.help
	m.roots = [2]*listView{newCollectionsView(e, false), newCollectionsView(e, true)}
	if o.Collection != nil && isInvestigation(o.Collection) {
		m.root = 1
	}
	m.started[m.root] = true
	m.stack = []view{m.roots[m.root]}
	switch {
	case o.Collection != nil:
		m.stack = append(m.stack, newEntitiesView(e, o.Collection, o.Query, o.Schemas))
	case o.Query != "" || len(o.Schemas) > 0:
		m.stack = append(m.stack, newEntitiesView(e, nil, o.Query, o.Schemas))
	case o.Status:
		m.stack = append(m.stack, newStatusView(e))
	}
	return m
}

func (m model) top() view { return m.stack[len(m.stack)-1] }

func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.stack))
	for _, v := range m.stack {
		cmds = append(cmds, v.init())
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := m.update(msg)
	m.env.topID = m.top().id()
	return m, cmd
}

// resumer is a view that wants to know when it is shown again after the
// view above it was closed.
type resumer interface{ resumed() tea.Cmd }

// popTop closes the top view and tells the view below.
func (m *model) popTop() tea.Cmd {
	m.stack = m.stack[:len(m.stack)-1]
	m.layout()
	if r, ok := m.top().(resumer); ok {
		return r.resumed()
	}
	return nil
}

func (m model) update(msg tea.Msg) (model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// The footer is indented by one column.
		m.help.SetWidth(max(msg.Width-1, 1))
		m.layout()
		return m, nil
	case tea.KeyPressMsg:
		m.status, m.isErr = "", false
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+z":
			return m, tea.Suspend
		}
		if !m.top().capturing() {
			switch {
			case key.Matches(msg, global.Quit):
				return m, tea.Quit
			case key.Matches(msg, global.Help):
				m.showHelp = !m.showHelp
				m.layout()
				return m, nil
			case len(m.stack) == 1 && key.Matches(msg, global.Switch):
				m.root = 1 - m.root
				m.stack[0] = m.roots[m.root]
				m.layout()
				if !m.started[m.root] {
					m.started[m.root] = true
					return m, m.roots[m.root].init()
				}
				return m, nil
			case key.Matches(msg, global.Status):
				if _, ok := m.top().(*statusView); !ok {
					return m, push(newStatusView(m.env))
				}
				return m, nil
			case key.Matches(msg, global.Back):
				if len(m.stack) > 1 {
					return m, m.popTop()
				}
				return m, nil
			}
		}
		return m, m.top().update(msg)
	case popMsg:
		// Only pop the view that asked for it, in case others were pushed since.
		if len(m.stack) > 1 && m.top().id() == msg.id {
			return m, m.popTop()
		}
		return m, nil
	case pushMsg:
		m.stack = append(m.stack, msg.v)
		m.layout()
		return m, msg.v.init()
	case statusMsg:
		m.status, m.isErr = msg.text, msg.err
		return m, nil
	case targeted:
		for _, v := range m.stack {
			if v.id() == msg.target() {
				return m, v.update(msg)
			}
		}
		return m, nil
	}
	return m, m.top().update(msg)
}

// layout gives every view the space between header and footer, inside the
// frame of the theme.
func (m *model) layout() {
	w, h := m.bodySize()
	for _, v := range m.stack {
		v.resize(w, h)
	}
}

func (m model) bodySize() (width, height int) {
	f := m.env.theme.frame
	width = m.width - f.GetHorizontalFrameSize()
	height = m.height - 1 - lipgloss.Height(m.footer()) - f.GetVerticalFrameSize()
	return max(width, 1), max(height, 1)
}

func (m model) header() string {
	// Both tabs on the first screen; deeper in only the active one, so the
	// path reads "Datasets ❯ Leak A".
	tabs := []string{m.roots[m.root].title()} // with the query, if any
	active := 0
	if len(m.stack) == 1 {
		tabs = []string{m.roots[0].name, m.roots[1].name}
		tabs[m.root], active = m.roots[m.root].title(), m.root
	}
	crumbs := make([]string, 0, len(m.stack)-1)
	for _, v := range m.stack[1:] {
		crumbs = append(crumbs, v.title())
	}
	return m.env.theme.header(tabs, active, crumbs, m.host, m.width)
}

func (m model) footer() string {
	if m.showHelp {
		return m.help.FullHelpView(m.FullHelp())
	}
	if m.status != "" {
		st := m.env.theme.status
		if m.isErr {
			st = m.env.theme.err
		}
		return truncate(st.Render(" "+m.status), m.width)
	}
	return truncate(" "+m.help.ShortHelpView(m.ShortHelp()), m.width)
}

func (m model) View() tea.View {
	var content string
	if m.width > 0 {
		// Clip the body, so that columns wider than a small terminal cut off
		// on the right instead of wrapping.
		w, h := m.bodySize()
		body := lipgloss.NewStyle().MaxWidth(w).MaxHeight(h).Render(m.top().render())
		content = lipgloss.JoinVertical(lipgloss.Left, m.header(), m.env.theme.frame.Render(body), m.footer())
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "oa · " + m.top().title()
	return v
}

// secondaryKeys are left out of the footer to keep it short; ? lists them.
var secondaryKeys = map[string]bool{
	"y": true, "o": true, "r": true, "J": true, "s": true, "F": true,
	"n": true, "e": true, "D": true, "i": true, "I": true, // in the a menu
}

// ShortHelp implements help.KeyMap.
func (m model) ShortHelp() []key.Binding {
	// Forms and dialogs take all keys, so global keys do not apply.
	if m.top().capturing() {
		if _, ok := m.top().(*listView); !ok {
			return m.top().keys()
		}
	}
	var ks []key.Binding
	for _, k := range m.top().keys() {
		if !secondaryKeys[k.Help().Key] {
			ks = append(ks, k)
		}
	}
	if _, ok := m.top().(*statusView); !ok {
		ks = append(ks, global.Status)
	}
	if len(m.stack) > 1 {
		ks = append(ks, global.Back)
	} else {
		ks = append(ks, global.Switch)
	}
	return append(ks, global.Help, global.Quit)
}

// FullHelp implements help.KeyMap.
func (m model) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		m.top().keys(),
		{navKeys.LineUp, navKeys.LineDown, navKeys.GotoTop, navKeys.GotoBottom},
		{navKeys.PageUp, navKeys.PageDown, navKeys.HalfPageUp, navKeys.HalfPageDown},
		{global.Back, global.Switch, global.Status, global.Help, global.Quit},
	}
}
