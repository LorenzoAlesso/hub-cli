package ui

import (
	"os"

	"charm.land/lipgloss/v2"
)

var (
	Accent    = lipgloss.Color("#00D7FF")
	Secondary = lipgloss.Color("#C3E88D")
	Muted     = lipgloss.Color("#6C7086")
	Success   = lipgloss.Color("#A6E3A1")
	Warning   = lipgloss.Color("#F9E2AF")
	Err       = lipgloss.Color("#F38BA8")
	White     = lipgloss.Color("#CDD6F4")

	TitleStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true).
			MarginBottom(1)

	LabelStyle = lipgloss.NewStyle().
			Foreground(Muted)

	ValueStyle = lipgloss.NewStyle().
			Foreground(White)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Success)

	WarnStyle = lipgloss.NewStyle().
			Foreground(Warning)

	ErrStyle = lipgloss.NewStyle().
			Foreground(Err)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Muted).
			Padding(0, 1)

	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(Accent).
				Bold(true)

	ItemStyle = lipgloss.NewStyle().
			Foreground(White)

	CursorStyle = lipgloss.NewStyle().
			Foreground(Accent)

	HelpStyle = lipgloss.NewStyle().
			Foreground(Muted).
			MarginTop(1)

	StepDoneStyle    = lipgloss.NewStyle().Foreground(Success)
	StepActiveStyle  = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	StepPendingStyle = lipgloss.NewStyle().Foreground(Muted)

	SectionStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true).
			MarginTop(1)

	DimStyle = lipgloss.NewStyle().Foreground(Muted)

	SecondaryStyle = lipgloss.NewStyle().Foreground(Secondary).Bold(true)
)

// rebuildStyles rebuilds every style from the current palette colors.
func rebuildStyles() {
	TitleStyle = lipgloss.NewStyle().Foreground(Accent).Bold(true).MarginBottom(1)
	LabelStyle = lipgloss.NewStyle().Foreground(Muted)
	ValueStyle = lipgloss.NewStyle().Foreground(White)
	SuccessStyle = lipgloss.NewStyle().Foreground(Success)
	WarnStyle = lipgloss.NewStyle().Foreground(Warning)
	ErrStyle = lipgloss.NewStyle().Foreground(Err)
	BoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Muted).Padding(0, 1)
	SelectedItemStyle = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	ItemStyle = lipgloss.NewStyle().Foreground(White)
	CursorStyle = lipgloss.NewStyle().Foreground(Accent)
	HelpStyle = lipgloss.NewStyle().Foreground(Muted).MarginTop(1)
	StepDoneStyle = lipgloss.NewStyle().Foreground(Success)
	StepActiveStyle = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	StepPendingStyle = lipgloss.NewStyle().Foreground(Muted)
	SectionStyle = lipgloss.NewStyle().Foreground(Accent).Bold(true).MarginTop(1)
	DimStyle = lipgloss.NewStyle().Foreground(Muted)
	SecondaryStyle = lipgloss.NewStyle().Foreground(Secondary).Bold(true)
}

// DetectTerminalBackground adapts the palette to the terminal background. Only a
// real console is asked: the query waits for a reply that never arrives when
// stdout is redirected, or from an MSYS pipe that only claims to be a terminal.
func DetectTerminalBackground() {
	if !isCharDevice(os.Stdin) || !isCharDevice(os.Stdout) {
		return // no terminal to ask: keep the dark palette
	}
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return // dark palette is already correct
	}
	applyLightPalette()
}

// applyLightPalette switches to darker variants for a light terminal.
func applyLightPalette() {
	Accent = lipgloss.Color("#0077AA")
	Secondary = lipgloss.Color("#2A6E2A")
	Muted = lipgloss.Color("#555570")
	Success = lipgloss.Color("#1E6B1E")
	Warning = lipgloss.Color("#7A5500")
	Err = lipgloss.Color("#AA2020")
	White = lipgloss.Color("#0D0D20")
	rebuildStyles()
}

// isCharDevice reports whether f is a console rather than a pipe or a file.
func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
