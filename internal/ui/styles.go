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
// Called from init() after overriding colors for light terminals.
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

func init() {
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return // dark palette is already correct
	}
	// Light terminal: use darker, more saturated variants for readability.
	Accent = lipgloss.Color("#0077AA")
	Secondary = lipgloss.Color("#2A6E2A")
	Muted = lipgloss.Color("#555570")
	Success = lipgloss.Color("#1E6B1E")
	Warning = lipgloss.Color("#7A5500")
	Err = lipgloss.Color("#AA2020")
	White = lipgloss.Color("#0D0D20")
	rebuildStyles()
}
