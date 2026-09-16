package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// tagFormModel asks the tag of every selected service in one frame. Asked one
// at a time, each question waited for the build and push of the service before
// it, and a run of three services had to be watched from start to end.
type tagFormModel struct {
	title  string
	rows   []tagFormRow
	cursor int
	done   bool
	quit   bool
	width  int
}

type tagFormRow struct {
	name      string
	current   string // tag the cluster runs
	suggested string // what an empty field stands for
	skipped   string // tag already on ACR the proposal stepped past, "" if none
	input     textinput.Model
}

func newTagFormRow(name, current, suggested, skipped string) tagFormRow {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 128
	ti.SetWidth(24)
	ti.SetValue(suggested)
	ti.CursorEnd()

	styles := textinput.DefaultDarkStyles()
	styles.Focused.Text = ValueStyle
	styles.Focused.Placeholder = DimStyle
	styles.Blurred.Text = ValueStyle
	styles.Blurred.Placeholder = DimStyle
	ti.SetStyles(styles)

	return tagFormRow{name: name, current: current, suggested: suggested, skipped: skipped, input: ti}
}

func newTagForm(rows []tagFormRow, width int) tagFormModel {
	m := tagFormModel{title: "Tag immagine", rows: rows, width: width}
	m.focus(0)
	return m
}

// focus moves the cursor to row i: only the row in hand takes keystrokes.
func (m *tagFormModel) focus(i int) tea.Cmd {
	for j := range m.rows {
		m.rows[j].input.Blur()
	}
	m.cursor = i
	return m.rows[i].input.Focus()
}

// value is the tag of row i: what was typed, or the proposal for an empty field.
func (m tagFormModel) value(i int) string {
	if v := strings.TrimSpace(m.rows[i].input.Value()); v != "" {
		return v
	}
	return m.rows[i].suggested
}

func (m tagFormModel) values() []string {
	out := make([]string, len(m.rows))
	for i := range m.rows {
		out[i] = m.value(i)
	}
	return out
}

func (m tagFormModel) Init() tea.Cmd {
	return m.rows[m.cursor].input.Focus()
}

func (m tagFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, nil
		case "enter":
			m.done = true
			return m, nil
		case "up", "shift+tab":
			if m.cursor > 0 {
				return m, m.focus(m.cursor - 1)
			}
			return m, nil
		case "down", "tab":
			if m.cursor < len(m.rows)-1 {
				return m, m.focus(m.cursor + 1)
			}
			return m, nil
		case "ctrl+a":
			// Services released together usually go out under the same tag.
			v := m.value(m.cursor)
			for i := range m.rows {
				m.rows[i].input.SetValue(v)
				m.rows[i].input.CursorEnd()
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.rows[m.cursor].input, cmd = m.rows[m.cursor].input.Update(msg)
	return m, cmd
}

// hint says, next to a row, what its tag means before it is confirmed.
func (r tagFormRow) hint(tag string) string {
	switch {
	case tag == r.current:
		return WarnStyle.Render("invariato: riavvio a fine deploy")
	case r.skipped != "":
		return DimStyle.Render("su ACR: tag immagine " + r.skipped + " già presente")
	}
	return ""
}

// tagFieldMin keeps the hints still while a tag of ordinary length is typed.
const tagFieldMin = 11

func (m tagFormModel) View() tea.View {
	nameW, currentW, fieldW := 0, 0, tagFieldMin
	for i, r := range m.rows {
		nameW = max(nameW, lipgloss.Width(r.name))
		currentW = max(currentW, lipgloss.Width(r.current))
		fieldW = max(fieldW, lipgloss.Width(m.value(i))+1)
	}

	lines := make([]string, len(m.rows))
	for i, r := range m.rows {
		cursor, name := "  ", ValueStyle.Render(r.name)
		if i == m.cursor {
			cursor, name = CursorStyle.Render(">")+" ", SelectedItemStyle.Render(r.name)
		}
		// The current tag is filled exactly: pad keeps a space even at the widest,
		// which would push that row's arrow one column right of the others.
		line := cursor + name + pad(lipgloss.Width(r.name), nameW+2) +
			DimStyle.Render(r.current) + strings.Repeat(" ", currentW-lipgloss.Width(r.current)) +
			DimStyle.Render("  →  ")
		// The field is drawn as wide as the column, not as the input's own width:
		// the hints sit just past the longest tag.
		in := r.input
		in.SetWidth(fieldW)
		field := in.View()
		line += field
		if hint := r.hint(m.value(i)); hint != "" {
			line += pad(lipgloss.Width(field), fieldW+3) + hint
		}
		lines[i] = line
	}

	title := SelectedItemStyle.Render(m.title)
	help := "enter conferma · esc annulla"
	if len(m.rows) > 1 {
		title += DimStyle.Render(fmt.Sprintf("  %d servizi", len(m.rows)))
		help = "↑/↓ servizio · ctrl+a stesso tag per tutti · enter conferma tutti · esc annulla"
	}

	content := questionBox(title, CursorStyle, lines) + "\n" + HelpStyle.Render(help)
	w := m.width
	if w == 0 {
		w = 80
	}
	if bar := renderStatusBar(w); bar != "" {
		content += "\n" + bar
	}
	return tea.NewView(content)
}
