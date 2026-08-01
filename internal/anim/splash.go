package anim

import (
	"strings"

	"github.com/0xdeafcafe/springclean/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

const titleArt = `
 ███████╗██████╗ ██████╗ ██╗███╗   ██╗ ██████╗
 ██╔════╝██╔══██╗██╔══██╗██║████╗  ██║██╔════╝
 ███████╗██████╔╝██████╔╝██║██╔██╗ ██║██║  ███╗
 ╚════██║██╔═══╝ ██╔══██╗██║██║╚██╗██║██║   ██║
 ███████║██║     ██║  ██║██║██║ ╚████║╚██████╔╝
 ╚══════╝╚═╝     ╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝ ╚═════╝
        ██████╗██╗     ███████╗ █████╗ ███╗   ██╗
       ██╔════╝██║     ██╔════╝██╔══██╗████╗  ██║
       ██║     ██║     █████╗  ███████║██╔██╗ ██║
       ██║     ██║     ██╔══╝  ██╔══██║██║╚██╗██║
       ╚██████╗███████╗███████╗██║  ██║██║ ╚████║
        ╚═════╝╚══════╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝`

// SplashFrames returns the bloom animation: each row corresponds to a stage
// of growth (seed → sprout → bud → bloom), tiled across the splash garden.
type bloomStage struct {
	row1, row2, row3, row4 string
}

func stage(i int) bloomStage {
	stages := []bloomStage{
		// seed
		{"   .   ", "       ", "       ", "   _   "},
		// sprout
		{"   |   ", "   |   ", "       ", "  _|_  "},
		// bud
		{"   o   ", "   |   ", "   |   ", "  _|_  "},
		// half bloom
		{"  ✿✿✿  ", "   |   ", "   |   ", "  _|_  "},
		// full bloom (5)
		{" \\❀|❀/ ", "  \\|/  ", "   |   ", "  _|_  "},
	}
	if i < 0 {
		i = 0
	}
	if i >= len(stages) {
		i = len(stages) - 1
	}
	return stages[i]
}

var bloomColors = []lipgloss.Color{
	theme.Petal, theme.Sun, theme.Lavender, theme.Leaf, theme.Sky, theme.PetalDeep,
}

// RenderSplash returns the splash frame for the given tick. width is the
// available terminal width; height is unused but the result is line-bounded.
func RenderSplash(tick, width int) string {
	if width < 40 {
		width = 40
	}
	title := renderTitle(tick)

	// Garden of 5 flowers, each at a stage proportional to tick.
	flowerCount := 7
	row1, row2, row3, row4 := []string{}, []string{}, []string{}, []string{}
	for i := 0; i < flowerCount; i++ {
		bloomAt := tick - i*3
		st := stage(bloomAt / 4)
		color := bloomColors[i%len(bloomColors)]
		style := lipgloss.NewStyle().Foreground(color)
		row1 = append(row1, style.Render(st.row1))
		row2 = append(row2, style.Render(st.row2))
		row3 = append(row3, style.Render(st.row3))
		row4 = append(row4, style.Render(st.row4))
	}
	soil := lipgloss.NewStyle().Foreground(theme.Soil).Render(strings.Repeat("~", flowerCount*7))

	garden := strings.Join(row1, "") + "\n" +
		strings.Join(row2, "") + "\n" +
		strings.Join(row3, "") + "\n" +
		strings.Join(row4, "") + "\n" +
		soil

	subtitle := theme.Subtitle.Render("✨ spring-clean your disk · made with goroutines and care ✨")

	hint := theme.KeyHint.Render("press [space] to begin · [o] scan options · [q] to quit")

	body := lipgloss.JoinVertical(lipgloss.Center,
		title,
		"",
		garden,
		"",
		subtitle,
		"",
		hint,
	)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, body)
}

func renderTitle(tick int) string {
	lines := strings.Split(strings.TrimLeft(titleArt, "\n"), "\n")
	var sb strings.Builder
	for i, line := range lines {
		col := bloomColors[(i+tick/6)%len(bloomColors)]
		sb.WriteString(lipgloss.NewStyle().Foreground(col).Bold(true).Render(line))
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
