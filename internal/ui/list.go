package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Item is a list entry: Value is returned on selection, Desc is an optional secondary line.
type Item struct {
	Value string
	Label string
	Desc  string
}

type listModel struct {
	title    string
	items    []Item
	cursor   int
	selected string
	done     bool
	quit     bool
	width    int
}

func (m listModel) Init() tea.Cmd { return nil }

func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter", " ":
			m.selected = m.items[m.cursor].Value
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m listModel) View() tea.View {
	if m.done {
		label := m.items[m.cursor].Label
		return tea.NewView(fmt.Sprintf("  %s %s %s\n",
			LabelStyle.Render(m.title+":"),
			CursorStyle.Render("▸"),
			SelectedItemStyle.Render(label),
		))
	}

	// Compute the label column width so descriptions line up.
	maxLen := 0
	for _, item := range m.items {
		if len(item.Label) > maxLen {
			maxLen = len(item.Label)
		}
	}

	var sb strings.Builder
	sb.WriteString(TitleStyle.Render(m.title) + "\n\n")

	lines := make([]string, len(m.items))
	for i, item := range m.items {
		padding := strings.Repeat(" ", maxLen-len(item.Label)+2)
		var desc string
		if item.Desc != "" {
			desc = lipgloss.NewStyle().Foreground(Muted).Render("· " + item.Desc)
		}

		cur := " "
		if i == m.cursor {
			cur = CursorStyle.Render(">")
		}

		label := ItemStyle.Render(item.Label)
		if i == m.cursor {
			label = SelectedItemStyle.Render(item.Label)
		}

		lines[i] = fmt.Sprintf("  %s %s%s%s", cur, label, padding, desc)
	}

	// Normalize all line widths so the box border never shifts on hover.
	maxWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxWidth {
			maxWidth = w
		}
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w < maxWidth {
			lines[i] = line + strings.Repeat(" ", maxWidth-w)
		}
	}

	sb.WriteString(BoxStyle.Render(strings.Join(lines, "\n")))
	sb.WriteString("\n" + HelpStyle.Render("↑/↓ naviga · enter seleziona · esc annulla"))
	w := m.width
	if w == 0 {
		w = 80
	}
	if bar := renderStatusBar(w); bar != "" {
		sb.WriteString("\n" + bar)
	}
	return tea.NewView(sb.String())
}

// RunList shows a simple list and returns the selected value, or ("", true) if cancelled.
func RunList(title string, values []string) (string, bool) {
	items := make([]Item, len(values))
	for i, v := range values {
		items[i] = Item{Value: v, Label: v}
	}
	return RunListItems(title, items)
}

// RunListItems shows a list with optional descriptions. Returns (selected Item.Value, cancelled).
func RunListItems(title string, items []Item) (string, bool) {
	m := listModel{title: title, items: items}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return "", true
	}
	result := final.(listModel)
	if result.quit || !result.done {
		return "", true
	}
	return result.selected, false
}
