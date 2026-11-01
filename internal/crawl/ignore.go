package crawl

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IgnoreFileName is read from the crawled directory.
const IgnoreFileName = ".openalephignore"

// Ignorer decides which paths are skipped. Its matching follows the Python
// client: patterns are fnmatch globs against the path relative to the root
// (where * also matches /), and a trailing slash ignores a directory and
// everything below it.
type Ignorer struct {
	patterns []string
	res      map[string]*regexp.Regexp
}

// NewIgnorer returns an Ignorer for the given patterns plus the files the
// crawler itself creates.
func NewIgnorer(patterns ...string) *Ignorer {
	ig := &Ignorer{res: map[string]*regexp.Regexp{}}
	ig.patterns = append(ig.patterns, stateFileName, IgnoreFileName, failedFileName)
	ig.patterns = append(ig.patterns, patterns...)
	return ig
}

// LoadIgnorer reads .openalephignore from root. A missing file is fine.
func LoadIgnorer(root string) (*Ignorer, error) {
	b, err := os.ReadFile(filepath.Join(root, IgnoreFileName))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var patterns []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return NewIgnorer(patterns...), nil
}

// Ignored reports whether rel (slash-separated, relative to the root) is skipped.
func (ig *Ignorer) Ignored(rel string, isDir bool) bool {
	for _, p := range ig.patterns {
		if strings.HasSuffix(p, "/") && isDir {
			prefix := strings.TrimRight(p, "/")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
		} else if ig.match(rel, p) {
			return true
		}
	}
	return false
}

func (ig *Ignorer) match(name, pat string) bool {
	re, ok := ig.res[pat]
	if !ok {
		re = translate(pat)
		ig.res[pat] = re
	}
	return re != nil && re.MatchString(name)
}

// translate converts an fnmatch pattern into a regular expression, like
// Python's fnmatch.translate. It returns nil for patterns that do not compile.
func translate(pat string) *regexp.Regexp {
	rs := []rune(pat)
	var b strings.Builder
	b.WriteString(`(?s)\A`)
	for i := 0; i < len(rs); {
		c := rs[i]
		i++
		switch c {
		case '*':
			for i < len(rs) && rs[i] == '*' {
				i++
			}
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i
			if j < len(rs) && rs[j] == '!' {
				j++
			}
			if j < len(rs) && rs[j] == ']' {
				j++
			}
			for j < len(rs) && rs[j] != ']' {
				j++
			}
			if j >= len(rs) {
				b.WriteString(`\[`)
				break
			}
			stuff := strings.ReplaceAll(string(rs[i:j]), `\`, `\\`)
			i = j + 1
			switch {
			case strings.HasPrefix(stuff, "!"):
				stuff = "^" + stuff[1:]
			case strings.HasPrefix(stuff, "^"):
				stuff = `\` + stuff
			}
			b.WriteString("[" + stuff + "]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`\z`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
