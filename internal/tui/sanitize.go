package tui

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// cleanText removes terminal control characters from text that comes from
// the server. Documents from leaks can contain escape sequences that would
// otherwise reach the terminal and, for example, change its title or write
// to the clipboard. Newlines and tabs stay; carriage returns become newlines.
//
// Bytes that are not valid UTF-8 are dropped too: a lone 0x9b is an 8-bit
// control sequence introducer for some terminals.
func cleanText(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, isControl) && !strings.Contains(s, "\t") {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
		case r == '\n':
			b.WriteRune(r)
		case r == '\r':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case isControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isControl matches C0 and C1 control characters and DEL, including ESC.
func isControl(r rune) bool {
	return r < 0x20 && r != '\n' || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// cleanValue applies cleanText to all strings in a decoded JSON value.
func cleanValue(v any) any {
	switch t := v.(type) {
	case string:
		return cleanText(t)
	case map[string]any:
		for k, x := range t {
			t[k] = cleanValue(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = cleanValue(x)
		}
		return t
	}
	return v
}

func cleanPage(p Page) Page {
	for _, it := range p.Items {
		cleanValue(it)
	}
	return p
}

var (
	reHTMLDrop  = regexp.MustCompile(`(?is)<(script|style|head|title)\b.*?</(script|style|head|title)\s*>`)
	reHTMLSpace = regexp.MustCompile(`\s+`)
	reHTMLBreak = regexp.MustCompile(`(?i)<\s*/?\s*(br|p|div|tr|table|ul|ol|h[1-6]|blockquote|hr)\b[^>]*>`)
	reHTMLItem  = regexp.MustCompile(`(?i)<\s*li\b[^>]*>`)
	reHTMLCell  = regexp.MustCompile(`(?i)<\s*/t[dh]\s*>`)
	reHTMLTag   = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank     = regexp.MustCompile(`\n{3,}`)
	reSpaces    = regexp.MustCompile(`[ \t\x{a0}]+`)
)

// htmlToText turns an HTML body into readable plain text: block elements
// become line breaks, tags are dropped and entities are decoded. It is meant
// for reading in a terminal, not for faithful rendering.
func htmlToText(s string) string {
	s = reHTMLDrop.ReplaceAllString(s, "")
	// Line breaks in HTML source are spaces; only elements break lines.
	s = reHTMLSpace.ReplaceAllString(s, " ")
	s = reHTMLBreak.ReplaceAllString(s, "\n")
	s = reHTMLItem.ReplaceAllString(s, "\n• ")
	s = reHTMLCell.ReplaceAllString(s, "\t")
	s = reHTMLTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = reBlank.ReplaceAllString(s, "\n\n")
	return cleanText(strings.TrimSpace(s))
}
