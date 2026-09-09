package ui

import (
	"strings"
	"testing"
)

// renderLog joins log entries the way the workflow views do.
func renderLog(entries []string) []string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e + "\n")
	}
	return strings.Split(strings.TrimSuffix(sb.String(), "\n"), "\n")
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

// The card announcing the tag must sit between one blank line above and one
// below: an asymmetric gap reads as if the card belonged to the lines under it.
func TestTagCardSpacingIsSymmetric(t *testing.T) {
	lines := renderLog([]string{
		"  ·  Immagine corrente: registry/apps/jboss-be:1.0.0-dev",
		wfTagCard("jboss-be", "1.0.0-dev", "1.0.1-dev"),
		"  ·  Dockerfile: /repo/jboss-be/Dockerfile",
	})

	first, last := -1, -1
	for i, l := range lines {
		if strings.ContainsAny(l, "╭╮╰╯") {
			if first == -1 {
				first = i
			}
			last = i
		}
	}
	if first == -1 {
		t.Fatalf("bordo della card non trovato in:\n%s", strings.Join(lines, "\n"))
	}

	if first < 1 || !isBlank(lines[first-1]) {
		t.Errorf("manca la riga vuota sopra la card:\n%s", strings.Join(lines, "\n"))
	}
	if last+1 >= len(lines) || !isBlank(lines[last+1]) {
		t.Errorf("manca la riga vuota sotto la card:\n%s", strings.Join(lines, "\n"))
	}
	if first >= 2 && isBlank(lines[first-2]) {
		t.Errorf("due righe vuote sopra la card:\n%s", strings.Join(lines, "\n"))
	}
	if last+2 < len(lines) && isBlank(lines[last+2]) {
		t.Errorf("due righe vuote sotto la card:\n%s", strings.Join(lines, "\n"))
	}
}

// The tags are what the card exists to show: they have to survive styling.
func TestTagCardShowsBothTags(t *testing.T) {
	card := wfTagCard("jboss-be", "3.0.9-dev", "3.0.10-dev")
	for _, want := range []string{"jboss-be", "3.0.9-dev", "3.0.10-dev"} {
		if !strings.Contains(card, want) {
			t.Errorf("la card non contiene %q:\n%s", want, card)
		}
	}
}
