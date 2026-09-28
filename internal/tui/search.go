package tui

import (
	"regexp"
	"strings"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

// search is a vim-style `/` filter over the suspect list.
//
// The query is a regular expression, matched case-insensitively unless it
// contains an upper-case letter, the way smartcase works in vim: `node` finds
// everything, `Node` only the capitalised ones.
type search struct {
	// query is what the user has typed. Kept separate from the compiled
	// expression so a half-finished pattern can stay on screen.
	query string
	// active means the input line has focus and keystrokes are text.
	active bool
	// re is the compiled query, nil while the pattern is invalid or empty.
	re *regexp.Regexp
	// invalid marks a query that doesn't compile, so the UI can say so
	// instead of silently matching nothing.
	invalid bool
	// prev restores the query when a search is cancelled with esc.
	prev string
}

// on reports whether a committed query is narrowing the list.
func (s search) on() bool { return s.query != "" }

// compile rebuilds the matcher after the query changes.
func (s *search) compile() {
	s.re, s.invalid = nil, false
	if s.query == "" {
		return
	}
	pattern := s.query
	if !hasUpper(pattern) {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		s.invalid = true
		return
	}
	s.re = re
}

// matches reports whether a suspect should survive the search.
//
// The path is what people search for, but reason and project are matched too,
// so "worktree" or a branch name finds things whose path doesn't say either.
func (s search) matches(sus domain.Suspect) bool {
	if s.re == nil {
		// An invalid pattern matches nothing, so the user can see they've
		// mistyped rather than wondering why the list looks normal.
		return !s.invalid
	}
	return s.re.MatchString(sus.Path) ||
		s.re.MatchString(sus.Reason) ||
		s.re.MatchString(sus.Project)
}

func hasUpper(s string) bool {
	return strings.ToLower(s) != s
}

func (s *search) typeRune(r string) {
	s.query += r
	s.compile()
}

func (s *search) backspace() {
	if s.query == "" {
		return
	}
	r := []rune(s.query)
	s.query = string(r[:len(r)-1])
	s.compile()
}

func (s *search) clear() {
	s.query = ""
	s.active = false
	s.compile()
}

// begin opens the input, remembering the current query so esc can put it back.
func (s *search) begin() {
	s.prev = s.query
	s.active = true
}

// cancel abandons an in-progress search and restores what was there before.
func (s *search) cancel() {
	s.query = s.prev
	s.active = false
	s.compile()
}

// accept keeps the typed query and returns focus to the list.
func (s *search) accept() {
	s.active = false
}
