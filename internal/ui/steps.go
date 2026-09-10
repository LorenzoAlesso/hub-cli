package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// These print outside the BubbleTea programs — the preflight before the TUI
// starts, and the summary after it exits. They follow the same grid as the log
// inside it, so a run reads as one screen and not as three.

// PrintOK prints a success line. A message is not a step: it keeps the colour of
// its outcome across the whole line, where a step colours only its glyph.
func PrintOK(msg string) {
	fmt.Println("  " + SuccessStyle.Render("✓") + "  " + SuccessStyle.Render(msg))
}

// PrintWarn prints a warning line.
func PrintWarn(msg string) {
	fmt.Println(WarnStyle.Render("  ⚠  " + msg))
}

// PrintErr prints an error line.
func PrintErr(msg string) {
	fmt.Println("  " + ErrStyle.Render("✗") + "  " + ErrStyle.Render(msg))
}

// PrintInfo prints a dimmed info line.
func PrintInfo(msg string) {
	fmt.Println(DimStyle.Render("  ·  " + msg))
}

// PrintRunning prints an in-progress action line.
func PrintRunning(msg string) {
	fmt.Println(CursorStyle.Render("  ▸  ") + ValueStyle.Render(msg))
}

// PrintStepDone prints a finished preflight step with its duration in the same
// column as every other duration of the run.
func PrintStepDone(label, note, elapsed string) {
	fmt.Println(logDone(label, note, elapsed))
}

// PrintDryRun prints a command that would be executed in dry-run mode.
func PrintDryRun(msg string) {
	fmt.Println(SecondaryStyle.Render("  ◆ DRY-RUN") + "  " + DimStyle.Render(msg))
}

// RenderGradientSeparator returns a horizontal line shaded Accent→Secondary.
func RenderGradientSeparator(width int) string {
	colors := lipgloss.Blend1D(width, Accent, Secondary)
	var sb strings.Builder
	for _, c := range colors {
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render("─"))
	}
	return sb.String()
}

// PrintRunHeader states where a run is going, once, before anything runs. These
// facts hold for the whole run, so they head it instead of scrolling past mixed
// with the steps — and printing them before the first step means the long Azure
// phase happens under a heading that already says where.
func PrintRunHeader(label, env, namespace string, details []string) {
	head := "  " + DimStyle.Render(label) + SelectedItemStyle.Render(env)
	if namespace != "" {
		ns := DimStyle.Render("namespace ") + ValueStyle.Render(namespace)
		head += pad(lipgloss.Width(head), logWidth-lipgloss.Width(ns)) + ns
	}
	fmt.Println(head)
	if len(details) > 0 {
		fmt.Println("  " + ValueStyle.Render(details[0]) +
			DimStyle.Render("  ·  "+strings.Join(details[1:], "  ·  ")))
	}
	fmt.Println()
}

// DeployResult is the outcome of deploying a single service.
type DeployResult struct {
	Service   string
	OldTag    string
	NewTag    string
	Elapsed   time.Duration
	Skipped   bool
	Restarted bool // redeployed under the same tag, so its pod was restarted
}

// summaryHeader and summaryFooter are the two things the closing table cannot
// read off the results: where the run went, and what closed it. The workflows
// know both and the command that prints the summary does not, so they are left
// here the way the status bar is.
var summaryHeader, summaryFooter string

// SetSummaryContext records what the closing table says about the run itself.
func SetSummaryContext(header, footer string) {
	summaryHeader = header
	summaryFooter = footer
}

// Summary table geometry, as minimums. The columns widen to fit what the table
// holds — a long service name pushes the tags right instead of running into
// them — and the frame widens with them only when it has to.
const (
	sumContent = 64 // minimum usable width between the frame's padding
	sumTagCol  = 12 // minimum column of the tag transition
	sumFlagCol = 42 // minimum column of a flag about the service
)

// PrintDeploySummary closes the run with one table inside one frame. It used to
// be three bordered panels per service — nine frames for three services, where
// the border separated columns of the same row rather than one thing from
// another.
func PrintDeploySummary(results []DeployResult) {
	if len(results) == 0 {
		return
	}

	var total time.Duration
	for _, r := range results {
		if !r.Skipped {
			total += r.Elapsed
		}
	}
	// With one service the total would repeat the line above it.
	totalStr := ""
	if len(results) > 1 {
		totalStr = formatElapsed(total)
	}

	l := layoutSummary(results, summaryHeader, summaryFooter, totalStr)
	rows := []string{l.title(), ""}
	for _, r := range results {
		rows = append(rows, l.row(r))
	}
	rows = append(rows, DimStyle.Render(strings.Repeat("─", l.width)), l.closing(totalStr))

	// A blank line on each side: the summary closes the run, and what follows it
	// is the shell prompt.
	fmt.Printf("\n%s\n\n", l.box(rows))
}

// summaryLayout is where each column of one summary table starts, and how wide
// the table is.
type summaryLayout struct {
	tagCol, flagCol, width int
}

// layoutSummary sizes the columns to the rows: each starts two spaces after the
// widest value of the column before it, and never before its minimum — so a
// table of short names keeps the geometry it always had.
func layoutSummary(results []DeployResult, header, footer, total string) summaryLayout {
	name, tags, flag, dur := 0, 0, 0, 0
	for _, r := range results {
		name = max(name, lipgloss.Width(r.Service))
		tags = max(tags, lipgloss.Width(r.OldTag+" → "+r.NewTag))
		flag = max(flag, lipgloss.Width(summaryFlag(r)))
		if !r.Skipped {
			dur = max(dur, lipgloss.Width(formatElapsed(r.Elapsed)))
		}
	}

	l := summaryLayout{tagCol: max(sumTagCol, name+2)}
	l.flagCol = max(sumFlagCol, l.tagCol+tags+2)
	end := l.tagCol + tags
	if flag > 0 {
		end = l.flagCol + flag
	}
	l.width = max(sumContent,
		end+2+dur,
		lipgloss.Width("RIEPILOGO")+2+lipgloss.Width(header),
		lipgloss.Width(footer)+2+lipgloss.Width(total))
	return l
}

func (l summaryLayout) title() string {
	line := DimStyle.Render("RIEPILOGO")
	if summaryHeader == "" {
		return line
	}
	return line + pad(9, l.width-lipgloss.Width(summaryHeader)) + DimStyle.Render(summaryHeader)
}

// row states, for one service, what it was, what it became, whether it needed
// a restart, and how long its own pipeline took.
func (l summaryLayout) row(r DeployResult) string {
	line := ValueStyle.Render(r.Service)
	col := lipgloss.Width(r.Service)

	tagStyle := SuccessStyle
	if r.OldTag == r.NewTag {
		tagStyle = WarnStyle
	}
	line += pad(col, l.tagCol) + DimStyle.Render(r.OldTag+" → ") + tagStyle.Render(r.NewTag)
	col = max(col+1, l.tagCol) + lipgloss.Width(r.OldTag+" → "+r.NewTag)

	if flag := summaryFlag(r); flag != "" {
		flagStyle := DimStyle
		if r.Restarted {
			flagStyle = CursorStyle
		}
		line += pad(col, l.flagCol) + flagStyle.Render(flag)
		col = max(col+1, l.flagCol) + lipgloss.Width(flag)
	}

	if r.Skipped {
		return line
	}
	elapsed := formatElapsed(r.Elapsed)
	return line + pad(col, l.width-lipgloss.Width(elapsed)) + ValueStyle.Render(elapsed)
}

// summaryFlag is what the table says about a service beyond its tags.
func summaryFlag(r DeployResult) string {
	switch {
	case r.Skipped:
		return "dry-run"
	case r.Restarted:
		return "↻ riavviato"
	}
	return ""
}

// closing is the line under the rule: what closed the run and, with more than
// one service, how long the services took in all.
func (l summaryLayout) closing(total string) string {
	line := SuccessStyle.Render(summaryFooter)
	if total == "" {
		return line
	}
	return line + pad(lipgloss.Width(summaryFooter), l.width-lipgloss.Width(total)) + ValueStyle.Render(total)
}

// box frames the table. One border, around the whole thing, with a blank row
// inside above and below so the rows do not touch it.
func (l summaryLayout) box(rows []string) string {
	inner := l.width + 4
	var sb strings.Builder
	sb.WriteString("  " + DimStyle.Render("╭"+strings.Repeat("─", inner)+"╮") + "\n")
	for _, row := range append(append([]string{""}, rows...), "") {
		gap := l.width - lipgloss.Width(row)
		if gap < 0 {
			gap = 0
		}
		sb.WriteString("  " + DimStyle.Render("│") + "  " + row + strings.Repeat(" ", gap) +
			"  " + DimStyle.Render("│") + "\n")
	}
	sb.WriteString("  " + DimStyle.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return sb.String()
}
