package tui

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

// theme is the complete look of the browser. Without colors (NO_COLOR or a
// dumb terminal) Bubble Tea drops the colors and keeps borders and bold.
type theme struct {
	// Header line: brand, breadcrumbs and host. bar is the background of
	// the whole line; brand, crumb and barDim must set the same background.
	bar, brand, crumb, barDim lipgloss.Style
	sep                       string

	// frame wraps the body (for example a border); zero means no frame.
	frame lipgloss.Style

	dim, status, err lipgloss.Style
	table            table.Styles
	help             help.Styles

	// Dialogs, forms and menus.
	dialog, title, label, focus, choice lipgloss.Style
}

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func helpStyles(key, desc, sep lipgloss.Style) help.Styles {
	return help.Styles{
		Ellipsis: sep, ShortKey: key, ShortDesc: desc, ShortSeparator: sep,
		FullKey: key, FullDesc: desc, FullSeparator: sep,
	}
}

func cell() lipgloss.Style { return lipgloss.NewStyle().Padding(0, 1) }

// newTheme returns the look of the browser: a purple header bar, a rounded
// border around the content and pink column titles.
func newTheme() *theme {
	purple, pink, gray := lipgloss.Color("99"), lipgloss.Color("212"), lipgloss.Color("245")
	onBar := func() lipgloss.Style { return lipgloss.NewStyle().Background(purple).Foreground(lipgloss.Color("231")) }
	return &theme{
		bar:    onBar(),
		brand:  onBar().Bold(true).Padding(0, 1),
		crumb:  onBar(),
		barDim: onBar().Foreground(lipgloss.Color("189")),
		sep:    " ❯ ",
		frame:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple),
		dim:    lipgloss.NewStyle().Foreground(gray),
		status: fg("120"),
		err:    fg("204").Bold(true),
		table: table.Styles{
			Header:   cell().Bold(true).Foreground(pink),
			Cell:     cell(),
			Selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(purple),
		},
		help: helpStyles(fg("212"), lipgloss.NewStyle().Foreground(gray), lipgloss.NewStyle().Foreground(lipgloss.Color("240"))),

		dialog: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(pink).Padding(1, 2),
		title:  lipgloss.NewStyle().Bold(true).Foreground(pink),
		label:  lipgloss.NewStyle().Foreground(gray),
		focus:  lipgloss.NewStyle().Bold(true).Foreground(pink),
		choice: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(purple).Padding(0, 1),
	}
}

// header renders the top line across the full width: the root tabs with the
// active one highlighted, then the path of opened views.
func (t *theme) header(tabs []string, active int, crumbs []string, host string, width int) string {
	tabParts := make([]string, len(tabs))
	for i, tab := range tabs {
		if i == active {
			tabParts[i] = t.crumb.Bold(true).Underline(true).Render(tab)
		} else {
			tabParts[i] = t.barDim.Render(tab)
		}
	}
	parts := []string{strings.Join(tabParts, t.barDim.Render(" │ "))}
	for _, c := range crumbs {
		parts = append(parts, t.crumb.Render(c))
	}
	left := t.brand.Render("oa") + t.crumb.Render(" ") + strings.Join(parts, t.barDim.Render(t.sep))
	right := t.barDim.Render(host + " ")
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, width)
	}
	return left + t.bar.Render(strings.Repeat(" ", gap)) + right
}
