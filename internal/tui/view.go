package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/springclean/internal/anim"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
)

const (
	headerHeight  = 5
	footerHeight  = 2
	animH         = 7
	minPanelWidth = 30
)

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	switch m.phase {
	case PhaseSplash:
		return m.viewSplash()
	case PhaseOptions:
		return m.viewOptions()
	case PhaseFDA:
		return m.viewFDA()
	case PhaseScanning:
		return m.viewDashboard(true)
	case PhaseReview:
		return m.viewDashboard(false)
	case PhaseConfirm:
		return m.viewConfirm()
	case PhaseApplying:
		return m.viewApplying()
	case PhasePermissionDenied:
		return m.viewPermissionDenied()
	case PhaseCelebration:
		return m.viewCelebration()
	}
	return ""
}

func (m Model) viewOptions() string {
	f := m.form

	panelWidth := min(84, m.width-4)
	// Border, padding and the two-column label gutter come off the top before
	// anything is allowed to occupy a line.
	const labelWidth = 18
	inner := panelWidth - 8
	valueWidth := inner - labelWidth - 2
	if valueWidth < 12 {
		valueWidth = 12
	}

	var body []string
	row := func(field formField, label, value, note string) {
		if !f.visible(field) {
			return
		}
		cursor := "  "
		labelStyle := theme.Dim
		valueStyle := lipgloss.NewStyle().Foreground(theme.Cream)
		if f.cursor == field {
			cursor = lipgloss.NewStyle().Foreground(theme.Sun).Render("▸ ")
			labelStyle = lipgloss.NewStyle().Foreground(theme.Sun)
			valueStyle = lipgloss.NewStyle().Foreground(theme.Sun).Bold(true)
		}
		body = append(body, cursor+labelStyle.Render(padRight(label, labelWidth))+
			valueStyle.Render(truncateMiddle(value, valueWidth)))
		// The explanation belongs to whichever row is selected. Showing every
		// one at once overflowed the panel and wrapped into the border.
		if f.cursor == field && note != "" {
			body = append(body, theme.Dim.Render(strings.Repeat(" ", labelWidth+2)+
				truncateMiddle(note, valueWidth)))
		}
	}

	// The tail of a path identifies it; truncatePath collapses the middle so
	// aggressively that two sibling directories look identical here.
	pathValue := truncateHead(shortenHome(f.path), valueWidth)
	if f.cursor == fieldPath && f.editing {
		pathValue = truncateHead(shortenHome(f.path), valueWidth-1) + "▏"
	}
	pathNote := "[e] to edit, [enter] to accept"
	if f.editing {
		pathNote = "typing… [enter] done · [ctrl+u] clear"
	}

	row(fieldScope, "scope", f.scope.Label(), f.scope.Blurb())
	row(fieldPath, "path", pathValue, pathNote)
	row(fieldWorktreeAge, "stale worktrees", ageLabel(f.worktreeAge), "untouched for at least this long")
	row(fieldIgnored, "gitignored cruft", onOff(f.ignored), "ask git for big, stale ignored files")
	row(fieldIgnoredAge, "cruft age", ageLabel(f.ignoredAge), "how long ignored files must sit unused")
	row(fieldCacheAge, "hide live caches", cacheAgeLabel(f.cacheAge),
		"skip caches whose project is still being worked on")
	row(fieldRealSize, "real disk use", onOff(f.realSize),
		"count copies that share storage once, not once each")

	heading := theme.PanelTitle.Render("scan options")
	intro := theme.Subtitle.Render("worktrees and gitignored cruft need a walking scope")

	hint := theme.KeyHint.Render("[↑↓] field · [←→] change · [space] scan · [esc] back")

	parts := []string{heading, "", intro, ""}
	parts = append(parts, body...)
	if m.err != "" {
		parts = append(parts, "", theme.Danger.Render(truncateMiddle("⚠  "+m.err, inner)))
	}
	parts = append(parts, "", hint)

	panel := theme.Panel.BorderForeground(theme.Lavender).
		Width(panelWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

// truncateMiddle shortens plain text to fit, keeping both ends readable.
func truncateMiddle(s string, max int) string {
	r := []rune(s)
	if max < 6 {
		max = 6
	}
	if len(r) <= max {
		return s
	}
	keep := max - 1
	head := keep / 2
	tail := keep - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// shortenHome swaps the home directory prefix for `~`.
func shortenHome(p string) string {
	home, err := homeDir()
	if err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// truncateHead keeps the tail of a string, which is what matters while the
// user is typing a path.
func truncateHead(s string, max int) string {
	r := []rune(s)
	if max < 4 {
		max = 4
	}
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-(max-1):])
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// cacheAgeLabel reads as a filter rather than a threshold, because 0 here
// means "show everything" rather than "no waiting period".
func cacheAgeLabel(days int) string {
	if days <= 0 {
		return "off (show every cache)"
	}
	if days == 1 {
		return "project idle 1+ day"
	}
	return "project idle " + itoa(days) + "+ days"
}

func (m Model) viewPermissionDenied() string {
	steps := []string{
		"1. Open  System Settings",
		"2. Privacy & Security › Automation",
		"3. Find your terminal app (Terminal, iTerm, Ghostty, …)",
		"4. Toggle ✓ next to  Finder",
		"5. Come back and press [r] to retry",
	}
	guide := lipgloss.NewStyle().Foreground(theme.Cream).Render(strings.Join(steps, "\n"))

	count := m.markedCount()
	bytes := humanize.Bytes(uint64(m.markedBytes()))

	heading := theme.Danger.Render("⚠  macOS blocked Apple-event Automation")
	intro := theme.Subtitle.Render(fmt.Sprintf(
		"springclean asked Finder to trash %d items (%s) but macOS denied the\nrequest (osascript error -1743). Finder integration preserves \"Put Back\".",
		count, bytes))

	manualNote := theme.Dim.Render("Fallback: move directly to ~/.Trash without Finder.\n" +
		"Loses Finder's \"Put Back\" metadata, but unblocks you right now.")

	hint := theme.KeyHint.Render("[r] retry with Finder · [m] move manually to ~/.Trash · [esc] back")

	body := lipgloss.JoinVertical(lipgloss.Left,
		heading, "", intro, "", guide, "", manualNote, "", hint,
	)
	panel := theme.Panel.BorderForeground(theme.Warning).Width(min(80, m.width-4)).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) contentHeight() int {
	h := m.height - headerHeight - footerHeight
	if h < 4 {
		h = 4
	}
	return h
}

func (m Model) animPanelHeight() int { return animH }

func (m Model) listHeight() int {
	// dashboard middle = content - anim panel
	return m.contentHeight() - animH - 2 // 2 for inter-panel borders/padding
}

// visibleListRows returns how many suspect rows actually fit inside the list
// panel. The panel has a rounded border (2 lines) and an internal title row,
// so the body area is listHeight() - 3.
func (m Model) visibleListRows() int {
	h := m.listHeight() - 3
	if h < 1 {
		return 1
	}
	return h
}

func (m Model) viewSplash() string {
	body := anim.RenderSplash(m.splashTick, m.width)
	footer := ""
	if m.fdaDone && !m.fda.Granted {
		footer = theme.Danger.Render("⚠  Full Disk Access not granted — some paths will be skipped.")
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, body, "", footer),
	)
}

func (m Model) viewFDA() string {
	steps := []string{
		"1. Open  System Settings",
		"2. Go to  Privacy & Security › Full Disk Access",
		"3. Click  +  and add your terminal (Terminal, iTerm, Ghostty, …)",
		"4. Toggle it ON, then restart your terminal",
		"5. Run springclean again",
	}
	bullets := strings.Join(steps, "\n")
	blocked := strings.Join(m.fda.BlockedPaths, "\n  ")

	heading := theme.Title.Render("✿  Full Disk Access needed")
	intro := theme.Subtitle.Render("Some of the juiciest gunk (Mail caches, Safari history, etc.) lives in protected paths.\nspringclean can run without it, but the report will be incomplete.")
	guide := lipgloss.NewStyle().Foreground(theme.Cream).Render(bullets)
	blockedBox := theme.Dim.Render("Blocked probes:\n  " + blocked)
	hint := theme.KeyHint.Render("press [c] to continue without FDA · [q] to quit and grant first")

	body := lipgloss.JoinVertical(lipgloss.Left,
		heading, "", intro, "", guide, "", blockedBox, "", hint,
	)
	panel := theme.Panel.BorderForeground(theme.Sun).Width(min(80, m.width-4)).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) viewDashboard(scanning bool) string {
	header := m.renderHeader(scanning)
	listPanel := m.renderListPanel()
	detailPanel := m.renderDetailPanel()
	row := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, detailPanel)
	animPanel := m.renderAnimPanel()
	footer := m.renderFooter(scanning)
	return lipgloss.JoinVertical(lipgloss.Left, header, row, animPanel, footer)
}

func (m Model) renderHeader(scanning bool) string {
	title := theme.Title.Render("✿ springclean")
	badge := m.renderPhaseBadge()
	modeStr := theme.Subtitle.Render(fmt.Sprintf("%s scan", domain.ScanMode(m.opts.ScanConfig.Mode).String()))

	var elapsed string
	if scanning {
		elapsed = theme.Dim.Render("· " + formatDuration(m.progress.Elapsed))
	} else if !m.result.FinishedAt.IsZero() {
		elapsed = theme.Dim.Render("· finished in " + formatDuration(m.result.Stats.Duration))
	}

	line1 := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", badge, "  ", modeStr, "  ", elapsed)
	line2 := m.renderPathLine(scanning)
	line3 := m.renderStatsLine(scanning)
	line4 := m.renderVanityLine(scanning)
	line5 := m.renderCategoryChips()

	body := lipgloss.JoinVertical(lipgloss.Left, line1, line2, line3, line4, line5)
	return lipgloss.NewStyle().Padding(0, 1).Width(m.width).Render(body)
}

func (m Model) renderPhaseBadge() string {
	switch m.phase {
	case PhaseScanning:
		return badge("SCANNING", theme.Sun, scanningSpinner(m.animTick), theme.Ink)
	case PhaseReview:
		return badge("REVIEW", theme.Leaf, "✓", theme.Ink)
	case PhaseConfirm:
		return badge("CONFIRM?", theme.PetalDeep, "?", theme.Ink)
	case PhaseApplying:
		return badge("APPLYING", theme.Petal, brailleSpinner(m.animTick), theme.Ink)
	case PhaseCelebration:
		return badge("DONE", theme.Success, "✿", theme.Ink)
	}
	return badge("READY", theme.Lavender, "•", theme.Ink)
}

func badge(label string, bg lipgloss.Color, prefix string, fg lipgloss.Color) string {
	st := lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true).Padding(0, 1)
	return st.Render(prefix + " " + label)
}

func scanningSpinner(tick int) string {
	frames := []string{"▰▱▱▱▱▱", "▰▰▱▱▱▱", "▱▰▰▱▱▱", "▱▱▰▰▱▱", "▱▱▱▰▰▱", "▱▱▱▱▰▰", "▱▱▱▱▱▰", "▰▱▱▱▱▰"}
	return frames[tick%len(frames)]
}

func brailleSpinner(tick int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[tick%len(frames)]
}

func (m Model) renderPathLine(scanning bool) string {
	if scanning {
		path := m.progress.CurrentPath
		if path == "" {
			return theme.Dim.Render("⮕  preparing…")
		}
		home, _ := os.UserHomeDir()
		if strings.HasPrefix(path, home) {
			path = "~" + path[len(home):]
		}
		max := m.width - 6
		if max < 20 {
			max = 20
		}
		if len(path) > max {
			path = "…" + path[len(path)-max+1:]
		}
		return theme.Dim.Render("⮕  ") + lipgloss.NewStyle().Foreground(theme.Sky).Render(path)
	}
	if m.phase == PhaseReview {
		return theme.Subtitle.Render(fmt.Sprintf("✓ scan complete · %d suspects · %s reclaimable",
			len(m.suspects), humanize.Bytes(uint64(totalBytes(m.suspects)))))
	}
	return ""
}

func (m Model) renderStatsLine(scanning bool) string {
	if scanning {
		return strings.Join([]string{
			stat("items", formatCount(m.progress.ItemsSeen), theme.Cream),
			stat("suspects", fmt.Sprintf("%d (%s)", m.progress.SuspectCount, humanize.Bytes(uint64(m.progress.SuspectBytes))), theme.Sun),
			stat("workers", fmt.Sprintf("%d active · %d queued", m.progress.ActiveWorkers, m.progress.Queued), theme.Leaf),
			stat("skipped", fmt.Sprintf("%d", m.progress.Skipped), theme.Mist),
		}, theme.Dim.Render("  ·  "))
	}
	if m.phase == PhaseReview && !m.result.FinishedAt.IsZero() {
		return strings.Join([]string{
			stat("marked", fmt.Sprintf("%d · %s", m.markedCount(), humanize.Bytes(uint64(m.markedBytes()))), theme.Sun),
			stat("walked", formatCount(m.result.Stats.TotalItems), theme.Cream),
			stat("sort", m.sort.Label(), theme.Sky),
			stat("filter", filterLabel(m.filter), theme.Lavender),
		}, theme.Dim.Render("  ·  "))
	}
	return ""
}

func (m Model) renderVanityLine(scanning bool) string {
	if scanning {
		return strings.Join([]string{
			theme.Highlight.Render("⚡ ") + stat("rate", fmt.Sprintf("%s items/s", formatCount(int64(m.progress.ItemsPerSec))), theme.Sun),
			stat("throughput", humanize.Bytes(uint64(m.progress.BytesPerSec))+"/s", theme.Leaf),
			stat("peak", fmt.Sprintf("%d workers", m.progress.PeakWorkers), theme.Lavender),
			stat("spawned", fmt.Sprintf("%s goroutines", formatCount(m.progress.GoroutinesSpawn)), theme.Sky),
		}, theme.Dim.Render("  ·  "))
	}
	if m.phase == PhaseReview && !m.result.FinishedAt.IsZero() {
		secs := m.result.Stats.Duration.Seconds()
		var ips, bps float64
		if secs > 0 {
			ips = float64(m.result.Stats.TotalItems) / secs
			bps = float64(m.result.Stats.TotalBytes) / secs
		}
		return strings.Join([]string{
			theme.Highlight.Render("⚡ ") + stat("avg rate", fmt.Sprintf("%s items/s", formatCount(int64(ips))), theme.Sun),
			stat("avg throughput", humanize.Bytes(uint64(bps))+"/s", theme.Leaf),
			stat("total bytes touched", humanize.Bytes(uint64(m.result.Stats.TotalBytes)), theme.Lavender),
		}, theme.Dim.Render("  ·  "))
	}
	return ""
}

func stat(label, value string, valueColor lipgloss.Color) string {
	return theme.Dim.Render(label+" ") + lipgloss.NewStyle().Foreground(valueColor).Bold(true).Render(value)
}

func filterLabel(c domain.Category) string {
	if c == "" {
		return "all"
	}
	return c.Glyph() + " " + c.Label()
}

func formatCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm %02ds", mins, secs)
}

func (m Model) renderCategoryChips() string {
	counts := map[domain.Category]int{}
	bytesBy := map[domain.Category]int64{}
	for _, s := range m.suspects {
		counts[s.Category]++
		bytesBy[s.Category] += s.Size
		counts[""]++
		bytesBy[""] += s.Size
	}
	var chips []string
	cats := m.visibleCategories()
	for _, c := range cats {
		label := "All"
		glyph := "✦"
		if c != "" {
			label = c.Label()
			glyph = c.Glyph()
		}
		chip := fmt.Sprintf("%s %s %d·%s", glyph, label, counts[c], humanize.Bytes(uint64(bytesBy[c])))
		st := theme.Dim
		if c == m.filter {
			st = lipgloss.NewStyle().Foreground(theme.Ink).Background(theme.Sun).Bold(true).Padding(0, 1)
		}
		chips = append(chips, st.Render(chip))
	}
	return strings.Join(chips, theme.Dim.Render(" "))
}

// renderSearchLine draws the `/` prompt, or a reminder of the query that's
// narrowing the list. Returns "" when no search is in play.
func (m Model) renderSearchLine(width int) string {
	if !m.search.active && !m.search.on() {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(theme.Sun)
	if m.search.invalid {
		style = theme.Danger
	}
	q := truncateHead(m.search.query, max(10, width-14))
	line := style.Render("/" + q)
	if m.search.active {
		line += style.Render("▏")
	}
	switch {
	case m.search.invalid:
		line += theme.Danger.Render("  invalid regex")
	case !m.search.active:
		line += theme.Dim.Render("  [esc] clear")
	}
	return "  " + line
}

// emptyListMessage explains why nothing is showing, which is otherwise a
// puzzle when a filter or a search is responsible.
func (m Model) emptyListMessage() string {
	switch {
	case m.search.invalid:
		return "that pattern doesn't compile"
	case m.search.on() && m.filter != "":
		return "nothing in " + m.filter.Label() + " matches /" + m.search.query
	case m.search.on():
		return "nothing matches /" + m.search.query
	case len(m.suspects) > 0 && m.filter != "":
		return "nothing in " + m.filter.Label()
	}
	return "no suspects yet… 🌱"
}

func (m Model) renderListPanel() string {
	width := m.listWidth()
	height := m.listHeight()

	title := fmt.Sprintf("Suspects  (%d / %d shown)", len(m.view), len(m.suspects))
	header := theme.PanelTitle.Render(title)
	if line := m.renderSearchLine(width - 4); line != "" {
		header = lipgloss.JoinVertical(lipgloss.Left, header, line)
	}

	if len(m.view) == 0 {
		body := theme.Dim.Render("  " + m.emptyListMessage())
		inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
		return theme.Panel.Width(width).Height(height).Render(inner)
	}

	var rows []string
	visibleCount := m.visibleListRows()
	end := m.listTop + visibleCount
	if end > len(m.view) {
		end = len(m.view)
	}
	for i := m.listTop; i < end; i++ {
		idx := m.view[i]
		s := m.suspects[idx]
		rows = append(rows, m.renderListRow(s, i == m.selected, width-4))
	}
	body := strings.Join(rows, "\n")
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return theme.Panel.Width(width).Height(height).Render(inner)
}

func (m Model) renderListRow(s domain.Suspect, selected bool, innerWidth int) string {
	mark := theme.Unmarked.Render(" ")
	if s.Marked {
		mark = theme.Marked.Render("●")
	}
	glyph := lipgloss.NewStyle().Foreground(categoryColor(s.Category)).Render(s.Category.Glyph())
	size := lipgloss.NewStyle().Foreground(theme.Sun).Bold(true).Render(humanize.Bytes(uint64(s.Size)))
	// Unsaved work is the one thing worth seeing without opening the detail
	// panel, so it gets a column of its own in the row.
	risk := " "
	if s.Warning != "" {
		risk = theme.Danger.Render("⚠")
	}
	pathStr := truncatePath(s.Path, innerWidth-24)
	row := fmt.Sprintf("%s %s %s  %s  %s", mark, risk, glyph, padRight(size, 9), pathStr)
	if selected {
		return theme.ListSelected.Width(innerWidth).Render(row)
	}
	return theme.ListItem.Render(row)
}

func (m Model) renderDetailPanel() string {
	width := m.width - m.listWidth()
	height := m.listHeight()
	header := theme.PanelTitle.Render("Details")

	if len(m.view) == 0 {
		body := theme.Dim.Render("  select an item to see details")
		inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
		return theme.Panel.BorderForeground(theme.Lavender).Width(width).Height(height).Render(inner)
	}
	s := m.suspects[m.view[m.selected]]

	field := func(label, value string, valueStyle lipgloss.Style) string {
		return theme.Dim.Render(padRight(label, 12)) + valueStyle.Render(value)
	}

	pathStr := s.Path
	if home, err := homeDir(); err == nil {
		if strings.HasPrefix(pathStr, home) {
			pathStr = "~" + pathStr[len(home):]
		}
	}
	wrapped := wrapPath(pathStr, width-6)

	regen := "no"
	regenStyle := theme.Danger
	if s.Regenerable {
		regen = "yes (cache · regenerated on demand)"
		regenStyle = theme.Good
	}

	mark := "○ unmarked"
	markStyle := theme.Unmarked
	if s.Marked {
		mark = "● marked for trash"
		markStyle = theme.Marked
	}

	lastUsed := "—"
	if !s.LastUsed.IsZero() {
		// humanize.Time already renders the "ago".
		lastUsed = fmt.Sprintf("%s (%s)",
			s.LastUsed.Format("2006-01-02"),
			humanize.Time(s.LastUsed),
		)
	}

	rows := []string{
		"",
		lipgloss.NewStyle().Foreground(theme.Cream).Bold(true).Render(wrapped),
		"",
		field("Category", s.Category.Glyph()+" "+s.Category.Label(), lipgloss.NewStyle().Foreground(categoryColor(s.Category))),
		field("Frees", humanize.Bytes(uint64(s.Size)), theme.Highlight),
	}
	// A tree a package manager installed by cloning costs almost nothing after
	// the first copy, and the gap between what it occupies and what deleting it
	// gives back is the whole point.
	if s.Shared > 0 {
		rows = append(rows,
			field("Occupies", humanize.Bytes(uint64(s.Size+s.Shared)), theme.ListItem),
			field("Shared", humanize.Bytes(uint64(s.Shared))+" with other copies", theme.Dim),
		)
	}
	rows = append(rows, field("Last used", lastUsed, theme.ListItem))
	// A cache's own mtime says when it was installed. Whether the project
	// around it is still live is the thing that decides if it should go.
	if s.Project != "" && !s.ProjectLastUsed.IsZero() {
		projStyle := theme.ListItem
		if time.Since(s.ProjectLastUsed) < 7*24*time.Hour {
			projStyle = lipgloss.NewStyle().Foreground(theme.Warning)
		}
		rows = append(rows,
			field("Project", truncateHead(shortenHome(s.Project), 34), theme.ListItem),
			field("Worked on", humanize.Time(s.ProjectLastUsed), projStyle),
		)
	}
	rows = append(rows,
		field("Regenerable", regen, regenStyle),
		field("Marked", mark, markStyle),
		"",
		theme.Dim.Render(s.Reason),
	)
	if s.Warning != "" {
		rows = append(rows, theme.Danger.Render("⚠  "+s.Warning))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return theme.Panel.BorderForeground(theme.Lavender).Width(width).Height(height).Render(inner)
}

func (m Model) renderAnimPanel() string {
	width := m.width
	if width < 10 {
		width = 10
	}
	height := animH
	innerW := width - 4

	if m.garden != nil {
		switch m.phase {
		case PhaseScanning:
			m.garden.SetMode(anim.GardenActive)
		case PhaseApplying:
			m.garden.SetMode(anim.GardenSweeping)
		case PhaseReview:
			m.garden.SetMode(anim.GardenSettled)
		default:
			m.garden.SetMode(anim.GardenCalm)
		}
	}

	gardenView := ""
	if m.garden != nil {
		gardenView = m.garden.Render()
	}

	banner := m.phaseBanner()
	bannerStyled := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, banner)

	body := lipgloss.JoinVertical(lipgloss.Left, gardenView, "", bannerStyled)
	return theme.Panel.BorderForeground(theme.Leaf).Width(width).Height(height).Render(body)
}

func (m Model) phaseBanner() string {
	switch m.phase {
	case PhaseScanning:
		brush := scanningSpinner(m.animTick)
		text := fmt.Sprintf("✨ scrubbing the gunk · %s items at %s items/s",
			formatCount(m.progress.ItemsSeen),
			formatCount(int64(m.progress.ItemsPerSec)),
		)
		return theme.Highlight.Render(brush+"  ") + theme.Subtitle.Render(text) + theme.Highlight.Render("  "+brush)
	case PhaseApplying:
		s := brailleSpinner(m.animTick)
		return theme.Highlight.Render(s+" ") + theme.Subtitle.Render("sweeping marked items to the Trash") + theme.Highlight.Render(" "+s)
	case PhaseReview:
		marked := m.markedBytes()
		total := totalBytes(m.suspects)
		if marked == 0 {
			return theme.Subtitle.Render(fmt.Sprintf("✿ %s of gunk awaits · press space to mark · D to trash",
				humanize.Bytes(uint64(total))))
		}
		return theme.Highlight.Render(fmt.Sprintf("✿ marked %s of %s · press D to trash · R to save report",
			humanize.Bytes(uint64(marked)), humanize.Bytes(uint64(total))))
	case PhaseConfirm:
		if m.applyMode == applyDelete {
			return theme.Danger.Render("⚠ about to delete items for good — confirm with y / cancel with n")
		}
		return theme.Danger.Render("⚠ about to move items to the Trash — confirm with y / cancel with n")
	case PhaseCelebration:
		return theme.Good.Render("✿ spring is sprung!")
	}
	return ""
}

func (m Model) renderFooter(scanning bool) string {
	// While typing a search, the only keys that do anything are the editing
	// ones, so showing the list bindings would be a lie.
	if m.search.active {
		return theme.KeyHint.Render("type a regex") +
			theme.Dim.Render(" · ") + theme.KeyHint.Render("enter") + theme.Dim.Render(" keep") +
			theme.Dim.Render(" · ") + theme.KeyHint.Render("esc") + theme.Dim.Render(" cancel") +
			theme.Dim.Render(" · ") + theme.KeyHint.Render("ctrl+u") + theme.Dim.Render(" clear")
	}

	var hints []string
	if scanning {
		hints = []string{
			theme.KeyHint.Render("space") + theme.Dim.Render(" mark"),
			theme.KeyHint.Render("←/→") + theme.Dim.Render(" tabs"),
			theme.KeyHint.Render("/") + theme.Dim.Render(" search"),
			theme.KeyHint.Render("s") + theme.Dim.Render(" sort"),
			theme.KeyHint.Render("esc") + theme.Dim.Render(" cancel scan"),
			theme.KeyHint.Render("q") + theme.Dim.Render(" quit"),
		}
	} else {
		hints = []string{
			theme.KeyHint.Render("space") + theme.Dim.Render(" mark"),
			theme.KeyHint.Render("S") + theme.Dim.Render(" mark safe"),
			theme.KeyHint.Render("←/→") + theme.Dim.Render(" tabs"),
			theme.KeyHint.Render("/") + theme.Dim.Render(" search"),
			theme.KeyHint.Render("s") + theme.Dim.Render(" sort"),
			theme.KeyHint.Render("o") + theme.Dim.Render(" options"),
			theme.KeyHint.Render("R") + theme.Dim.Render(" save report"),
			theme.KeyHint.Render("r") + theme.Dim.Render(" rescan"),
			theme.KeyHint.Render("D") + theme.Dim.Render(" trash marked"),
			theme.KeyHint.Render("X") + theme.Dim.Render(" delete marked"),
			theme.KeyHint.Render("q") + theme.Dim.Render(" quit"),
		}
	}
	line := strings.Join(hints, theme.Dim.Render(" · "))

	flash := ""
	if m.flash != "" && time.Since(m.flashAt) < 4*time.Second {
		flash = theme.Highlight.Render(" " + m.flash)
	}
	return lipgloss.NewStyle().Padding(0, 1).Width(m.width).Render(line + flash)
}

func (m Model) viewConfirm() string {
	marked := m.markedBytes()
	count := m.markedCount()

	// Build summary by category
	byCat := map[domain.Category]struct {
		count int
		bytes int64
	}{}
	for _, s := range m.suspects {
		if !s.Marked {
			continue
		}
		v := byCat[s.Category]
		v.count++
		v.bytes += s.Size
		byCat[s.Category] = v
	}
	var lines []string
	for _, c := range domain.AllCategories() {
		v, ok := byCat[c]
		if !ok || v.count == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s %-14s %4d items · %s",
			c.Glyph(), c.Label(), v.count, humanize.Bytes(uint64(v.bytes))))
	}

	things := fmt.Sprintf("%d items", count)
	if count == 1 {
		things = "1 item"
	}

	heading := theme.Title.Render("✿  Ready to spring-clean")
	question := theme.Highlight.Render(fmt.Sprintf("Move %s (%s) to the Trash?",
		things, humanize.Bytes(uint64(marked))))
	note := theme.Subtitle.Render("Items go to ~/.Trash (Finder \"Put Back\" preserved). You can sanity-check before emptying.")
	hint := theme.KeyHint.Render("[y/enter] yes, sweep it · [n/esc] cancel")
	border := theme.Sun

	if m.applyMode == applyDelete {
		heading = theme.Danger.Render("⚠  Delete, not trash")
		question = theme.Danger.Render(fmt.Sprintf("Permanently delete %s (%s)?",
			things, humanize.Bytes(uint64(marked))))
		note = theme.Subtitle.Render("Straight to gone: no Trash, no Put Back, no emptying a bin of\n" +
			"a hundred thousand files afterwards. Nothing here comes back.")
		hint = theme.KeyHint.Render("[y/enter] I'm sure · [n/esc] cancel")
		border = theme.Warning
		if m.confirmStep > 0 {
			question = theme.Danger.Render(fmt.Sprintf("Really? %s (%s), gone for good.",
				things, humanize.Bytes(uint64(marked))))
			hint = theme.KeyHint.Render("[y/enter] delete them · [n/esc] cancel")
		}
	}

	breakdown := lipgloss.NewStyle().Foreground(theme.Cream).Render(strings.Join(lines, "\n"))
	body := lipgloss.JoinVertical(lipgloss.Left,
		heading, "", question, "", breakdown, "", note, "", hint)
	panel := theme.Panel.BorderForeground(border).Width(min(80, m.width-4)).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) viewApplying() string {
	what := "moving items to the Trash via Finder"
	if m.applyMode == applyDelete {
		what = "deleting items"
	}
	body := lipgloss.JoinVertical(lipgloss.Center,
		theme.Title.Render("✿ sweeping…"),
		"",
		theme.Subtitle.Render(what),
		"",
		theme.Highlight.Render(animatedSpinner(m.animTick)),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) viewCelebration() string {
	trashed := len(m.applyResult.Trashed)
	failed := len(m.applyResult.Failed)
	skipped := len(m.applyResult.Skipped)

	var trashedBytes int64
	idx := map[string]int64{}
	for _, s := range m.suspects {
		idx[s.Path] = s.Size
	}
	for _, p := range m.applyResult.Trashed {
		trashedBytes += idx[p]
	}

	var header string
	switch {
	case trashed > 0:
		header = theme.Title.Render("✿  spring is sprung!")
	case skipped > 0 && failed == 0:
		header = theme.Subtitle.Render("✿  nothing to do — items were already gone")
	case failed > 0 && trashed == 0:
		header = theme.Danger.Render("⚠  couldn't trash anything")
	default:
		header = theme.Subtitle.Render("✿  done")
	}

	summary := theme.Highlight.Render(fmt.Sprintf("reclaimed  %s  across %d items",
		humanize.Bytes(uint64(trashedBytes)), trashed))

	parts := []string{header, "", summary}

	if skipped > 0 {
		parts = append(parts, theme.Dim.Render(fmt.Sprintf("%d items were already gone", skipped)))
	}
	if failed > 0 {
		parts = append(parts, "")
		parts = append(parts, theme.Danger.Render(fmt.Sprintf("%d items failed:", failed)))
		parts = append(parts, m.renderFailureList())
	}
	parts = append(parts, "")
	parts = append(parts, theme.KeyHint.Render("[r] rescan · [q] quit"))

	body := lipgloss.JoinVertical(lipgloss.Center, parts...)

	confetti := ""
	if m.celebration != nil && trashed > 0 {
		confetti = m.celebration.Render()
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, confetti, "", body),
	)
}

func (m Model) renderFailureList() string {
	const maxShown = 5
	home, _ := os.UserHomeDir()
	var lines []string
	i := 0
	for p, err := range m.applyResult.Failed {
		if i >= maxShown {
			break
		}
		display := p
		if home != "" && strings.HasPrefix(display, home) {
			display = "~" + display[len(home):]
		}
		lines = append(lines,
			theme.Dim.Render("  "+display)+"\n"+
				theme.Danger.Render("    ↳ ")+theme.Dim.Render(err.Error()),
		)
		i++
	}
	if len(m.applyResult.Failed) > maxShown {
		lines = append(lines, theme.Dim.Render(fmt.Sprintf("  …and %d more", len(m.applyResult.Failed)-maxShown)))
	}
	return strings.Join(lines, "\n")
}

// helpers

func (m Model) listWidth() int {
	if m.width < 80 {
		return m.width
	}
	w := m.width * 3 / 5
	if w < minPanelWidth {
		w = minPanelWidth
	}
	return w
}

func categoryColor(c domain.Category) lipgloss.Color {
	switch c {
	case domain.CatDevCache:
		return theme.Leaf
	case domain.CatAppCache:
		return theme.Sky
	case domain.CatAppLog:
		return theme.Lavender
	case domain.CatXcode:
		return theme.PetalDeep
	case domain.CatPkgCache:
		return theme.Sun
	case domain.CatDocker:
		return theme.Stone
	case domain.CatTrash:
		return theme.Warning
	case domain.CatDownload:
		return theme.Cream
	case domain.CatUnusedApp:
		return theme.Petal
	case domain.CatLargeFile:
		return theme.Sun
	case domain.CatDuplicate:
		return theme.Mist
	case domain.CatGitWorktree:
		return theme.Leaf
	case domain.CatIgnoredCruft:
		return theme.Soil
	}
	return theme.Cream
}

// truncatePath shortens a path to fit, keeping as many trailing components as
// there is room for.
//
// The tail is what tells two paths apart. A list of build caches is fifty rows
// all ending in `node_modules`, and it's the directory above that says which
// worktree each one belongs to; the leading directories they share say nothing.
func truncatePath(p string, max int) string {
	if max < 10 {
		max = 10
	}
	home, err := homeDir()
	if err == nil && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	if len(p) <= max {
		return p
	}
	parts := strings.Split(p, string(filepath.Separator))
	tail := parts[len(parts)-1]
	if len(tail)+2 > max {
		return "…" + p[len(p)-max+1:]
	}
	for i := len(parts) - 2; i > 0; i-- {
		grown := parts[i] + "/" + tail
		if len(grown)+2 > max {
			break
		}
		tail = grown
	}
	return "…/" + tail
}

func wrapPath(p string, w int) string {
	if w < 10 {
		w = 10
	}
	if len(p) <= w {
		return p
	}
	var sb strings.Builder
	for len(p) > w {
		sb.WriteString(p[:w])
		sb.WriteString("\n")
		p = p[w:]
	}
	sb.WriteString(p)
	return sb.String()
}

func padRight(s string, w int) string {
	if lipgloss.Width(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}

func homeDir() (string, error) {
	return os.UserHomeDir()
}

func animatedSpinner(tick int) string {
	frames := []string{"✿     ", " ✿    ", "  ✿   ", "   ✿  ", "    ✿ ", "     ✿", "    ✿ ", "   ✿  ", "  ✿   ", " ✿    "}
	return frames[tick%len(frames)]
}

func totalBytes(suspects []domain.Suspect) int64 {
	var t int64
	for _, s := range suspects {
		t += s.Size
	}
	return t
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
