package ui

import (
	"fmt"
	"path/filepath"
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

// dockerfileItems labels absolute Dockerfile paths relative to root: the prefix
// is the same on every row and only widens the box. Value keeps the absolute
// path, which is what the build needs.
func dockerfileItems(files []string, root string) []Item {
	items := make([]Item, len(files))
	for i, f := range files {
		label := f
		if root != "" {
			if rel, err := filepath.Rel(root, f); err == nil && !strings.HasPrefix(rel, "..") {
				label = rel
			}
		}
		items[i] = Item{Value: f, Label: label}
	}
	return items
}

type listModel struct {
	title    string
	items    []Item
	cursor   int
	selected string
	done     bool
	quit     bool
	width    int
	filter   string
}

// listWindow caps how many entries are drawn at once. Some lists are long — a
// repo can publish dozens of branches — and a box taller than the terminal
// scrolls the frame away instead of showing it.
const listWindow = 12

func (m listModel) Init() tea.Cmd { return nil }

// matches returns the entries left after the filter, and is the list the cursor
// indexes into.
func (m listModel) matches() []Item {
	if m.filter == "" {
		return m.items
	}
	needle := strings.ToLower(m.filter)
	var out []Item
	for _, item := range m.items {
		if strings.Contains(strings.ToLower(item.Label), needle) {
			out = append(out, item)
		}
	}
	return out
}

func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "esc":
			// Clear the filter first: escaping a typo should not throw away
			// the whole selection.
			if m.filter != "" {
				m.filter = ""
				m.cursor = 0
				return m, nil
			}
			m.quit = true
			return m, tea.Quit
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.matches())-1 {
				m.cursor++
			}
		case "backspace":
			if m.filter != "" {
				m.filter = m.filter[:len(m.filter)-1]
				m.cursor = 0
			}
		case "enter":
			visible := m.matches()
			if len(visible) == 0 {
				return m, nil
			}
			m.selected = visible[m.cursor].Value
			m.done = true
			return m, tea.Quit
		default:
			// Anything printable narrows the list. Long lists are unusable
			// otherwise, and typing is faster than scrolling on short ones too.
			if r := []rune(key); len(r) == 1 && r[0] >= ' ' {
				m.filter += key
				m.cursor = 0
			}
		}
	}
	return m, nil
}

// listSlice returns the window of entries to draw around the cursor, plus how
// many are hidden above and below.
func listSlice(count, cursor int) (start, end, above, below int) {
	if count <= listWindow {
		return 0, count, 0, 0
	}
	start = cursor - listWindow/2
	if start < 0 {
		start = 0
	}
	if start+listWindow > count {
		start = count - listWindow
	}
	end = start + listWindow
	return start, end, start, count - end
}

func (m listModel) View() tea.View {
	visible := m.matches()

	if m.done {
		label := ""
		if m.cursor < len(visible) {
			label = visible[m.cursor].Label
		}
		return tea.NewView(fmt.Sprintf("  %s %s %s\n",
			LabelStyle.Render(m.title+":"),
			CursorStyle.Render("▸"),
			SelectedItemStyle.Render(label),
		))
	}

	title := TitleStyle.Render(m.title)
	if m.filter != "" {
		title += DimStyle.Render(fmt.Sprintf("  (filtro: %s — %d di %d)",
			m.filter, len(visible), len(m.items)))
	}

	var sb strings.Builder
	sb.WriteString(title + "\n\n")

	if len(visible) == 0 {
		sb.WriteString(BoxStyle.Render(WarnStyle.Render("nessuna voce per " + m.filter)))
		sb.WriteString("\n" + HelpStyle.Render("digita per filtrare · backspace cancella · esc azzera il filtro"))
		return tea.NewView(sb.String())
	}

	start, end, above, below := listSlice(len(visible), m.cursor)
	window := visible[start:end]

	// Compute the label column width so descriptions line up.
	maxLen := 0
	for _, item := range window {
		if len(item.Label) > maxLen {
			maxLen = len(item.Label)
		}
	}

	lines := make([]string, len(window))
	for i, item := range window {
		padding := strings.Repeat(" ", maxLen-len(item.Label)+2)
		var desc string
		if item.Desc != "" {
			desc = lipgloss.NewStyle().Foreground(Muted).Render("· " + item.Desc)
		}

		cur := " "
		if start+i == m.cursor {
			cur = CursorStyle.Render(">")
		}

		label := ItemStyle.Render(item.Label)
		if start+i == m.cursor {
			label = SelectedItemStyle.Render(item.Label)
		}

		lines[i] = fmt.Sprintf("  %s %s%s%s", cur, label, padding, desc)
	}

	if above > 0 {
		lines = append([]string{DimStyle.Render(fmt.Sprintf("    ↑ altre %d", above))}, lines...)
	}
	if below > 0 {
		lines = append(lines, DimStyle.Render(fmt.Sprintf("    ↓ altre %d", below)))
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
	sb.WriteString("\n" + HelpStyle.Render("↑/↓ naviga · digita per filtrare · enter seleziona · esc annulla"))
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
