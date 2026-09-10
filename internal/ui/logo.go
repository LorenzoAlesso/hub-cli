package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	figure "github.com/common-nighthawk/go-figure"
)

const (
	logoCharDelay = 6 * time.Millisecond
	logoEndDelay  = 450 * time.Millisecond

	// The subtitle says what hub-cli does, not where: it deploys to the local
	// cluster and to PSN, so naming only the first dated the opening screen.
	logoTagline = "build · push · deploy"
	logoTargets = "locale · psn"
)

type logoCharMsg struct{}
type logoEndMsg struct{}

type logoModel struct {
	ascii   []rune // flat rune array of the ASCII art (newlines embedded)
	sub     []rune // subtitle runes
	visible int    // runes revealed so far (across ascii + sub)
	total   int
	done    bool
}

func newLogoModel() logoModel {
	fig := figure.NewFigure("HUB-CLI", "slant", true)
	raw := fig.String()

	// Trim trailing spaces per line so the cursor doesn't float on whitespace.
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	ascii := []rune(strings.Join(lines, "\n"))
	sub := []rune("\n\n  " + logoTagline + logoSubtitleGap(lines) + logoTargets)

	return logoModel{
		ascii: ascii,
		sub:   sub,
		total: len(ascii) + len(sub),
	}
}

// logoSubtitleGap right-aligns the targets under the last column of the
// lettering, so the subtitle reads as the base of the logo instead of a line
// that happens to follow it.
func logoSubtitleGap(lines []string) string {
	width := 0
	for _, l := range lines {
		if n := len([]rune(l)); n > width {
			width = n
		}
	}
	gap := width - 2 - len([]rune(logoTagline)) - len([]rune(logoTargets))
	if gap < 2 {
		gap = 2
	}
	return strings.Repeat(" ", gap)
}

func (m logoModel) Init() tea.Cmd {
	return logoTick()
}

func logoTick() tea.Cmd {
	return tea.Tick(logoCharDelay, func(t time.Time) tea.Msg {
		return logoCharMsg{}
	})
}

func (m logoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case logoCharMsg:
		if m.visible < m.total {
			m.visible++
			return m, logoTick()
		}
		// Animation complete — short pause, then exit.
		return m, tea.Tick(logoEndDelay, func(t time.Time) tea.Msg {
			return logoEndMsg{}
		})
	case logoEndMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m logoModel) View() tea.View {
	asciiLen := len(m.ascii)
	logoStyle := lipgloss.NewStyle().Foreground(Accent)
	typing := !m.done && m.visible < m.total

	var sb strings.Builder

	if m.visible <= asciiLen {
		// Still typing the ASCII art.
		sb.WriteString(logoStyle.Render(string(m.ascii[:m.visible])))
		if typing {
			sb.WriteString(CursorStyle.Render("▌"))
		}
	} else {
		// ASCII art done, now typing the subtitle.
		sb.WriteString(logoStyle.Render(string(m.ascii)))
		subIdx := m.visible - asciiLen
		sb.WriteString(DimStyle.Render(string(m.sub[:subIdx])))
		if typing {
			sb.WriteString(DimStyle.Render("▌"))
		}
	}

	return tea.NewView(sb.String())
}

// RunLogo plays the typewriter logo animation and blocks until it finishes.
func RunLogo() {
	_, _ = tea.NewProgram(newLogoModel()).Run()
}
