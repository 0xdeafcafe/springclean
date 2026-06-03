package anim

import (
	"math/rand"
	"strings"

	"github.com/0xdeafcafe/springclean/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

var petalGlyphs = []string{"✿", "❀", "✾", "✽", "❁", "✼", "❃", "❋", "✻", "✺"}
var sparkleGlyphs = []string{"·", "˙", "˚", "✧", "·"}

var petalColors = []lipgloss.Color{
	theme.Petal, theme.PetalDeep, theme.Sun, theme.Lavender, theme.Leaf, theme.Sky,
}

type particle struct {
	x, y     float64
	vx, vy   float64
	glyph    string
	color    lipgloss.Color
	lifespan int
	age      int
	sparkle  bool
}

type GardenMode int

const (
	GardenCalm GardenMode = iota
	GardenActive
	GardenSweeping
	GardenSettled
)

// Garden is a drifting petal animation suited to a small persistent panel.
// Petals are blown leftward and gently downward by a soft breeze. The mode
// controls density and wind strength so the animation can reflect what the
// app is currently doing.
type Garden struct {
	width, height int
	particles     []particle
	rng           *rand.Rand
	tick          int
	density       int
	mode          GardenMode
}

func NewGarden(w, h, seed int) *Garden {
	g := &Garden{
		width: w,
		height: h,
		rng:   rand.New(rand.NewSource(int64(seed))),
		mode:  GardenCalm,
	}
	g.density = g.targetDensity()
	for i := 0; i < g.density; i++ {
		g.particles = append(g.particles, g.spawn(true))
	}
	return g
}

func (g *Garden) SetMode(m GardenMode) {
	if g.mode == m {
		return
	}
	g.mode = m
	g.density = g.targetDensity()
}

func (g *Garden) targetDensity() int {
	base := g.width * g.height
	switch g.mode {
	case GardenActive:
		return max(8, base/7)
	case GardenSweeping:
		return max(8, base/9)
	case GardenSettled:
		return max(4, base/18)
	}
	return max(4, base/12)
}

func (g *Garden) Resize(w, h int) {
	if w == g.width && h == g.height {
		return
	}
	g.width, g.height = w, h
	g.density = g.targetDensity()
	for len(g.particles) < g.density {
		g.particles = append(g.particles, g.spawn(true))
	}
	if len(g.particles) > g.density {
		g.particles = g.particles[:g.density]
	}
}

func (g *Garden) spawn(any bool) particle {
	sparkle := g.rng.Intn(3) == 0
	glyph := pick(g.rng, petalGlyphs)
	if sparkle {
		glyph = pick(g.rng, sparkleGlyphs)
	}
	x := float64(g.width)
	if any {
		x = g.rng.Float64() * float64(g.width)
	}
	y := g.rng.Float64() * float64(g.height-1)

	// Wind strength varies by mode — scanning gusts hard, review calm.
	windBase, windJitter := 0.4, 0.6
	switch g.mode {
	case GardenActive:
		windBase, windJitter = 0.9, 1.1
	case GardenSweeping:
		windBase, windJitter = 0.6, 0.4
	case GardenSettled:
		windBase, windJitter = 0.15, 0.25
	}

	return particle{
		x:        x,
		y:        y,
		vx:       -windBase - g.rng.Float64()*windJitter,
		vy:       (g.rng.Float64() - 0.4) * 0.25,
		glyph:    glyph,
		color:    pick(g.rng, petalColors),
		lifespan: 40 + g.rng.Intn(80),
		sparkle:  sparkle,
	}
}

func (g *Garden) Tick() {
	g.tick++
	// Grow the particle slice if mode bumped density.
	for len(g.particles) < g.density {
		g.particles = append(g.particles, g.spawn(true))
	}
	for i := range g.particles {
		p := &g.particles[i]
		p.x += p.vx
		p.y += p.vy
		p.age++
		if g.tick%4 == 0 {
			p.vy += (g.rng.Float64() - 0.5) * 0.1
			if p.vy > 0.4 {
				p.vy = 0.4
			}
			if p.vy < -0.4 {
				p.vy = -0.4
			}
		}
		if p.x < -1 || p.y < -1 || p.y >= float64(g.height) || p.age > p.lifespan {
			*p = g.spawn(false)
		}
	}
	if len(g.particles) > g.density {
		g.particles = g.particles[:g.density]
	}
}

func (g *Garden) Render() string {
	if g.width <= 0 || g.height <= 0 {
		return ""
	}
	type cell struct {
		ch    string
		color lipgloss.Color
		set   bool
	}
	grid := make([][]cell, g.height)
	for i := range grid {
		grid[i] = make([]cell, g.width)
	}
	for _, p := range g.particles {
		xi, yi := int(p.x), int(p.y)
		if xi < 0 || xi >= g.width || yi < 0 || yi >= g.height {
			continue
		}
		grid[yi][xi] = cell{ch: p.glyph, color: p.color, set: true}
	}

	var sb strings.Builder
	for y, row := range grid {
		for _, c := range row {
			if c.set {
				sb.WriteString(lipgloss.NewStyle().Foreground(c.color).Render(c.ch))
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

func pick[T any](r *rand.Rand, xs []T) T {
	return xs[r.Intn(len(xs))]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
