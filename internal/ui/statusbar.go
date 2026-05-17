package ui

import "charm.land/lipgloss/v2"

var statusLeft, statusRight string

func SetStatus(left, right string) {
	statusLeft = left
	statusRight = right
}

func ClearStatus() {
	statusLeft = ""
	statusRight = ""
}

func renderStatusBar(width int) string {
	if statusLeft == "" && statusRight == "" {
		return ""
	}

	barBg := lipgloss.Color("#0d1117")

	left := lipgloss.NewStyle().
		Background(Secondary).
		Foreground(lipgloss.Color("#0d0d0d")).
		Bold(true).
		Padding(0, 1).
		Render(statusLeft)

	right := lipgloss.NewStyle().
		Background(barBg).
		Foreground(Accent).
		Padding(0, 1).
		Render(statusRight)

	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 0 {
		gap = 0
	}

	spacer := lipgloss.NewStyle().Background(barBg).Width(gap).Render("")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, spacer, right)
}
