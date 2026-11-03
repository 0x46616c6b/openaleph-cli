package tui

import (
	"testing"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"plain":                            "plain",
		"a\x1b]52;c;ZXZpbA==\x07b":         "a]52;c;ZXZpbA==b",
		"title\x1b]0;pwned\x1b\\":          "title]0;pwned\\",
		"red\x1b[31mtext":                  "red[31mtext",
		"line1\r\nline2\rline3":            "line1\nline2\nline3",
		"tab\there":                        "tab    here",
		"c1\u009bcontrol\u0085x\u007fy":    "c1controlxy",
		"umlauts äöü and emoji 🙂 stay\nok": "umlauts äöü and emoji 🙂 stay\nok",
		"raw\x9bbyte\xffend":               "rawbyteend",
	}
	for in, want := range cases {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanValue(t *testing.T) {
	o := api.Object{"caption": "x\x1b[2Jy", "properties": map[string]any{"name": []any{"a\x07b"}, "n": []any{1.0}}}
	cleanValue(o)
	if o["caption"] != "x[2Jy" || o["properties"].(map[string]any)["name"].([]any)[0] != "ab" {
		t.Fatalf("cleaned = %v", o)
	}
}

func TestHTMLToText(t *testing.T) {
	in := `<html><head><title>T</title><style>p{}</style></head><body>
		<p>Dear&nbsp;Jane,</p><p>see   the <b>offer</b>:<br>
		<ul><li>one</li><li>two &amp; three</li></ul>
		<table><tr><td>a</td><td>b</td></tr></table>
		<script>alert(1)</script>Bye</body></html>`
	want := "Dear Jane,\n\nsee the offer:\n\n• one\n• two & three\n\na b\n\nBye"
	if got := htmlToText(in); got != want {
		t.Fatalf("htmlToText =\n%q\nwant\n%q", got, want)
	}
}
