package walk

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreFileName is the per-directory ignore file back-then honors during a
// walk. It uses a gitignore-style syntax (a practical subset): one pattern per
// line, `#` comments, blank lines ignored, `!` to negate (re-include), a
// trailing `/` to match directories only, and a leading `/` to anchor a
// pattern to the directory that holds the ignore file. Patterns without a
// slash match by base name at any depth below their file; patterns containing
// a slash match against the path relative to their file. Glob metacharacters
// (`*`, `?`, `[...]`) are supported via path matching.
const IgnoreFileName = ".backthenignore"

// ignorePattern is one compiled line from a .backthenignore file.
type ignorePattern struct {
	// raw is the original pattern text (post-trim), kept for debugging.
	raw string
	// pattern is the pattern with any leading/trailing markers stripped and
	// slashes normalized to forward slashes.
	pattern string
	// negate is true for `!pattern` lines (re-include a previously ignored path).
	negate bool
	// dirOnly is true for `pattern/` lines (match directories only).
	dirOnly bool
	// anchored is true when the pattern was rooted with a leading `/` or
	// otherwise contains a slash, so it matches against the relative path from
	// the ignore file's directory rather than by base name at any depth.
	anchored bool
}

// ignoreScope is the set of patterns from a single .backthenignore file,
// together with the directory that file lives in. Matching is performed
// relative to base.
type ignoreScope struct {
	// base is the absolute directory containing the .backthenignore file.
	base string
	// patterns are the compiled, ordered patterns from the file. Order matters:
	// later patterns override earlier ones (last match wins).
	patterns []ignorePattern
}

// ignoreStack is the ordered list of ignore scopes currently in effect as the
// walk descends. Shallower scopes come first; deeper (more specific) scopes
// come later and take precedence. A copy is taken at each directory so sibling
// branches do not see each other's nested ignore files.
type ignoreStack []ignoreScope

// loadIgnoreScope reads dir/.backthenignore if present and returns a scope for
// it. The boolean is false when no ignore file exists (a normal, common case);
// an unreadable file is treated as absent rather than aborting the walk.
func loadIgnoreScope(dir string) (ignoreScope, bool) {
	f, err := os.Open(filepath.Join(dir, IgnoreFileName))
	if err != nil {
		return ignoreScope{}, false
	}
	defer f.Close()

	pats := parseIgnore(f)
	if len(pats) == 0 {
		return ignoreScope{}, false
	}
	return ignoreScope{base: dir, patterns: pats}, true
}

// parseIgnore compiles the lines of an ignore file into patterns, skipping
// comments and blanks.
func parseIgnore(r io.Reader) []ignorePattern {
	var out []ignorePattern
	sc := bufio.NewScanner(r)
	// Allow generously long lines; ignore files are small but a stray long line
	// should not truncate silently.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if p, ok := compilePattern(sc.Text()); ok {
			out = append(out, p)
		}
	}
	return out
}

// compilePattern parses a single ignore line. The boolean is false for lines
// that carry no pattern (blank or comment).
func compilePattern(line string) (ignorePattern, bool) {
	// Trim surrounding whitespace. Trailing spaces are not significant here;
	// gitignore allows escaping them but that edge is out of scope.
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return ignorePattern{}, false
	}

	p := ignorePattern{raw: s}

	if strings.HasPrefix(s, "!") {
		p.negate = true
		s = s[1:]
	}
	// Normalize separators so patterns written with either slash behave the
	// same regardless of host OS.
	s = filepath.ToSlash(s)

	if strings.HasSuffix(s, "/") {
		p.dirOnly = true
		s = strings.TrimSuffix(s, "/")
	}

	// A leading slash anchors to the ignore file's directory. A pattern that
	// still contains a slash after trimming is also matched against the
	// relative path (gitignore semantics), so mark it anchored.
	if strings.HasPrefix(s, "/") {
		p.anchored = true
		s = strings.TrimPrefix(s, "/")
	} else if strings.Contains(s, "/") {
		p.anchored = true
	}

	s = strings.Trim(s, "/")
	if s == "" {
		// A bare "/" or "!" line carries no usable pattern.
		return ignorePattern{}, false
	}
	p.pattern = s
	return p, true
}

// matches reports whether pattern p matches an entry with the given path
// relative to the pattern's scope base (rel, forward-slashed) and whether that
// entry is a directory. rel must not be empty.
func (p ignorePattern) matches(rel string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if p.anchored {
		// Match against the full relative path. Also treat the pattern as
		// matching any descendant of a matched directory (so "build/" ignores
		// everything under build/).
		if ok, _ := filepath.Match(p.pattern, rel); ok {
			return true
		}
		// Prefix match for directory-style patterns: rel is inside the pattern.
		if strings.HasPrefix(rel, p.pattern+"/") {
			return true
		}
		return false
	}
	// Unanchored: match by base name at any depth.
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	ok, _ := filepath.Match(p.pattern, base)
	return ok
}

// ignored reports whether the entry at absolute path abs (a directory when
// isDir is true) is ignored by the stack. The last matching pattern across all
// scopes wins, so a deeper or later `!negate` can re-include something a
// shallower scope ignored. When no pattern matches, the entry is not ignored.
func (st ignoreStack) ignored(abs string, isDir bool) bool {
	decided := false
	result := false
	for _, sc := range st {
		rel, err := filepath.Rel(sc.base, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" || strings.HasPrefix(rel, "../") {
			// Entry is not under this scope's base; skip.
			continue
		}
		for _, p := range sc.patterns {
			if p.matches(rel, isDir) {
				decided = true
				result = !p.negate
			}
		}
	}
	if !decided {
		return false
	}
	return result
}
