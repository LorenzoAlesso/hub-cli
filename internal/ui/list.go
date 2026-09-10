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

	// errorTone frames the list in the error colour: the same question shape,
	// asked because something broke rather than because the workflow reached a
	// fork.
	errorTone bool

	// quiet drops the echo of the answer, for a choice that whatever prints next
	// restates in full.
	quiet bool
}

// borderStyle is the colour of the frame, which says who is asking.
func (m listModel) borderStyle() lipgloss.Style {
	if m.errorTone {
		return ErrStyle
	}
	return CursorStyle
}

// itemLines renders the entries of a list. The description sits on the same row
// as its label when the pair fits the terminal, and drops to its own indented
// row when it does not — a decision with a long explanation would otherwise
// push the frame off screen.
func itemLines(items []Item, cursor, offset, width int) []string {
	labelW, descW := 0, 0
	for _, it := range items {
		labelW = max(labelW, lipgloss.Width(it.Label))
		descW = max(descW, lipgloss.Width(it.Desc))
	}

	limit := width - 10
	if limit <= 0 || limit > 96 {
		limit = 96
	}
	inline := descW == 0 || 6+labelW+descW <= limit

	var lines []string
	for i, it := range items {
		cur := " "
		label := ItemStyle.Render(it.Label)
		if offset+i == cursor {
			cur = CursorStyle.Render(">")
			label = SelectedItemStyle.Render(it.Label)
		}

		if it.Desc == "" {
			lines = append(lines, fmt.Sprintf("%s %s", cur, label))
			continue
		}
		if inline {
			gap := strings.Repeat(" ", labelW-lipgloss.Width(it.Label)+2)
			lines = append(lines, fmt.Sprintf("%s %s%s%s", cur, label, gap, DimStyle.Render(it.Desc)))
			continue
		}
		lines = append(lines,
			fmt.Sprintf("%s %s", cur, label),
			"    "+DimStyle.Render(it.Desc))
	}
	return lines
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
		if m.quiet {
			return tea.NewView("")
		}
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

	title := SelectedItemStyle.Render(m.title)
	if m.filter != "" {
		title += DimStyle.Render(fmt.Sprintf("  (filtro: %s — %d di %d)",
			m.filter, len(visible), len(m.items)))
	}

	var sb strings.Builder

	if len(visible) == 0 {
		sb.WriteString(questionBox(title, m.borderStyle(),
			[]string{WarnStyle.Render("nessuna voce per " + m.filter)}))
		sb.WriteString("\n" + HelpStyle.Render("digita per filtrare · backspace cancella · esc azzera il filtro"))
		return tea.NewView(sb.String())
	}

	start, end, above, below := listSlice(len(visible), m.cursor)
	lines := itemLines(visible[start:end], m.cursor, start, m.width)

	if above > 0 {
		lines = append([]string{DimStyle.Render(fmt.Sprintf("  ↑ altre %d", above))}, lines...)
	}
	if below > 0 {
		lines = append(lines, DimStyle.Render(fmt.Sprintf("  ↓ altre %d", below)))
	}

	sb.WriteString(questionBox(title, m.borderStyle(), lines))
	// The filter is only worth announcing on a list long enough to need it: on a
	// two-way decision it reads as an option the question does not have.
	help := "↑/↓ naviga · enter seleziona · esc annulla"
	if len(m.items) > listWindow/2 {
		help = "↑/↓ naviga · digita per filtrare · enter seleziona · esc annulla"
	}
	sb.WriteString("\n" + HelpStyle.Render(help))
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
	return runList(listModel{title: title, items: items})
}

// RunListItemsQuiet is RunListItems for a choice that whatever prints next
// restates: the echo of the answer would say the same thing twice.
func RunListItemsQuiet(title string, items []Item) (string, bool) {
	return runList(listModel{title: title, items: items, quiet: true})
}

func runList(m listModel) (string, bool) {
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
