package ui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// capture runs fn with stdout redirected, so the printers that write straight to
// the terminal can be inspected.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The run header is two lines and a gap. A style with a top margin, rendered
// piecewise, would silently break one of them in two.
func TestRunHeaderIsTwoLines(t *testing.T) {
	out := capture(t, func() {
		PrintRunHeader("Ambiente PSN: ", "Cluster A — Collaudo", "app-coll",
			[]string{"app-site-a-coll", "chart app", "dev-site-a", "site-a-pre-prod"})
	})

	// Only the final newline is dropped: the blank line after the header is part
	// of what is being asserted.
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 || strings.TrimSpace(lines[2]) != "" {
		t.Fatalf("testata di %d righe, attese 2 più una vuota:\n%s", len(lines), stripANSI(out))
	}
	if w := lipgloss.Width(lines[0]); w != logWidth {
		t.Errorf("prima riga larga %d invece di %d: %s", w, logWidth, stripANSI(lines[0]))
	}
	for _, want := range []string{"Ambiente PSN: Cluster A — Collaudo", "namespace app-coll"} {
		if !strings.Contains(stripANSI(lines[0]), want) {
			t.Errorf("la prima riga non contiene %q:\n%s", want, stripANSI(lines[0]))
		}
	}
	if got := stripANSI(lines[1]); !strings.HasPrefix(got, "  app-site-a-coll  ·  chart app") {
		t.Errorf("seconda riga inattesa: %q", got)
	}
}

// Without a namespace the header is still one line, not one line with a gap
// waiting for something that never comes.
func TestRunHeaderWithoutNamespace(t *testing.T) {
	out := capture(t, func() {
		PrintRunHeader("Ambiente: ", "Locale", "", []string{"eu-west-1"})
	})
	first := strings.Split(out, "\n")[0]
	if strings.HasSuffix(stripANSI(first), " ") {
		t.Errorf("spazio in coda senza namespace: %q", stripANSI(first))
	}
}
