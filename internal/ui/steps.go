package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

type StepStatus int

const (
	StepPending StepStatus = iota
	StepActive
	StepDone
	StepError
)

type Step struct {
	Label  string
	Status StepStatus
}

// PrintStep prints a single step header (active style). Used before running a step.
func PrintStep(n, total int, label string) {
	fmt.Printf("\n%s\n",
		SectionStyle.Render(fmt.Sprintf("── [%d/%d] %s", n, total, strings.ToUpper(label))),
	)
}

// PrintOK prints a success line.
func PrintOK(msg string) {
	fmt.Println(SuccessStyle.Render("  ✓ " + msg))
}

// PrintWarn prints a warning line.
func PrintWarn(msg string) {
	fmt.Println(WarnStyle.Render("  ⚠ " + msg))
}

// PrintErr prints an error line.
func PrintErr(msg string) {
	fmt.Println(ErrStyle.Render("  ✗ " + msg))
}

// PrintInfo prints a dimmed info line.
func PrintInfo(msg string) {
	fmt.Println(DimStyle.Render("  · " + msg))
}

// PrintRunning prints an in-progress action line.
func PrintRunning(msg string) {
	fmt.Println(ValueStyle.Render("  ▶ " + msg))
}

// PrintDeployHeader prints a context box with service and tag transition before the deploy.
func PrintDeployHeader(service, oldTag, newTag string) {
	content := fmt.Sprintf("  %s    %s  →  %s  ",
		SelectedItemStyle.Render(service),
		DimStyle.Render(oldTag),
		SuccessStyle.Render(newTag),
	)
	fmt.Println("\n" + BoxStyle.Render(content))
}

// PrintDryRun prints a command that would be executed in dry-run mode.
func PrintDryRun(msg string) {
	fmt.Println(SecondaryStyle.Render("  ◆ DRY-RUN") + "  " + DimStyle.Render(msg))
}

// PrintWorkflowChecklist prints workflow progress as a checklist: the first
// doneCount steps are ✓, the next is ▸ (active), the rest are pending.
func PrintWorkflowChecklist(labels []string, doneCount int) {
	fmt.Println()
	for i, label := range labels {
		switch {
		case i < doneCount:
			fmt.Println(SuccessStyle.Render("  ✓  ") + DimStyle.Render(label))
		case i == doneCount:
			fmt.Println(CursorStyle.Render("  ▸  ") + ValueStyle.Render(strings.ToUpper(label)))
		default:
			fmt.Println(DimStyle.Render("     " + label))
		}
	}
	fmt.Println()
}

// RenderGradientSeparator returns a horizontal line shaded Accent→Secondary.
func RenderGradientSeparator(width int) string {
	colors := lipgloss.Blend1D(width, Accent, Secondary)
	var sb strings.Builder
	for _, c := range colors {
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render("─"))
	}
	return sb.String()
}

// DeployResult is the outcome of deploying a single service.
type DeployResult struct {
	Service string
	OldTag  string
	NewTag  string
	Elapsed time.Duration
	Skipped bool
}

// PrintDeploySummary prints the final summary card (three panels with gradient borders).
func PrintDeploySummary(service, oldTag, newTag string, elapsed time.Duration) {
	printDeploySummary(service, oldTag, newTag, elapsed, 0)
}

func printDeploySummary(service, oldTag, newTag string, elapsed time.Duration, fixedW int) {
	elapsedStr := "✓ " + formatElapsed(elapsed)
	// Target content width — usually driven by the service name.
	// len() overcounts by 2 bytes for "→" and "✓" (3-byte UTF-8, 1 visual col), acceptable.
	w := max(len(service), len(oldTag), len("→ "+newTag), len(elapsedStr), fixedW)

	// In lipgloss v2, Width() includes borders and padding.
	// Overhead: border left(1) + padding left(2) + padding right(2) + border right(1) = 6.
	panelW := w + 6

	panelColors := lipgloss.Blend1D(3, Accent, Secondary)
	makePanel := func(i int) lipgloss.Style {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(panelColors[i]).
			Padding(0, 2).
			Width(panelW)
	}

	// Each panel has exactly 4 content rows to keep a uniform height.
	svcPanel := makePanel(0).Render(
		DimStyle.Render("SERVIZIO") + "\n\n" +
			SelectedItemStyle.Render(service) + "\n",
	)
	tagPanel := makePanel(1).Render(
		DimStyle.Render("TAG") + "\n\n" +
			DimStyle.Render(oldTag) + "\n" +
			SuccessStyle.Render("→ "+newTag),
	)
	timePanel := makePanel(2).Render(
		DimStyle.Render("DURATA") + "\n\n" +
			SuccessStyle.Render(elapsedStr) + "\n",
	)

	fmt.Printf("\n%s\n", lipgloss.JoinHorizontal(lipgloss.Top, svcPanel, "  ", tagPanel, "  ", timePanel))
}

// PrintMultiDeploySummary prints one card per service deployed in sequence.
func PrintMultiDeploySummary(results []DeployResult) {
	succeeded := 0
	for _, r := range results {
		if !r.Skipped {
			succeeded++
		}
	}
	fmt.Printf("\n%s\n",
		SuccessStyle.Render(fmt.Sprintf("  ✓  %d / %d servizi deployati", succeeded, len(results))),
	)

	// Compute the global max width so every card lines up.
	globalW := 0
	for _, r := range results {
		if r.Skipped {
			continue
		}
		elapsedStr := "✓ " + formatElapsed(r.Elapsed)
		w := max(len(r.Service), len(r.OldTag), len("→ "+r.NewTag), len(elapsedStr))
		if w > globalW {
			globalW = w
		}
	}

	for _, r := range results {
		if r.Skipped {
			fmt.Printf("%s\n", WarnStyle.Render(fmt.Sprintf("  ⚡  %s  —  saltato", r.Service)))
		} else {
			printDeploySummary(r.Service, r.OldTag, r.NewTag, r.Elapsed, globalW)
		}
	}
}
