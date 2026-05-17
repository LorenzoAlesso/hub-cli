package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type multiSelectModel struct {
	title    string
	items    []Item
	cursor   int
	selected map[int]bool
	done     bool
	quit     bool
	width    int
}

func (m multiSelectModel) Init() tea.Cmd { return nil }

func (m multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "i", " ", "space":
			if m.selected[m.cursor] {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = true
			}
		case "a":
			if len(m.selected) == len(m.items) {
				m.selected = make(map[int]bool)
			} else {
				for i := range m.items {
					m.selected[i] = true
				}
			}
		case "enter":
			if len(m.selected) > 0 {
				m.done = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m multiSelectModel) View() tea.View {
	if m.done {
		names := make([]string, 0, len(m.selected))
		for i, item := range m.items {
			if m.selected[i] {
				names = append(names, item.Label)
			}
		}
		return tea.NewView(fmt.Sprintf("  %s %s %s\n",
			LabelStyle.Render(m.title+":"),
			CursorStyle.Render("▸"),
			SelectedItemStyle.Render(strings.Join(names, ", ")),
		))
	}

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

		dot := DimStyle.Render("○")
		if m.selected[i] {
			dot = SuccessStyle.Render("●")
		}

		label := ItemStyle.Render(item.Label)
		if i == m.cursor {
			label = SelectedItemStyle.Render(item.Label)
		}

		lines[i] = fmt.Sprintf("  %s %s %s%s%s", cur, dot, label, padding, desc)
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

	count := len(m.selected)
	hint := HelpStyle.Render("↑/↓ naviga · i seleziona · a tutto · enter conferma · esc annulla")
	if count > 0 {
		countStr := SuccessStyle.Render(fmt.Sprintf("  %d selezionati", count))
		sb.WriteString("\n" + hint + countStr)
	} else {
		sb.WriteString("\n" + hint)
	}
	w := m.width
	if w == 0 {
		w = 80
	}
	if bar := renderStatusBar(w); bar != "" {
		sb.WriteString("\n" + bar)
	}
	return tea.NewView(sb.String())
}

// RunMultiSelect shows a multi-select list. Returns selected Item.Value slice and cancelled bool.
// At least one item must be selected before Enter is accepted.
func RunMultiSelect(title string, items []Item) ([]string, bool) {
	m := multiSelectModel{
		title:    title,
		items:    items,
		selected: make(map[int]bool),
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, true
	}
	result := final.(multiSelectModel)
	if result.quit || !result.done {
		return nil, true
	}
	selected := make([]string, 0, len(result.selected))
	for i, item := range result.items {
		if result.selected[i] {
			selected = append(selected, item.Value)
		}
	}
	return selected, false
}
