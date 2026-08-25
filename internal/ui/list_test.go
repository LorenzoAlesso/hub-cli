package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestDockerfileItemsLabelsAreRelative(t *testing.T) {
	root := filepath.FromSlash(`C:/Users/x/dev/ACME/docker`)
	files := []string{
		filepath.Join(root, "consul", "Dockerfile"),
		filepath.Join(root, "jboss-eap", "modules", "Dockerfile"),
	}

	items := dockerfileItems(files, root)

	for i, item := range items {
		if item.Value != files[i] {
			t.Errorf("Value alterato: %q, atteso %q", item.Value, files[i])
		}
		if strings.Contains(item.Label, "ACME") {
			t.Errorf("Label ancora assoluta: %q", item.Label)
		}
	}
	if want := filepath.Join("jboss-eap", "modules", "Dockerfile"); items[1].Label != want {
		t.Errorf("Label = %q, atteso %q", items[1].Label, want)
	}
}

func TestDockerfileItemsKeepsPathsOutsideRoot(t *testing.T) {
	root := filepath.FromSlash(`C:/dev/docker`)
	outside := filepath.FromSlash(`D:/altro/repo/Dockerfile`)

	items := dockerfileItems([]string{outside}, root)

	if items[0].Label != outside {
		t.Errorf("un path fuori dalla root va mostrato intero: %q", items[0].Label)
	}
}

// The box is sized on its widest row, so absolute paths would double its width
// without adding information.
func TestDockerfileListStaysNarrow(t *testing.T) {
	root := filepath.FromSlash(`C:/Users/dev/dev/ACME/docker`)
	names := []string{
		"consul", "jboss-amq", "jboss-be", "jboss-eap", "jboss-eap/modules",
		"jboss-esb", "jboss-fe", "keycloak", "app-proxy", "app-proxy/webapp",
		"webapp", "webapp-npm", "webapp-public", "zookeeper",
	}
	files := make([]string, len(names))
	for i, n := range names {
		files[i] = filepath.Join(root, filepath.FromSlash(n), "Dockerfile")
	}

	m := listModel{
		title:  "Seleziona Dockerfile",
		items:  dockerfileItems(files, root),
		cursor: len(files) - 1,
		width:  120,
	}

	var widths []int
	for _, line := range strings.Split(m.View().Content, "\n") {
		if w := lipgloss.Width(line); w > 0 {
			widths = append(widths, w)
		}
	}

	maxWidth := 0
	for _, w := range widths {
		if w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth > 45 {
		t.Errorf("lista larga %d colonne: le etichette non sono più relative alla root", maxWidth)
	}
}

// Every row of the box must share one width, or the border cannot close.
func TestListBoxBorderIsSquare(t *testing.T) {
	items := []Item{
		{Value: "a", Label: "corto"},
		{Value: "b", Label: "un-nome-molto-piu-lungo-degli-altri"},
		{Value: "c", Label: "medio"},
	}
	m := listModel{title: "Test", items: items, cursor: 1, width: 120}

	var boxWidths []int
	for _, line := range strings.Split(m.View().Content, "\n") {
		trimmed := strings.TrimSpace(stripANSI(line))
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "╭"), strings.HasPrefix(trimmed, "╰"), strings.HasPrefix(trimmed, "│"):
			boxWidths = append(boxWidths, lipgloss.Width(line))
		}
	}

	if len(boxWidths) != len(items)+2 {
		t.Fatalf("righe del box = %d, attese %d", len(boxWidths), len(items)+2)
	}
	for i, w := range boxWidths {
		if w != boxWidths[0] {
			t.Errorf("riga %d larga %d, la prima è %d: il bordo non chiude", i, w, boxWidths[0])
		}
	}
}

// stripANSI removes escape sequences so the leading rune can be inspected.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
