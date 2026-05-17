package ui

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type spinnerDoneMsg struct {
	output []byte
	err    error
}

type spinnerTickMsg time.Time

type spinnerModel struct {
	spinner spinner.Model
	label   string
	start   time.Time
	done    bool
	output  []byte
	err     error
	width   int
}

func newSpinnerModel(label string) spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = CursorStyle
	return spinnerModel{
		spinner: s,
		label:   label,
		start:   time.Now(),
	}
}

func (m spinnerModel) Init() tea.Cmd {
	s := m.spinner
	return tea.Batch(func() tea.Msg { return s.Tick() }, spinnerTick())
}

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case spinnerDoneMsg:
		m.done = true
		m.output = msg.output
		m.err = msg.err
		return m, tea.Quit
	case spinnerTickMsg:
		return m, spinnerTick()
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m spinnerModel) View() tea.View {
	elapsed := formatElapsed(time.Since(m.start))

	if m.done {
		if m.err != nil {
			return tea.NewView(fmt.Sprintf("  %s  %s  %s\n",
				ErrStyle.Render("✗"),
				ValueStyle.Render(m.label),
				DimStyle.Render(elapsed),
			))
		}
		return tea.NewView(fmt.Sprintf("  %s  %s  %s\n",
			SuccessStyle.Render("✓"),
			ValueStyle.Render(m.label),
			DimStyle.Render(elapsed),
		))
	}

	w := m.width
	if w == 0 {
		w = 80
	}
	line := fmt.Sprintf("  %s  %s  %s",
		m.spinner.View(),
		ValueStyle.Render(m.label),
		DimStyle.Render(elapsed),
	)
	if bar := renderStatusBar(w); bar != "" {
		return tea.NewView(line + "\n" + bar)
	}
	return tea.NewView(line)
}

func formatElapsed(d time.Duration) string {
	d = d.Round(100 * time.Millisecond)
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", m, s)
}

// RunSpinner runs fn while showing an animated spinner with elapsed timer.
// fn is given an io.Writer for stdout/stderr; if it fails, the captured output
// is printed below the error.
func RunSpinner(label string, fn func(out io.Writer) error) error {
	var buf bytes.Buffer
	m := newSpinnerModel(label)
	p := tea.NewProgram(m)

	go func() {
		err := fn(&buf)
		p.Send(spinnerDoneMsg{output: buf.Bytes(), err: err})
	}()

	finalModel, runErr := p.Run()
	if runErr != nil {
		return runErr
	}

	result := finalModel.(spinnerModel)
	if result.err != nil {
		if len(result.output) > 0 {
			sep := lipgloss.NewStyle().Foreground(Muted).Render("  ── output ─────────────────────────────────────")
			fmt.Printf("\n%s\n%s\n", sep, string(result.output))
		}
		return result.err
	}
	return nil
}
