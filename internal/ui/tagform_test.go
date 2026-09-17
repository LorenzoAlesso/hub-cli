package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sampleTagForm() tagFormModel {
	return newTagForm([]tagFormRow{
		newTagFormRow("app-be", "3.0.11-dev", "3.0.12-dev", ""),
		newTagFormRow("app-esb", "3.0.11-dev", "3.0.12-dev", ""),
		newTagFormRow("app-webapp-public", "1.0.0", "1.0.2", "1.0.1"),
	}, 120)
}

func press(m tagFormModel, keys ...tea.KeyPressMsg) tagFormModel {
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(tagFormModel)
	}
	return m
}

var (
	keyDown     = tea.KeyPressMsg{Code: tea.KeyDown}
	keyUp       = tea.KeyPressMsg{Code: tea.KeyUp}
	keyTab      = tea.KeyPressMsg{Code: tea.KeyTab}
	keyShiftTab = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	keyEnter    = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc      = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyCtrlA    = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	keyBack     = tea.KeyPressMsg{Code: tea.KeyBackspace}
)

func typed(s string) []tea.KeyPressMsg {
	var keys []tea.KeyPressMsg
	for _, r := range s {
		keys = append(keys, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return keys
}

// The proposals are already in the fields: enter alone takes them all.
func TestTagFormStartsWithTheProposals(t *testing.T) {
	m := press(sampleTagForm(), keyEnter)
	if !m.done {
		t.Fatal("enter deve confermare")
	}
	want := []string{"3.0.12-dev", "3.0.12-dev", "1.0.2"}
	for i, got := range m.values() {
		if got != want[i] {
			t.Errorf("riga %d = %q, atteso %q", i, got, want[i])
		}
	}
}

// Arrows and tab move between services and stop at the ends; what is typed
// goes only to the service in hand.
func TestTagFormMovesBetweenServices(t *testing.T) {
	m := sampleTagForm()
	m = press(m, keyUp)
	if m.cursor != 0 {
		t.Errorf("oltre la prima riga: cursore %d", m.cursor)
	}
	m = press(m, keyDown, keyTab, keyDown)
	if m.cursor != 2 {
		t.Errorf("oltre l'ultima riga: cursore %d", m.cursor)
	}
	m = press(m, keyShiftTab)
	if m.cursor != 1 {
		t.Errorf("shift+tab: cursore %d, atteso 1", m.cursor)
	}

	keys := append(slicesRepeat(keyBack, 10), typed("3.1.0-dev")...)
	m = press(m, keys...)
	if got := m.values(); got[1] != "3.1.0-dev" || got[0] != "3.0.12-dev" || got[2] != "1.0.2" {
		t.Errorf("valori = %v: solo la riga 1 doveva cambiare", got)
	}
	for i, r := range m.rows {
		if r.input.Focused() != (i == m.cursor) {
			t.Errorf("riga %d: focus %v con cursore su %d", i, r.input.Focused(), m.cursor)
		}
	}
}

func slicesRepeat(k tea.KeyPressMsg, n int) []tea.KeyPressMsg {
	out := make([]tea.KeyPressMsg, n)
	for i := range out {
		out[i] = k
	}
	return out
}

// ctrl+a gives every service the tag of the row in hand.
func TestTagFormSameTagForAll(t *testing.T) {
	m := press(sampleTagForm(), keyDown)
	m = press(m, append(slicesRepeat(keyBack, 10), typed("4.0.0-dev")...)...)
	m = press(m, keyCtrlA)
	for i, got := range m.values() {
		if got != "4.0.0-dev" {
			t.Errorf("riga %d = %q dopo ctrl+a", i, got)
		}
	}
}

// An emptied field stands for the proposal, not for an empty tag.
func TestTagFormEmptyFieldMeansTheProposal(t *testing.T) {
	m := press(sampleTagForm(), slicesRepeat(keyBack, 10)...)
	if got := m.values()[0]; got != "3.0.12-dev" {
		t.Errorf("campo vuoto = %q, atteso la proposta", got)
	}
	m.rows[1].input.SetValue("   ")
	if got := m.values()[1]; got != "3.0.12-dev" {
		t.Errorf("campo di soli spazi = %q, atteso la proposta", got)
	}
}

func TestTagFormEscCancels(t *testing.T) {
	if m := press(sampleTagForm(), keyEsc); !m.quit || m.done {
		t.Errorf("esc: quit=%v done=%v", m.quit, m.done)
	}
}

// The hint follows what is typed: the running tag is an unchanged deploy, and
// a proposal that skipped a tag says which one ACR already holds.
func TestTagFormHints(t *testing.T) {
	m := sampleTagForm()
	view := stripANSI(m.View().Content)
	if strings.Contains(view, "invariato") {
		t.Errorf("nessun tag invariato all'inizio:\n%s", view)
	}
	if !strings.Contains(view, "su ACR: tag immagine 1.0.1 già presente") {
		t.Errorf("manca il suggerimento su ACR:\n%s", view)
	}

	m = press(m, keyDown)
	m = press(m, append(slicesRepeat(keyBack, 10), typed("3.0.11-dev")...)...)
	view = stripANSI(m.View().Content)
	if !strings.Contains(view, "invariato: riavvio a fine deploy") {
		t.Errorf("il tag in esecuzione va segnalato come invariato:\n%s", view)
	}
}

// Names, current tags and hints each keep one column, whatever their length.
func TestTagFormColumns(t *testing.T) {
	m := sampleTagForm()
	m.rows[1].input.SetValue("3.0.11-dev") // invariato: a hint on a second row
	var rows []string
	for _, l := range strings.Split(stripANSI(m.View().Content), "\n") {
		if strings.Contains(l, "→") {
			rows = append(rows, l)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("righe = %d, attese 3:\n%s", len(rows), stripANSI(m.View().Content))
	}
	if strings.Index(rows[0], "→") != strings.Index(rows[2], "→") {
		t.Errorf("frecce disallineate:\n%s\n%s", rows[0], rows[2])
	}
	if a, b := strings.Index(rows[1], "invariato"), strings.Index(rows[2], "su ACR"); a != b {
		t.Errorf("suggerimenti disallineati (%d, %d):\n%s\n%s", a, b, rows[1], rows[2])
	}
}

// One service asks the same way, without the multi-service help.
func TestTagFormSingleService(t *testing.T) {
	m := newTagForm([]tagFormRow{newTagFormRow("app-be", "1.0.0", "1.0.1", "")}, 100)
	view := stripANSI(m.View().Content)
	if strings.Contains(view, "servizi") || strings.Contains(view, "ctrl+a") {
		t.Errorf("con un servizio niente conteggio né ctrl+a:\n%s", view)
	}
	if !strings.Contains(view, "enter conferma · esc annulla") {
		t.Errorf("aiuto:\n%s", view)
	}
}
