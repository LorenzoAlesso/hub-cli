package ui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type textAreaModel struct {
	title    string
	textarea textarea.Model
	done     bool
	quit     bool
	width    int
}

func newTextAreaModel(title, placeholder string) textAreaModel {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.SetWidth(72)
	ta.SetHeight(12)
	ta.CharLimit = 8192
	ta.Focus()

	s := textarea.DefaultDarkStyles()
	s.Focused.Base = s.Focused.Base.BorderForeground(Muted)
	s.Focused.Text = ValueStyle
	s.Focused.Placeholder = DimStyle
	ta.SetStyles(s)

	return textAreaModel{title: title, textarea: ta}
}

func (m textAreaModel) Init() tea.Cmd {
	return m.textarea.Focus()
}

func (m textAreaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		w := msg.Width - 8
		if w < 40 {
			w = 40
		}
		m.textarea.SetWidth(w)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "ctrl+s":
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m textAreaModel) View() tea.View {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(TitleStyle.Render("  "+m.title) + "\n\n")
	sb.WriteString(m.textarea.View() + "\n")
	sb.WriteString(HelpStyle.Render("  ctrl+s conferma · esc annulla"))

	w := m.width
	if w == 0 {
		w = 80
	}
	if bar := renderStatusBar(w); bar != "" {
		sb.WriteString("\n" + bar)
	}
	return tea.NewView(sb.String())
}

// RunTextArea shows a multi-line editor and returns the entered text.
// Confirm with Ctrl+S, cancel with Esc.
func RunTextArea(title, placeholder string) (string, bool) {
	m := newTextAreaModel(title, placeholder)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return "", true
	}
	result := final.(textAreaModel)
	if result.quit || !result.done {
		return "", true
	}
	return result.textarea.Value(), false
}
