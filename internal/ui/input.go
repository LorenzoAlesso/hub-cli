package ui

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type inputModel struct {
	title     string
	textInput textinput.Model
	done      bool
	quit      bool
	width     int
}

func newInputModel(title, placeholder, defaultVal string) inputModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(defaultVal)
	ti.Focus()
	ti.CharLimit = 128
	ti.SetWidth(40)

	styles := textinput.DefaultDarkStyles()
	styles.Focused.Prompt = CursorStyle
	styles.Focused.Text = ValueStyle
	styles.Focused.Placeholder = DimStyle
	styles.Blurred.Prompt = CursorStyle
	styles.Blurred.Text = ValueStyle
	styles.Blurred.Placeholder = DimStyle
	ti.SetStyles(styles)

	return inputModel{title: title, textInput: ti}
}

func (m inputModel) Init() tea.Cmd { return nil }

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m inputModel) View() tea.View {
	if m.done {
		return tea.NewView(fmt.Sprintf("%s %s %s\n",
			LabelStyle.Render(m.title+":"),
			CursorStyle.Render("▸"),
			SelectedItemStyle.Render(m.textInput.Value()),
		))
	}
	w := m.width
	if w == 0 {
		w = 80
	}
	bar := renderStatusBar(w)
	content := fmt.Sprintf("%s\n%s\n%s",
		TitleStyle.Render(m.title),
		BoxStyle.Render(m.textInput.View()),
		HelpStyle.Render("enter conferma · esc annulla"),
	)
	if bar != "" {
		content += "\n" + bar
	}

	return tea.NewView(content)
}

// RunInput shows a text input and returns the entered value.
// Returns ("", true) if cancelled.
func RunInput(title, placeholder, defaultVal string) (string, bool) {
	m := newInputModel(title, placeholder, defaultVal)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return "", true
	}
	result := final.(inputModel)
	if result.quit || !result.done {
		return "", true
	}
	val := result.textInput.Value()
	if val == "" {
		val = defaultVal
	}
	return val, false
}
