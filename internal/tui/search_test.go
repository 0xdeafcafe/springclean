package tui

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/springclean/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

func sampleSuspects() []domain.Suspect {
	return []domain.Suspect{
		{Path: "/p/langwatch/node_modules", Size: 300, Category: domain.CatDevCache, Reason: "Node.js dependencies"},
		{Path: "/p/langwatch/.venv", Size: 200, Category: domain.CatDevCache, Reason: "Python virtualenv"},
		{Path: "/p/worktrees/billing-3b", Size: 100, Category: domain.CatGitWorktree, Reason: "Linked git worktree"},
		{Path: "/p/Downloads/Big.dmg", Size: 50, Category: domain.CatDownload, Reason: "Download untouched"},
	}
}

func reviewModel(t *testing.T) Model {
	t.Helper()
	m := New(Options{})
	m.width, m.height = 140, 40
	m.phase = PhaseReview
	m.suspects = sampleSuspects()
	m.rebuildView()
	return m
}

func press(m Model, msg tea.KeyMsg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestCycleFilterOnlyVisitsPopulatedTabs is the reported bug: the chips showed
// three tabs, but the key that moved between them walked every category in the
// catalogue, so most presses landed on an empty list.
func TestCycleFilterOnlyVisitsPopulatedTabs(t *testing.T) {
	m := reviewModel(t)

	want := m.visibleCategories()
	if len(want) != 4 { // all + three populated categories
		t.Fatalf("visibleCategories() = %v, want all plus 3", want)
	}

	// Walking a full lap must return to the start, and never show an empty list.
	seen := []domain.Category{m.filter}
	for i := 0; i < len(want); i++ {
		m = press(m, tea.KeyMsg{Type: tea.KeyRight})
		if len(m.view) == 0 {
			t.Fatalf("tab %q is empty; the tabs are stepping through categories with no items", m.filter)
		}
		if i < len(want)-1 {
			seen = append(seen, m.filter)
		}
	}
	if m.filter != "" {
		t.Errorf("after a full lap filter = %q, want back on all", m.filter)
	}
	if len(seen) != len(want) {
		t.Errorf("visited %d tabs, want %d", len(seen), len(want))
	}
}

func TestTabsGoBothWays(t *testing.T) {
	m := reviewModel(t)
	m = press(m, tea.KeyMsg{Type: tea.KeyRight})
	first := m.filter
	m = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.filter != "" {
		t.Errorf("left from the first tab gave %q, want all", m.filter)
	}
	// Left from "all" wraps to the last populated tab, not to an empty one.
	m = press(m, tea.KeyMsg{Type: tea.KeyLeft})
	if len(m.view) == 0 {
		t.Errorf("wrapping backwards landed on empty tab %q", m.filter)
	}
	if m.filter == first {
		t.Error("wrapping backwards should not land on the first tab")
	}
}

func TestSlashOpensSearchAndFilters(t *testing.T) {
	m := reviewModel(t)
	m = press(m, runes("/"))
	if !m.search.active {
		t.Fatal("/ did not open the search line")
	}

	for _, r := range "venv" {
		m = press(m, runes(string(r)))
	}
	if len(m.view) != 1 {
		t.Fatalf("view has %d rows, want 1 matching venv", len(m.view))
	}
	if got := m.suspects[m.view[0]].Path; !strings.Contains(got, ".venv") {
		t.Errorf("matched %q, want the venv entry", got)
	}

	// The query is visible while typing.
	if out := m.View(); !strings.Contains(out, "/venv") {
		t.Error("the search line should show the query")
	}

	// Enter keeps the filter and returns focus to the list.
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.search.active {
		t.Error("enter should close the input")
	}
	if !m.search.on() || len(m.view) != 1 {
		t.Error("enter should keep the query applied")
	}

	// esc from the list clears it.
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.search.on() || len(m.view) != 4 {
		t.Errorf("esc should clear the search, got %d rows", len(m.view))
	}
}

func TestSearchIsRegex(t *testing.T) {
	m := reviewModel(t)
	m = press(m, runes("/"))
	for _, r := range `node|venv` {
		m = press(m, runes(string(r)))
	}
	if len(m.view) != 2 {
		t.Errorf("alternation matched %d rows, want 2", len(m.view))
	}

	m = reviewModel(t)
	m = press(m, runes("/"))
	for _, r := range `billing-\d` {
		m = press(m, runes(string(r)))
	}
	if len(m.view) != 1 {
		t.Errorf("`billing-\\d` matched %d rows, want 1", len(m.view))
	}
}

func TestSearchSmartCase(t *testing.T) {
	var s search

	// All lower case is case-insensitive.
	s.query = "big.dmg"
	s.compile()
	if !s.matches(domain.Suspect{Path: "/p/Downloads/Big.dmg"}) {
		t.Error("a lower-case query should match regardless of case")
	}

	// An upper-case character makes it case-sensitive, as in vim.
	s.query = "Big"
	s.compile()
	if !s.matches(domain.Suspect{Path: "/p/Downloads/Big.dmg"}) {
		t.Error("should match the capitalised path")
	}
	if s.matches(domain.Suspect{Path: "/p/downloads/big.dmg"}) {
		t.Error("an upper-case query should not match lower-case text")
	}
}

func TestSearchMatchesReasonAndProject(t *testing.T) {
	var s search
	s.query = "virtualenv"
	s.compile()
	if !s.matches(domain.Suspect{Path: "/p/x", Reason: "Python virtualenv"}) {
		t.Error("search should look at the reason as well as the path")
	}

	s.query = "billing-3b"
	s.compile()
	if !s.matches(domain.Suspect{Path: "/p/x/node_modules", Project: "/p/worktrees/billing-3b"}) {
		t.Error("search should find a cache by the project owning it")
	}
}

func TestInvalidRegexIsReported(t *testing.T) {
	m := reviewModel(t)
	m = press(m, runes("/"))
	m = press(m, runes("["))
	if !m.search.invalid {
		t.Fatal("an unterminated class should be marked invalid")
	}
	if len(m.view) != 0 {
		t.Errorf("an invalid pattern matched %d rows, want none", len(m.view))
	}
	out := m.View()
	if !strings.Contains(out, "invalid") {
		t.Errorf("the screen should say the pattern is invalid\n%s", out)
	}

	// Completing the class makes it valid again.
	m = press(m, runes("v"))
	m = press(m, runes("]"))
	if m.search.invalid {
		t.Error("`[v]` should compile")
	}
}

func TestSearchCancelRestoresPreviousQuery(t *testing.T) {
	m := reviewModel(t)
	m = press(m, runes("/"))
	for _, r := range "venv" {
		m = press(m, runes(string(r)))
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Start a second search, type something else, then back out of it.
	m = press(m, runes("/"))
	for _, r := range "dmg" {
		m = press(m, runes(string(r)))
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.search.query != "venv" {
		t.Errorf("query = %q, want the previous search restored", m.search.query)
	}
	if len(m.view) != 1 {
		t.Errorf("view has %d rows, want the restored search applied", len(m.view))
	}
}

// TestTypingInSearchDoesNotTriggerListBindings guards the overlap between the
// search input and the single-letter list keys: typing "s" in a search must
// not re-sort, and "q" must not quit.
func TestTypingInSearchDoesNotTriggerListBindings(t *testing.T) {
	m := reviewModel(t)
	sortBefore := m.sort
	m = press(m, runes("/"))
	for _, r := range "sq" {
		m = press(m, runes(string(r)))
	}
	if m.sort != sortBefore {
		t.Error("typing s in a search re-sorted the list")
	}
	if m.search.query != "sq" {
		t.Errorf("query = %q, want sq", m.search.query)
	}
}

func TestSearchCombinesWithTabFilter(t *testing.T) {
	m := reviewModel(t)
	// Narrow to dev cache, then search within it.
	m = press(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.filter != domain.CatDevCache {
		t.Fatalf("filter = %q, want dev cache", m.filter)
	}
	m = press(m, runes("/"))
	for _, r := range "venv" {
		m = press(m, runes(string(r)))
	}
	if len(m.view) != 1 {
		t.Errorf("view has %d rows, want the venv entry inside dev cache", len(m.view))
	}

	// A term that only exists outside the tab finds nothing, and says so.
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "billing" {
		m = press(m, runes(string(r)))
	}
	if len(m.view) != 0 {
		t.Errorf("view has %d rows, want none", len(m.view))
	}
	if !strings.Contains(m.View(), "Dev cache") {
		t.Error("the empty message should name the tab that's narrowing the list")
	}
}
