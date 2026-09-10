package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// The whole point of the grid is that durations end in the same column: a run
// is scanned down that column, and a step that stops one character short breaks
// the scan without looking broken.
func TestStepsEndOnTheSameColumn(t *testing.T) {
	lines := []string{
		logDone("Build", "", "2m 43s"),
		logDone("Push", "", "4.9s"),
		logDone("helm upgrade", "3 servizi · 1 revisione", "7.4s"),
		logFail("Sync del values", "", "1.2s"),
	}

	for _, line := range lines {
		if w := lipgloss.Width(line); w != logWidth {
			t.Errorf("riga larga %d invece di %d:\n%s", w, logWidth, stripANSI(line))
		}
	}
}

// A step with no duration is a step all the same: it keeps the glyph and label
// columns, so it lines up with the ones around it.
func TestStepWithoutValueKeepsItsColumns(t *testing.T) {
	line := stripANSI(logDone("Sessione Azure", "", ""))
	if !strings.HasPrefix(line, "  ✓  Sessione Azure") {
		t.Errorf("colonne del passo cambiate: %q", line)
	}
}

// Info lines share the label column with each other, so a block of them reads
// as a table rather than as a paragraph.
func TestInfoLinesShareTheValueColumn(t *testing.T) {
	short := stripANSI(logInfo("Immagine", "registry/apps/jboss-be:3.0.10-dev"))
	long := stripANSI(logInfo("Dockerfile", "~/repos/jboss-be/Dockerfile"))

	if strings.Index(short, "registry") != strings.Index(long, "~/repos") {
		t.Errorf("valori a colonne diverse:\n%s\n%s", short, long)
	}
}

// The service header carries the tag transition flush right, so the tags of one
// service line up with the tags of the next.
func TestServiceHeaderEndsOnTheGrid(t *testing.T) {
	head := logServiceHeader(1, 3, "jboss-be", "3.0.9-dev", "3.0.10-dev")
	line := strings.TrimPrefix(head, "\n")

	if w := lipgloss.Width(line); w != logWidth {
		t.Errorf("testata larga %d invece di %d:\n%s", w, logWidth, stripANSI(line))
	}
	for _, want := range []string{"1/3", "jboss-be", "3.0.9-dev", "3.0.10-dev"} {
		if !strings.Contains(stripANSI(line), want) {
			t.Errorf("la testata non contiene %q:\n%s", want, stripANSI(line))
		}
	}
}

// An unchanged tag is a state of the service, so it is said in the header — and
// only there, and only when it is true.
func TestServiceHeaderMarksAnUnchangedTag(t *testing.T) {
	unchanged := stripANSI(logServiceHeader(1, 1, "jboss-be", "3.0.10-dev", "3.0.10-dev"))
	if !strings.Contains(unchanged, "invariato") {
		t.Errorf("tag invariato non segnalato:\n%s", unchanged)
	}
	changed := stripANSI(logServiceHeader(1, 1, "jboss-be", "3.0.9-dev", "3.0.10-dev"))
	if strings.Contains(changed, "invariato") {
		t.Errorf("tag nuovo segnalato come invariato:\n%s", changed)
	}
}

// With one service the counter says nothing: 1/1 is noise on every line of the
// only service there is.
func TestServiceHeaderDropsTheCounterWhenAlone(t *testing.T) {
	if line := stripANSI(logServiceHeader(1, 1, "webapp", "1.0.0", "1.0.1")); strings.Contains(line, "1/1") {
		t.Errorf("contatore mostrato con un solo servizio:\n%s", line)
	}
}

// Command output belongs to the step that produced it, so it is indented under
// it instead of starting at the left margin like a new step.
func TestOutputIsIndentedUnderItsStep(t *testing.T) {
	out := logOutput([]byte("Error: UPGRADE FAILED\ncannot patch jboss-be\n"))
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(stripANSI(line), "     ") {
			t.Errorf("riga di output non rientrata: %q", stripANSI(line))
		}
	}
	if logOutput(nil) != "" {
		t.Error("output vuoto: non deve produrre righe")
	}
}

// The frame hugs its content: a box wider than what it holds turns a two-line
// question into something that looks like a form.
func TestQuestionBoxHugsItsContent(t *testing.T) {
	box := questionBox(SelectedItemStyle.Render("Riavviare?"), CursorStyle,
		[]string{ItemStyle.Render("> Riavvia"), ItemStyle.Render("  Salta")})

	width := 0
	for _, line := range strings.Split(box, "\n") {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	// "  " margin + border + padding + title + padding + border
	if want := 2 + 1 + len("─ Riavviare? ") + 3 + 1; width > want+2 {
		t.Errorf("riquadro largo %d colonne per un titolo di %d", width, len("Riavviare?"))
	}

	lines := strings.Split(box, "\n")
	for _, line := range lines {
		if w := lipgloss.Width(line); w != lipgloss.Width(lines[0]) {
			t.Errorf("bordo non allineato: riga larga %d, prima riga %d", w, lipgloss.Width(lines[0]))
		}
	}
}
