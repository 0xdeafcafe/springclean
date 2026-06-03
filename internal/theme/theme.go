package theme

import "github.com/charmbracelet/lipgloss"

var (
	Petal     = lipgloss.Color("#FFB7C5")
	PetalDeep = lipgloss.Color("#FF8FA8")
	Leaf      = lipgloss.Color("#6FD79B")
	LeafDeep  = lipgloss.Color("#3CA66E")
	Sun       = lipgloss.Color("#FFD93D")
	Sky       = lipgloss.Color("#9CD8F2")
	Lavender  = lipgloss.Color("#B7A4DD")
	Cream     = lipgloss.Color("#FAF0E6")
	Soil      = lipgloss.Color("#9C7A5C")
	Stone     = lipgloss.Color("#5F5F6E")
	Mist      = lipgloss.Color("#B8B8C8")
	Ink       = lipgloss.Color("#1B1B25")
	Warning   = lipgloss.Color("#FF6B6B")
	Success   = lipgloss.Color("#4CD964")
)

var (
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Petal)

	Subtitle = lipgloss.NewStyle().
			Foreground(Leaf).
			Italic(true)

	Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Petal).
		Padding(0, 1)

	PanelTitle = lipgloss.NewStyle().
			Foreground(Petal).
			Bold(true).
			Padding(0, 1)

	ListItem = lipgloss.NewStyle().
			Foreground(Cream)

	ListSelected = lipgloss.NewStyle().
			Foreground(Ink).
			Background(Sun).
			Bold(true)

	Marked = lipgloss.NewStyle().
		Foreground(Leaf).
		Bold(true)

	Unmarked = lipgloss.NewStyle().
			Foreground(Mist)

	Dim = lipgloss.NewStyle().
		Foreground(Stone)

	Highlight = lipgloss.NewStyle().
			Foreground(Sun).
			Bold(true)

	Danger = lipgloss.NewStyle().
		Foreground(Warning).
		Bold(true)

	Good = lipgloss.NewStyle().
		Foreground(Success).
		Bold(true)

	KeyHint = lipgloss.NewStyle().
		Foreground(Lavender).
		Bold(true)

	ProgressBarFull  = lipgloss.NewStyle().Foreground(Leaf)
	ProgressBarEmpty = lipgloss.NewStyle().Foreground(Stone)
)
