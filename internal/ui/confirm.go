package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type confirmModel struct {
	title  string
	body   string
	choice int // 0 = yes, 1 = no
	done   bool
	quit   bool
	width  int
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch strings.ToLower(msg.String()) {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "left", "h", "shift+tab":
			m.choice = 0
		case "right", "l", "tab":
			m.choice = 1
		case "y":
			m.choice = 0
			m.done = true
			return m, tea.Quit
		case "n":
			m.choice = 1
			m.done = true
			return m, tea.Quit
		case "enter", " ":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func confirmButton(label string, active bool) string {
	if active {
		return lipgloss.NewStyle().
			Padding(0, 3).
			Background(Accent).
			Foreground(lipgloss.Color("#0d0d0d")).
			Bold(true).
			Render(label)
	}
	return lipgloss.NewStyle().
		Padding(0, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Muted).
		Foreground(Muted).
		Render(label)
}

func (m confirmModel) View() tea.View {
	if m.done {
		ans := SuccessStyle.Render("sì")
		if m.choice == 1 {
			ans = ErrStyle.Render("no")
		}
		return tea.NewView(fmt.Sprintf("  %s %s %s\n",
			LabelStyle.Render(m.title+":"), CursorStyle.Render("▸"), ans))
	}
	if m.quit {
		return tea.NewView("")
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(WarnStyle.Render("  ⚠  "+m.title) + "\n\n")
	if m.body != "" {
		sb.WriteString(BoxStyle.Render(m.body) + "\n\n")
	}

	btnSi := confirmButton("  Sì  ", m.choice == 0)
	btnNo := confirmButton("  No  ", m.choice == 1)
	sb.WriteString("  " + lipgloss.JoinHorizontal(lipgloss.Center, btnSi, "   ", btnNo) + "\n\n")
	sb.WriteString(HelpStyle.Render("  ← → naviga · enter conferma · esc annulla"))

	w := m.width
	if w == 0 {
		w = 80
	}
	if bar := renderStatusBar(w); bar != "" {
		sb.WriteString("\n" + bar)
	}

	return tea.NewView(sb.String())
}

// RunConfirm shows a confirmation dialog with navigable buttons.
// Returns (confirmed bool, cancelled bool).
func RunConfirm(title, body string) (bool, bool) {
	m := confirmModel{title: title, body: body}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return false, true
	}
	result := final.(confirmModel)
	if result.quit {
		return false, true
	}
	return result.choice == 0, false
}

// RunTagSyncConfirm is the specialised popup for tag synchronisation.
func RunTagSyncConfirm(serviceName, deployed, stored string) (bool, bool) {
	body := fmt.Sprintf("  %s  %s\n  %s  %s",
		LabelStyle.Render("Deployato: "),
		SuccessStyle.Render(deployed),
		LabelStyle.Render("Salvato:   "),
		WarnStyle.Render(stored),
	)
	return RunConfirm(
		fmt.Sprintf("Tag sfasato per %q — aggiornare?", serviceName),
		body,
	)
}
