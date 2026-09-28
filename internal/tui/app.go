package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/0xdeafcafe/springclean/internal/anim"
	"github.com/0xdeafcafe/springclean/internal/cache"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/report"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/0xdeafcafe/springclean/internal/trash"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type editorReloadedMsg struct{ r report.Report }

func editorCmd(path string) *exec.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

type Phase int

const (
	PhaseSplash Phase = iota
	PhaseOptions
	PhaseFDA
	PhaseScanning
	PhaseReview
	PhaseConfirm
	PhaseApplying
	PhasePermissionDenied
	PhaseCelebration
)

type sortMode int

const (
	sortBySize sortMode = iota
	sortByCategory
	sortByPath
	sortByLastUsed
)

func (s sortMode) Label() string {
	switch s {
	case sortBySize:
		return "size"
	case sortByCategory:
		return "category"
	case sortByPath:
		return "path"
	case sortByLastUsed:
		return "last used"
	}
	return "?"
}

type Options struct {
	ScanConfig      scan.Config
	StartFromReport *report.Report // if non-nil, skip scan and show this report
	SkipSplash      bool
	ReportPath      string // path to save reports to / load from
	// InitialFilter pre-selects a category so a targeted invocation lands on
	// its answer rather than on everything the scan happened to find.
	InitialFilter domain.Category
}

type Model struct {
	opts Options
	keys keymap

	phase Phase

	width, height int

	splashTick int
	animTick   int

	garden      *anim.Garden
	celebration *anim.Celebration

	fda     scan.FDAStatus
	fdaDone bool

	scanCtx    context.Context
	scanCancel context.CancelFunc
	scanCh     <-chan scan.Event
	progress   domain.ScanProgress
	result     domain.ScanResult

	suspects []domain.Suspect
	view     []int // indices into suspects, post-filter & sort

	filter   domain.Category
	sort     sortMode
	selected int
	listTop  int

	applyResult trash.Result
	applyErr    error
	confirmStep int
	// applyMode is what the pending confirmation will do.
	applyMode applyMode

	helpOpen bool
	flash    string
	flashAt  time.Time
	err      string

	form   optionsForm
	search search
}

func New(opts Options) Model {
	m := Model{
		opts:     opts,
		keys:     newKeymap(),
		phase:    PhaseSplash,
		filter:   opts.InitialFilter,
		sort:     sortBySize,
		selected: 0,
	}
	m.form = newOptionsForm(opts.ScanConfig)
	if opts.SkipSplash {
		m.phase = PhaseScanning
	}
	if opts.StartFromReport != nil {
		m.phase = PhaseReview
		m.suspects = append([]domain.Suspect(nil), opts.StartFromReport.Suspects...)
		m.rebuildView()
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if m.opts.StartFromReport == nil {
		cmds = append(cmds, probeFDACmd(), loadCachedCmd())
	}
	// SkipSplash used to set the phase to scanning without ever starting a
	// scan, leaving the dashboard sitting at zero forever.
	if m.opts.SkipSplash && m.opts.StartFromReport == nil {
		cmds = append(cmds, func() tea.Msg { return autoBeginMsg{} })
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
}

func probeFDACmd() tea.Cmd {
	return func() tea.Msg {
		return fdaProbedMsg{status: scan.ProbeFullDiskAccess()}
	}
}

func loadCachedCmd() tea.Cmd {
	return func() tea.Msg {
		r, ok, _ := cache.Load()
		return cachedScanLoadedMsg{result: r, ok: ok}
	}
}

func waitForScanEvent(ch <-chan scan.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return scanClosedMsg{}
		}
		return scanEventMsg{ev: ev}
	}
}

func (m *Model) startScan() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.scanCtx = ctx
	m.scanCancel = cancel
	sc := scan.New(m.opts.ScanConfig)
	m.scanCh = sc.Run(ctx)
	m.phase = PhaseScanning
	m.suspects = m.suspects[:0]
	m.view = m.view[:0]
	m.result = domain.ScanResult{}
	m.progress = domain.ScanProgress{Phase: "starting"}
	return waitForScanEvent(m.scanCh)
}

// applyMode is where marked items go.
type applyMode int

const (
	// applyTrash is the default: Finder moves the items, so Put Back works.
	applyTrash applyMode = iota
	// applyManualTrash writes into ~/.Trash directly, for when macOS won't
	// let the terminal drive Finder.
	applyManualTrash
	// applyDelete removes them outright. Faster by a wide margin on a tree of
	// many small files, and irreversible.
	applyDelete
)

func applyCmd(paths []string, mode applyMode) tea.Cmd {
	return func() tea.Msg {
		var res trash.Result
		switch mode {
		case applyManualTrash:
			res = trash.MoveManyManual(paths)
		case applyDelete:
			res = trash.DeleteMany(paths)
		default:
			res = trash.MoveMany(paths)
		}
		return applyDoneMsg{result: res}
	}
}

func allFailuresAutomationDenied(res trash.Result) bool {
	if len(res.Failed) == 0 {
		return false
	}
	for _, err := range res.Failed {
		if !trash.IsAutomationDenied(err) {
			return false
		}
	}
	return true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureAnim()
		return m, nil

	case tickMsg:
		return m.onTick()

	case tea.KeyMsg:
		return m.onKey(msg)

	case fdaProbedMsg:
		m.fda = msg.status
		m.fdaDone = true
		return m, nil

	case cachedScanLoadedMsg:
		// We only surface cached results in review phase after the user begins,
		// to avoid clobbering a fresh scan. Stored for now; consumed on Begin.
		if msg.ok {
			m.result = msg.result
		}
		return m, nil

	case autoBeginMsg:
		cmd := m.startScan()
		return m, cmd

	case scanEventMsg:
		return m.onScanEvent(msg.ev)

	case scanClosedMsg:
		// Channel closed without a DoneEvent — treat as cancelled.
		if m.phase == PhaseScanning {
			m.phase = PhaseReview
			m.rebuildView()
		}
		return m, nil

	case applyDoneMsg:
		m.applyResult = msg.result
		m.applyErr = msg.err
		if allFailuresAutomationDenied(msg.result) {
			m.phase = PhasePermissionDenied
			return m, nil
		}
		m.phase = PhaseCelebration
		m.celebration = anim.NewCelebration(m.width, m.contentHeight(), int(time.Now().UnixNano()))
		return m, nil

	case errMsg:
		m.err = msg.err.Error()
		return m, nil

	case editorReloadedMsg:
		marked := map[string]bool{}
		for _, s := range msg.r.Suspects {
			if s.Marked {
				marked[s.ID] = true
			}
		}
		for i := range m.suspects {
			m.suspects[i].Marked = marked[m.suspects[i].ID]
		}
		m.setFlash("report reloaded from $EDITOR")
		return m, nil
	}
	return m, nil
}

func (m Model) onTick() (tea.Model, tea.Cmd) {
	m.splashTick++
	m.animTick++
	if m.garden != nil {
		m.garden.Tick()
	}
	if m.celebration != nil {
		m.celebration.Tick()
	}
	return m, tickCmd()
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.phase {
	case PhaseSplash:
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		if msg.String() == "o" {
			m.phase = PhaseOptions
			return m, nil
		}
		if key.Matches(msg, m.keys.Begin) {
			if !m.fda.Granted && m.fdaDone {
				m.phase = PhaseFDA
				return m, nil
			}
			cmd := m.startScan()
			return m, cmd
		}
	case PhaseOptions:
		return m.onOptionsKey(msg)
	case PhaseFDA:
		if key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Cancel) {
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Confirm) || msg.String() == "c" {
			// continue anyway
			cmd := m.startScan()
			return m, cmd
		}
	case PhaseScanning:
		if key.Matches(msg, m.keys.Quit) {
			if m.scanCancel != nil {
				m.scanCancel()
			}
			return m, tea.Quit
		}
		if msg.String() == "esc" {
			if m.scanCancel != nil {
				m.scanCancel()
			}
			return m, nil
		}
	case PhaseReview:
		return m.onReviewKey(msg)
	case PhaseConfirm:
		return m.onConfirmKey(msg)
	case PhaseApplying:
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
	case PhasePermissionDenied:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.phase = PhaseReview
			return m, nil
		case "r", "y", "enter":
			paths := m.markedPaths()
			m.phase = PhaseApplying
			return m, applyCmd(paths, applyTrash)
		case "m":
			paths := m.markedPaths()
			m.phase = PhaseApplying
			return m, applyCmd(paths, applyManualTrash)
		}
	case PhaseCelebration:
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Rescan) {
			cmd := m.startScan()
			return m, cmd
		}
	}
	return m, nil
}

// onOptionsKey drives the scope/threshold form. While the path field is being
// typed into, printable keys go to the text rather than to navigation.
func (m Model) onOptionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.form.editing {
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			m.form.editing = false
			return m, nil
		case tea.KeyBackspace:
			m.form.backspacePath()
			return m, nil
		case tea.KeyRunes, tea.KeySpace:
			m.form.typePath(string(msg.Runes))
			if msg.Type == tea.KeySpace {
				m.form.typePath(" ")
			}
			return m, nil
		case tea.KeyCtrlU:
			m.form.path = ""
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.phase = PhaseSplash
		m.err = ""
		return m, nil
	case "up", "k":
		m.form.moveCursor(-1)
		return m, nil
	case "down", "j", "tab":
		m.form.moveCursor(1)
		return m, nil
	case "left", "h":
		m.form.adjust(-1)
		return m, nil
	case "right", "l":
		m.form.adjust(1)
		return m, nil
	case "e", "enter":
		// Enter edits the path when the cursor is on it, and otherwise starts
		// the scan, so the obvious key does the obvious thing in both places.
		if m.form.cursor == fieldPath && m.form.pathActive() {
			m.form.editing = true
			m.form.scope = scopeCustom
			return m, nil
		}
		if msg.String() == "enter" {
			return m.beginFromOptions()
		}
		return m, nil
	case " ":
		return m.beginFromOptions()
	}
	return m, nil
}

// beginFromOptions validates the form and starts a scan with it.
func (m Model) beginFromOptions() (tea.Model, tea.Cmd) {
	cfg, err := m.form.config()
	if err != nil {
		m.err = fmt.Sprintf("%v: %s", err, m.form.path)
		return m, nil
	}
	m.err = ""
	m.opts.ScanConfig = cfg
	if !m.fda.Granted && m.fdaDone && cfg.Mode != domain.ModeRoot {
		m.phase = PhaseFDA
		return m, nil
	}
	cmd := m.startScan()
	return m, cmd
}

func (m Model) onReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the search line has focus every printable key is text, so this has
	// to run before any of the single-letter list bindings.
	if m.search.active {
		return m.onSearchKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Search):
		m.search.begin()
		return m, nil
	case msg.String() == "esc":
		if m.search.on() {
			m.search.clear()
			m.selected, m.listTop = 0, 0
			m.rebuildView()
			m.setFlash("search cleared")
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.moveSelection(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveSelection(1)
	case key.Matches(msg, m.keys.PageUp):
		m.moveSelection(-m.visibleListRows() + 1)
	case key.Matches(msg, m.keys.PageDown):
		m.moveSelection(m.visibleListRows() - 1)
	case key.Matches(msg, m.keys.Home):
		m.selected = 0
		m.listTop = 0
	case key.Matches(msg, m.keys.End):
		if len(m.view) > 0 {
			m.selected = len(m.view) - 1
			m.ensureVisible()
		}
	case key.Matches(msg, m.keys.Toggle):
		m.toggleSelected()
	case key.Matches(msg, m.keys.MarkAll):
		for _, idx := range m.view {
			m.suspects[idx].Marked = true
		}
		m.setFlash(fmt.Sprintf("marked %d items", len(m.view)))
	case key.Matches(msg, m.keys.UnmarkAll):
		for i := range m.suspects {
			m.suspects[i].Marked = false
		}
		m.setFlash("unmarked everything")
	case key.Matches(msg, m.keys.MarkSafe):
		n := 0
		for _, idx := range m.view {
			if m.suspects[idx].Regenerable {
				m.suspects[idx].Marked = true
				n++
			}
		}
		m.setFlash(fmt.Sprintf("marked %d regenerable items", n))
	case key.Matches(msg, m.keys.NextCat):
		m.cycleFilter(1)
	case key.Matches(msg, m.keys.PrevCat):
		m.cycleFilter(-1)
	case key.Matches(msg, m.keys.Sort):
		m.sort = (m.sort + 1) % 4
		m.rebuildView()
		m.setFlash("sort by " + m.sort.Label())
	case key.Matches(msg, m.keys.Save):
		path := m.opts.ReportPath
		if path == "" {
			path = "springclean-report.yaml"
		}
		r := report.Build(m.result)
		// merge marked state from our suspects
		marked := map[string]bool{}
		for _, s := range m.suspects {
			marked[s.ID] = s.Marked
		}
		for i := range r.Suspects {
			if marked[r.Suspects[i].ID] {
				r.Suspects[i].Marked = true
			}
		}
		if err := report.Save(r, path); err != nil {
			m.setFlash("save failed: " + err.Error())
		} else {
			m.setFlash("report saved → " + path)
		}
	case key.Matches(msg, m.keys.Edit):
		path := m.opts.ReportPath
		if path == "" {
			path = "springclean-report.yaml"
		}
		r := report.Build(m.result)
		marked := map[string]bool{}
		for _, s := range m.suspects {
			marked[s.ID] = s.Marked
		}
		for i := range r.Suspects {
			r.Suspects[i].Marked = marked[r.Suspects[i].ID]
		}
		_ = report.Save(r, path)
		// Suspend the program, open editor, reload.
		return m, tea.ExecProcess(editorCmd(path), func(err error) tea.Msg {
			if err != nil {
				return errMsg{err: err}
			}
			loaded, loadErr := report.Load(path)
			if loadErr != nil {
				return errMsg{err: loadErr}
			}
			return editorReloadedMsg{r: loaded}
		})
	case key.Matches(msg, m.keys.Apply):
		if m.markedCount() == 0 {
			m.setFlash("nothing marked")
			return m, nil
		}
		m.phase = PhaseConfirm
		m.confirmStep = 0
		m.applyMode = applyTrash
	case key.Matches(msg, m.keys.Delete):
		if m.markedCount() == 0 {
			m.setFlash("nothing marked")
			return m, nil
		}
		m.phase = PhaseConfirm
		m.confirmStep = 0
		m.applyMode = applyDelete
	case key.Matches(msg, m.keys.Rescan):
		cmd := m.startScan()
		return m, cmd
	case key.Matches(msg, m.keys.Options):
		m.phase = PhaseOptions
		return m, nil
	}
	return m, nil
}

func (m Model) onConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Cancel) {
		m.phase = PhaseReview
		return m, nil
	}
	if key.Matches(msg, m.keys.Confirm) {
		// Deleting is irreversible, so it takes a second look rather than the
		// single keypress that sending things to the Trash gets.
		if m.applyMode == applyDelete && m.confirmStep == 0 {
			m.confirmStep = 1
			return m, nil
		}
		paths := m.markedPaths()
		m.phase = PhaseApplying
		return m, applyCmd(paths, m.applyMode)
	}
	return m, nil
}

func (m Model) onScanEvent(ev scan.Event) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case scan.ProgressEvent:
		m.progress = e.P
	case scan.SuspectEvent:
		m.suspects = append(m.suspects, e.S)
		m.refreshViewIfHot()
	case scan.ErrorEvent:
		// swallow non-fatal scan errors silently for now
	case scan.DoneEvent:
		m.result = e.R
		// Adopt the scanner's final ordering as our source of truth, but
		// preserve marks the user may have toggled mid-scan.
		marked := map[string]bool{}
		for _, s := range m.suspects {
			if s.Marked {
				marked[s.ID] = true
			}
		}
		m.suspects = append([]domain.Suspect(nil), m.result.Suspects...)
		for i := range m.suspects {
			if marked[m.suspects[i].ID] {
				m.suspects[i].Marked = true
			}
		}
		_ = cache.Save(m.result)
		m.phase = PhaseReview
		m.rebuildView()
		return m, nil
	}
	return m, waitForScanEvent(m.scanCh)
}

func (m *Model) ensureAnim() {
	h := m.animPanelHeight()
	w := m.width - 4
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	if m.garden == nil {
		m.garden = anim.NewGarden(w, h, int(time.Now().UnixNano()))
	} else {
		m.garden.Resize(w, h)
	}
}

func (m *Model) rebuildView() {
	m.view = m.view[:0]
	for i, s := range m.suspects {
		if m.filter != "" && s.Category != m.filter {
			continue
		}
		if m.search.on() && !m.search.matches(s) {
			continue
		}
		m.view = append(m.view, i)
	}
	sort.Slice(m.view, func(a, b int) bool {
		sa, sb := m.suspects[m.view[a]], m.suspects[m.view[b]]
		switch m.sort {
		case sortBySize:
			return sa.Size > sb.Size
		case sortByCategory:
			if sa.Category != sb.Category {
				return sa.Category < sb.Category
			}
			return sa.Size > sb.Size
		case sortByPath:
			return sa.Path < sb.Path
		case sortByLastUsed:
			return sa.LastUsed.Before(sb.LastUsed)
		}
		return false
	})
	if m.selected >= len(m.view) {
		m.selected = len(m.view) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.ensureVisible()
}

func (m *Model) refreshViewIfHot() {
	// During an active scan we re-sort on every Nth suspect to keep the
	// largest items pinned to the top without thrashing.
	if len(m.suspects)%5 != 0 {
		return
	}
	m.rebuildView()
}

func (m *Model) moveSelection(delta int) {
	if len(m.view) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.view) {
		m.selected = len(m.view) - 1
	}
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	h := m.visibleListRows()
	if h <= 0 {
		return
	}
	if m.selected < m.listTop {
		m.listTop = m.selected
	}
	if m.selected >= m.listTop+h {
		m.listTop = m.selected - h + 1
	}
	if m.listTop < 0 {
		m.listTop = 0
	}
}

func (m *Model) toggleSelected() {
	if len(m.view) == 0 {
		return
	}
	idx := m.view[m.selected]
	m.suspects[idx].Marked = !m.suspects[idx].Marked
}

// onSearchKey handles keystrokes while the `/` line has focus. The list
// re-filters on every character, so the result is visible as it's typed.
func (m Model) onSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.search.accept()
		if m.search.invalid {
			m.setFlash("invalid pattern")
		}
		return m, nil
	case tea.KeyEsc:
		m.search.cancel()
	case tea.KeyBackspace:
		m.search.backspace()
	case tea.KeyCtrlU:
		m.search.query = ""
		m.search.compile()
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeySpace:
		m.search.typeRune(" ")
	case tea.KeyRunes:
		m.search.typeRune(string(msg.Runes))
	default:
		return m, nil
	}
	m.selected, m.listTop = 0, 0
	m.rebuildView()
	return m, nil
}

// visibleCategories returns the tabs actually on screen: "all", then only the
// categories the scan found something in. The chips and the keys that move
// between them have to agree on this list, or the tabs step through categories
// that aren't shown and land on an empty list.
func (m Model) visibleCategories() []domain.Category {
	counts := map[domain.Category]int{}
	for _, s := range m.suspects {
		counts[s.Category]++
	}
	cats := []domain.Category{""}
	for _, c := range domain.AllCategories() {
		if counts[c] > 0 {
			cats = append(cats, c)
		}
	}
	return cats
}

func (m *Model) cycleFilter(dir int) {
	cats := m.visibleCategories()
	if len(cats) == 0 {
		return
	}
	cur := 0
	for i, c := range cats {
		if c == m.filter {
			cur = i
			break
		}
	}
	cur = (cur + dir + len(cats)) % len(cats)
	m.filter = cats[cur]
	m.selected = 0
	m.listTop = 0
	m.rebuildView()
}

func (m *Model) setFlash(s string) {
	m.flash = s
	m.flashAt = time.Now()
}

func (m Model) markedCount() int {
	n := 0
	for _, s := range m.suspects {
		if s.Marked {
			n++
		}
	}
	return n
}

func (m Model) markedBytes() int64 {
	var t int64
	for _, s := range m.suspects {
		if s.Marked {
			t += s.Size
		}
	}
	return t
}

func (m Model) markedPaths() []string {
	out := make([]string, 0, len(m.suspects))
	for _, s := range m.suspects {
		if s.Marked {
			out = append(out, s.Path)
		}
	}
	return out
}
