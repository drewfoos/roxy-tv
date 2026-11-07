package library

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// .ignore / .roxyignore files with patterns, read the way Jellyfin reads them (gitignore
// rules): each folder's file applies to everything below it, including one at the library
// root. An empty file (or one with only comments) ignores its whole folder, as before.
//
//	# comment          blank lines and comments are skipped
//	*.sample.mkv       no slash: a name, at any depth below
//	/Bonus             leading slash: only right in this folder
//	Extras/            trailing slash: folders only
//	Season 0*/**       ** spans any number of folders
//	!keep.mkv          ! brings back what an earlier line ignored (the last match wins)

type ignoreRule struct {
	re      *regexp.Regexp
	dirOnly bool
	negate  bool
}

// ignoreRules is one folder's .ignore: all=true for an empty one (everything below).
type ignoreRules struct {
	all   bool
	rules []ignoreRule
}

// readIgnore reads dir's .ignore or .roxyignore; nil when it has none.
func readIgnore(dir string) *ignoreRules {
	for _, m := range IgnoreMarkers {
		b, err := os.ReadFile(filepath.Join(dir, m))
		if err != nil {
			continue
		}
		r := &ignoreRules{}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimRight(line, "\r")
			if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if rule, ok := compileIgnore(strings.TrimSpace(line)); ok {
				r.rules = append(r.rules, rule)
			}
		}
		r.all = len(r.rules) == 0
		return r
	}
	return nil
}

func compileIgnore(p string) (ignoreRule, bool) {
	var r ignoreRule
	if strings.HasPrefix(p, "!") {
		r.negate, p = true, p[1:]
	}
	if strings.HasSuffix(p, "/") {
		r.dirOnly, p = true, strings.TrimRight(p, "/")
	}
	anchored := strings.HasPrefix(p, "/") || strings.Contains(p, "/")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return r, false
	}
	var b strings.Builder
	b.WriteString("(?i)^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				b.WriteString(".*")
				i++
				if i+1 < len(p) && p[i+1] == '/' {
					i++
					b.WriteString("/?")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("(?:/.*)?$") // a folder that matches takes everything in it along
	re, err := regexp.Compile(b.String())
	if err != nil {
		return r, false
	}
	r.re = re
	return r, true
}

// match says whether rel (relative to the rules' folder, slash separated) is ignored:
// decided by the last rule that matches it, or by an empty file (everything).
func (r *ignoreRules) match(rel string, isDir bool) (ignored, decided bool) {
	if r.all {
		return true, true
	}
	for _, rule := range r.rules {
		if rule.dirOnly && !isDir && !strings.Contains(rel, "/") {
			continue
		}
		if rule.re.MatchString(rel) {
			ignored, decided = !rule.negate, true
		}
	}
	return ignored, decided
}

// ignoreCache holds each folder's .ignore for one scan.
type ignoreCache map[string]*ignoreRules

// ignored reports whether path (a file or folder below root) is ignored by an .ignore in
// root or any folder between them; the nearest folder's file has the last word.
func (c ignoreCache) ignored(root, path string, isDir bool) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	result := false
	for i := 0; i < len(parts); i++ {
		dir := filepath.Join(root, filepath.FromSlash(strings.Join(parts[:i], "/")))
		rules, ok := c[dir]
		if !ok {
			rules = readIgnore(dir)
			c[dir] = rules
		}
		if rules == nil {
			continue
		}
		if ign, decided := rules.match(strings.Join(parts[i:], "/"), isDir); decided {
			result = ign
		}
	}
	return result
}
