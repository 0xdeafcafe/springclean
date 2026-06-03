package anim

import (
	"math"
	"math/rand"
	"strings"

	"github.com/0xdeafcafe/springclean/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

var confettiGlyphs = []string{"✿", "❀", "✾", "✽", "❁", "✼", "❃", "❋", "✦", "✧", "✺", "✻"}

type confettiPiece struct {
	x, y     float64
	vx, vy   float64
	color    lipgloss.Color
	glyph    string
	rotation int
}

// Celebration is a confetti burst that radiates outward from the centre.
type Celebration struct {
	width, height int
	pieces        []confettiPiece
	rng           *rand.Rand
	tick          int
}

func NewCelebration(w, h, seed int) *Celebration {
	c := &Celebration{width: w, height: h, rng: rand.New(rand.NewSource(int64(seed)))}
	count := max(20, (w*h)/15)
	cx, cy := float64(w)/2, float64(h)/2
	for i := 0; i < count; i++ {
		angle := c.rng.Float64() * 2 * math.Pi
		speed := 0.6 + c.rng.Float64()*1.4
		c.pieces = append(c.pieces, confettiPiece{
			x:     cx,
			y:     cy,
			vx:    math.Cos(angle) * speed,
			vy:    math.Sin(angle) * speed * 0.5,
			color: pick(c.rng, petalColors),
			glyph: pick(c.rng, confettiGlyphs),
		})
	}
	return c
}

func (c *Celebration) Resize(w, h int) {
	if w == c.width && h == c.height {
		return
	}
	c.width, c.height = w, h
}

func (c *Celebration) Tick() {
	c.tick++
	for i := range c.pieces {
		p := &c.pieces[i]
		p.x += p.vx
		p.y += p.vy
		p.vy += 0.04 // gravity
		p.vx *= 0.985
	}
}

func (c *Celebration) Render() string {
	if c.width <= 0 || c.height <= 0 {
		return ""
	}
	type cell struct {
		ch    string
		color lipgloss.Color
		set   bool
	}
	grid := make([][]cell, c.height)
	for i := range grid {
		grid[i] = make([]cell, c.width)
	}
	for _, p := range c.pieces {
		xi, yi := int(p.x), int(p.y)
		if xi < 0 || xi >= c.width || yi < 0 || yi >= c.height {
			continue
		}
		grid[yi][xi] = cell{ch: p.glyph, color: p.color, set: true}
	}
	var sb strings.Builder
	for y, row := range grid {
		for _, cc := range row {
			if cc.set {
				sb.WriteString(lipgloss.NewStyle().Foreground(cc.color).Render(cc.ch))
			} else {
				sb.WriteString(" ")
			}
		}
		if y < len(grid)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// CelebrationBanner returns the celebration headline overlaid on the confetti.
func CelebrationBanner(tick int, reclaimed string) string {
	pulse := lipgloss.NewStyle().
		Foreground(bloomColors[(tick/3)%len(bloomColors)]).
		Bold(true)
	lines := []string{
		pulse.Render("  ✦  ✿  ❀  spring is sprung  ❀  ✿  ✦  "),
		theme.Highlight.Render("       reclaimed " + reclaimed),
		theme.KeyHint.Render("       [q] quit · [r] re-scan"),
	}
	return strings.Join(lines, "\n")
}
