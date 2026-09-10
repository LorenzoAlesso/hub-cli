package ui

import (
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// The log is drawn on one grid, shared by both workflows: glyph in column 3,
// label in column 6, note in column 30, value flush right at logWidth. Fixed
// columns are what make a run scannable vertically — durations line up under
// each other instead of following the length of the label before them.
const (
	logWidth   = 72 // right edge of the grid
	logLabel   = 5  // where a label starts, after "  x  "
	logNote    = 30 // where the note column starts
	logValue16 = 16 // where an info line's value starts
)

// pad returns the spaces needed to move from column `at` to column `to`,
// never less than one space so two fields cannot touch.
func pad(at, to int) string {
	if to <= at {
		return " "
	}
	return strings.Repeat(" ", to-at)
}

// logStep is one step of the run: a glyph, what it did, optionally what it did
// it to, and how long it took.
func logStep(glyph string, glyphStyle lipgloss.Style, label, note, value string) string {
	var b strings.Builder
	b.WriteString("  " + glyphStyle.Render(glyph) + "  ")
	col := logLabel + lipgloss.Width(label)
	b.WriteString(ValueStyle.Render(label))

	if note != "" {
		b.WriteString(pad(col, logNote))
		col = max(col+1, logNote) + lipgloss.Width(note)
		b.WriteString(DimStyle.Render(note))
	}
	if value != "" {
		b.WriteString(pad(col, logWidth-lipgloss.Width(value)))
		b.WriteString(ValueStyle.Render(value))
	}
	return b.String()
}

// logDone, logFail and logWarnStep are the three outcomes a step can have. A
// failure keeps the same columns as a success: only the glyph and the colour
// say it went wrong.
func logDone(label, note, value string) string {
	return logStep("✓", SuccessStyle, label, note, value)
}

func logFail(label, note, value string) string {
	return logStep("✗", ErrStyle, label, note, value)
}

// logRunning is the step in flight. The glyph is the spinner frame, already
// styled by the spinner itself, so it goes through untouched: a running step
// keeps the same columns as the finished ones above it, instead of being a
// bare spinner floating next to a number.
func logRunning(frame, label, elapsed string) string {
	return logStep(frame, lipgloss.NewStyle(), label, "", elapsed)
}

// logDetail continues the step above it: an error message, or a hint about what
// to do next. It is indented under the label, not under the glyph.
func logDetail(text string) string {
	return "     " + DimStyle.Render(text)
}

// logFailure is a failed step plus its reason. The reason goes underneath and
// not in the note column: an error message is a sentence, and a sentence would
// push the duration off the grid.
func logFailure(label string, err error, value string) []string {
	lines := []string{logFail(label, "", value)}
	if err != nil {
		lines = append(lines, logDetail(err.Error()))
	}
	return lines
}

// shortPath abbreviates the home directory to ~. Every managed clone lives
// under it, so the prefix is the same on every path printed and only pushes the
// part that differs towards the right margin.
func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// logInfo states a fact about the run rather than something that happened.
func logInfo(label, value string) string {
	return "  " + DimStyle.Render("·") + "  " +
		DimStyle.Render(label) + pad(logLabel+lipgloss.Width(label), logValue16) +
		ValueStyle.Render(value)
}

// logWarn is an advisory, with its continuation lines indented under the text
// rather than under the glyph.
func logWarn(lines ...string) []string {
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == 0 {
			out = append(out, "  "+WarnStyle.Render("⚠")+"  "+WarnStyle.Render(l))
			continue
		}
		out = append(out, "     "+DimStyle.Render(l))
	}
	return out
}

// logOutput indents captured command output under the step that produced it,
// so it reads as belonging to that step instead of to the run.
func logOutput(out []byte) string {
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = "     " + DimStyle.Render(strings.TrimRight(l, "\r"))
	}
	return strings.Join(lines, "\n")
}

// logSection opens a part of the run: the services that compose it, and the
// release that closes it. A blank line on each side is what makes it read as a
// heading instead of as one more step in the list above. SectionStyle is not
// used for the pieces of these lines: it carries a top margin, which lipgloss
// re-applies on every Render and would break one heading into three.
func logSection(title, note string) string {
	line := "\n" + CursorStyle.Render("──  ") + SelectedItemStyle.Render(title)
	if note != "" {
		line += "  " + DimStyle.Render(note)
	}
	return line + "\n"
}

// logServiceHeader opens one service and carries its tag transition, flush
// right. The tag used to live in a framed card of its own, which spent a
// border on information and repeated what this line already says.
func logServiceHeader(idx, total int, name, oldTag, newTag string) string {
	head := CursorStyle.Render("──  ")
	col := 4
	if total > 1 {
		counter := strconv.Itoa(idx) + "/" + strconv.Itoa(total)
		head += DimStyle.Render(counter) + "  "
		col += len(counter) + 2
	}
	head += SelectedItemStyle.Render(name)
	col += lipgloss.Width(name)

	// Until the tag is chosen the header already stands, its tag half pending:
	// whatever the service logs before the question lands underneath it.
	if newTag == "" {
		tail := oldTag + " → …"
		return "\n" + head + pad(col, logWidth-lipgloss.Width(tail)) + DimStyle.Render(tail) + "\n"
	}

	unchanged := oldTag == newTag
	tagStyle := SuccessStyle
	tail := oldTag + " → " + newTag
	if unchanged {
		tagStyle = WarnStyle
		tail += "  invariato"
	}

	head += pad(col, logWidth-lipgloss.Width(tail))
	head += DimStyle.Render(oldTag) + DimStyle.Render(" → ") + tagStyle.Render(newTag)
	if unchanged {
		head += WarnStyle.Render("  invariato")
	}
	return "\n" + head + "\n"
}

// The tracker has two rows because a run has two scales: the phases the whole
// run goes through, and the stages the service in hand goes through. Deploy and
// Sync used to sit in the second row, promising per service something that
// happens once for everybody.
type trackerState int

const (
	trackPending trackerState = iota
	trackInteractive
	trackSpinning
	trackFailed
	trackDone
)

// trackerTab renders one entry of either row. frame is the current spinner
// frame, used for whatever is running right now.
func trackerTab(state trackerState, label, frame string) string {
	switch state {
	case trackDone:
		return SuccessStyle.Render("✓ ") + DimStyle.Render(label)
	case trackSpinning:
		return frame + " " + SelectedItemStyle.Render(label)
	case trackInteractive:
		return CursorStyle.Render("▸ ") + SelectedItemStyle.Render(label)
	case trackFailed:
		return ErrStyle.Render("✗ " + label)
	default:
		return DimStyle.Render("· " + label)
	}
}

// trackerRow joins the entries of a row. Two spaces, not a pipe: with six
// phases the separators cost more width than they buy.
func trackerRow(tabs []string) string {
	return "  " + strings.Join(tabs, "  ")
}

// stageRow is the per-service row: the service in hand, then its stages. The
// arrows are what make it read as a sequence rather than a set.
func stageRow(service string, tabs []string) string {
	row := "  " + SelectedItemStyle.Render(strings.ToUpper(service))
	return row + pad(2+lipgloss.Width(service), 14) +
		strings.Join(tabs, DimStyle.Render("  →  "))
}

// trackerRule closes the tracker, at the terminal width when it is known.
func trackerRule(width int) string {
	if width <= 4 {
		width = 80
	}
	return DimStyle.Render("  " + strings.Repeat("─", width-4))
}

// questionBox frames a decision. A border means the run has stopped and is
// waiting for an answer, so it is spent here and nowhere else; the title rides
// on the top edge, and the colour says who is asking — the workflow, or an
// error that needs a way out. The title arrives already styled, because a list
// adds its filter state to it and only the caller knows which part of it is a
// count rather than a question. The frame hugs what it holds: a box wider than
// its content turns a two-line question into something that looks like a form.
func questionBox(title string, border lipgloss.Style, body []string) string {
	inner := lipgloss.Width(title) + 6
	for _, l := range body {
		if w := lipgloss.Width(l) + 4; w > inner {
			inner = w
		}
	}

	var b strings.Builder
	fill := inner - lipgloss.Width(title) - 3
	if fill < 1 {
		fill = 1
	}
	b.WriteString("  " + border.Render("╭─ ") + title +
		border.Render(" "+strings.Repeat("─", fill)+"╮") + "\n")

	// A blank row above and below what is being asked: the options should not
	// touch the frame that holds them.
	for _, l := range append(append([]string{""}, body...), "") {
		gap := inner - 4 - lipgloss.Width(l)
		if gap < 0 {
			gap = 0
		}
		b.WriteString("  " + border.Render("│") + "  " + l + strings.Repeat(" ", gap) +
			"  " + border.Render("│") + "\n")
	}

	b.WriteString("  " + border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return b.String()
}
