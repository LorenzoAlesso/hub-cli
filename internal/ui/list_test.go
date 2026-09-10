package ui

import (
	"fmt"
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

	// Only the box matters here: the help line below it has a fixed width of
	// its own and says nothing about the labels.
	maxWidth := 0
	for _, line := range strings.Split(m.View().Content, "\n") {
		trimmed := strings.TrimSpace(stripANSI(line))
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "╭"), strings.HasPrefix(trimmed, "╰"), strings.HasPrefix(trimmed, "│"):
			if w := lipgloss.Width(line); w > maxWidth {
				maxWidth = w
			}
		}
	}
	if maxWidth > 45 {
		t.Errorf("box largo %d colonne: le etichette non sono più relative alla root", maxWidth)
	}
}

// A list longer than the window must not draw every entry: a box taller than
// the terminal scrolls the frame away instead of showing it.
func TestLongListIsWindowed(t *testing.T) {
	items := make([]Item, 40)
	for i := range items {
		items[i] = Item{Value: fmt.Sprint(i), Label: fmt.Sprintf("branch-%02d", i)}
	}
	m := listModel{title: "Branch", items: items, cursor: 0, width: 120}

	rows := 0
	for _, line := range strings.Split(m.View().Content, "\n") {
		trimmed := strings.TrimSpace(stripANSI(line))
		// Only rows carrying an entry: the frame pads itself top and bottom.
		if strings.HasPrefix(trimmed, "│") && strings.Trim(trimmed, "│ ") != "" {
			rows++
		}
	}
	if rows > listWindow+2 { // window plus the two "altre N" hints
		t.Errorf("righe disegnate = %d, la finestra è di %d", rows, listWindow)
	}
}

// Typing narrows the list, which is the only way 57 branches stay usable.
func TestFilterNarrowsTheList(t *testing.T) {
	m := listModel{items: []Item{
		{Value: "a", Label: "site-a-pre-prod"},
		{Value: "b", Label: "site-a-prod"},
		{Value: "c", Label: "site-c-pre-prod"},
		{Value: "d", Label: "master"},
	}}

	m.filter = "site-a"
	if got := len(m.matches()); got != 2 {
		t.Errorf("voci filtrate = %d, attese 2", got)
	}

	m.filter = "PROD" // case-insensitive
	if got := len(m.matches()); got != 3 {
		t.Errorf("filtro case-insensitive: voci = %d, attese 3", got)
	}

	m.filter = "nulla"
	if got := len(m.matches()); got != 0 {
		t.Errorf("filtro senza riscontri: voci = %d, attese 0", got)
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

	// The entries, the two border lines, and the blank row the frame keeps above
	// and below them.
	if want := len(items) + 4; len(boxWidths) != want {
		t.Fatalf("righe del box = %d, attese %d", len(boxWidths), want)
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
