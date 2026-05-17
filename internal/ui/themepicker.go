package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type themePickerModel struct {
	cursor int
	done   bool
	quit   bool
	width  int
}

func (m themePickerModel) Init() tea.Cmd { return nil }

func (m themePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(BuiltinThemes)-1 {
				m.cursor++
			}
		case "enter", " ":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m themePickerModel) View() tea.View {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(TitleStyle.Render("  Seleziona tema") + "\n\n")

	for i, t := range BuiltinThemes {
		if i == m.cursor {
			sb.WriteString(CursorStyle.Render("  ▸ ") + ValueStyle.Render(t.Name) + "\n")
		} else {
			sb.WriteString(DimStyle.Render("    "+t.Name) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(renderThemePreview(BuiltinThemes[m.cursor]))

	w := m.width
	if w == 0 {
		w = 80
	}
	sb.WriteString(HelpStyle.Render("  ↑↓ naviga · enter seleziona · esc annulla"))
	if bar := renderStatusBar(w); bar != "" {
		sb.WriteString("\n" + bar)
	}

	return tea.NewView(sb.String())
}

// renderThemePreview renders the theme's palette as colored swatches with usage labels.
func renderThemePreview(t Theme) string {
	type entry struct {
		hex   string
		label string
	}
	entries := []entry{
		{t.Accent, "Accent — prompt, cursore, link, tab attiva"},
		{t.Secondary, "Secondary — badge status bar, evidenziato"},
		{t.Success, "Success — operazioni completate ✓"},
		{t.Warning, "Warning — avvisi ⚠"},
		{t.Err, "Error — errori ✗"},
		{t.White, "Text — testo principale, valori"},
		{t.Muted, "Muted — info, help, testo secondario"},
	}

	var sb strings.Builder
	for _, e := range entries {
		c := lipgloss.Color(e.hex)
		swatch := lipgloss.NewStyle().Background(c).Padding(0, 2).Render("")
		label := lipgloss.NewStyle().Foreground(c).Render(e.label)
		sb.WriteString(fmt.Sprintf("  %s  %s\n", swatch, label))
	}
	sb.WriteString("\n")
	return sb.String()
}

// RunThemePicker shows an interactive picker with live preview. Returns (theme name, cancelled).
func RunThemePicker() (string, bool) {
	p := tea.NewProgram(themePickerModel{})
	final, err := p.Run()
	if err != nil {
		return "", true
	}
	result := final.(themePickerModel)
	if result.quit || !result.done {
		return "", true
	}
	return BuiltinThemes[result.cursor].Name, false
}
