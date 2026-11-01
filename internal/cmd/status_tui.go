package cmd

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"

	progressWidth = 12
	maxLabelWidth = 40
)

type cell struct {
	text  string
	style string
	right bool
}

// renderStatusTUI draws a bordered, colored table with progress bars and a
// footer. Lines end in clear-to-EOL plus CRLF so it can be repainted in place
// while the terminal is in raw mode.
func renderStatusTUI(rows []any, now time.Time, interval time.Duration, color bool) string {
	paint := func(style, s string) string {
		if !color || style == "" {
			return s
		}
		return style + s + ansiReset
	}
	header := []cell{
		{text: "ID"}, {text: "COLLECTION"}, {text: "PROGRESS"},
		{text: "TODO", right: true}, {text: "DOING", right: true}, {text: "FAILED", right: true},
		{text: "DONE", right: true}, {text: "REMAINING", right: true},
	}
	for i := range header {
		header[i].style = ansiBold
	}
	var body [][]cell
	var done, total float64
	for _, r := range rows {
		o, _ := r.(map[string]any)
		todo, doing, failed, fin := num(o, "todo"), num(o, "doing"), num(o, "failed"), num(o, "finished")
		all := todo + doing + failed + fin
		done += fin + failed
		total += all
		ratio := 0.0
		if all > 0 {
			ratio = (fin + failed) / all
		}
		label := statusLabel(o)
		if utf8.RuneCountInString(label) > maxLabelWidth {
			label = string([]rune(label)[:maxLabelWidth-1]) + "…"
		}
		var failStyle, doingStyle string
		if failed > 0 {
			failStyle = ansiRed
		}
		if doing > 0 {
			doingStyle = ansiYellow
		}
		body = append(body, []cell{
			{text: statusID(o), style: ansiDim},
			{text: label, style: ansiCyan},
			{text: bar(ratio), style: ansiGreen},
			{text: str(o, "todo"), right: true},
			{text: str(o, "doing"), right: true, style: doingStyle},
			{text: str(o, "failed"), right: true, style: failStyle},
			{text: str(o, "finished"), right: true, style: ansiGreen},
			{text: remaining(str(o, "remaining_time")), right: true},
		})
	}

	widths := make([]int, len(header))
	for _, line := range append([][]cell{header}, body...) {
		for i, c := range line {
			if n := utf8.RuneCountInString(c.text); n > widths[i] {
				widths[i] = n
			}
		}
	}
	border := func(l, m, r string) string {
		parts := make([]string, len(widths))
		for i, w := range widths {
			parts[i] = strings.Repeat("─", w+2)
		}
		return paint(ansiDim, l+strings.Join(parts, m)+r)
	}
	line := func(cells []cell) string {
		sep := paint(ansiDim, "│")
		var b strings.Builder
		b.WriteString(sep)
		for i, c := range cells {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.text))
			text := paint(c.style, c.text)
			if c.right {
				text = pad + text
			} else {
				text += pad
			}
			b.WriteString(" " + text + " " + sep)
		}
		return b.String()
	}

	pct := 0.0
	if total > 0 {
		pct = done / total
	}
	var out []string
	out = append(out,
		paint(ansiBold, "Processing status")+paint(ansiDim, fmt.Sprintf("  ·  %d collection(s)  ·  %.0f%% overall  ·  refresh %s  ·  %s", len(rows), pct*100, interval, now.Format("15:04:05"))),
		"",
		border("┌", "┬", "┐"),
		line(header),
		border("├", "┼", "┤"),
	)
	for _, b := range body {
		out = append(out, line(b))
	}
	out = append(out, border("└", "┴", "┘"), "", paint(ansiDim, "Press q to quit"))
	return strings.Join(out, "\x1b[K\r\n") + "\x1b[K\r\n"
}

// bar renders a fixed-width progress bar followed by the percentage.
func bar(ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*progressWidth + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", progressWidth-filled) + fmt.Sprintf(" %3.0f%%", ratio*100)
}
