package cmd

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	"Hub-cli/internal/ui"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// config show prints the paths both workflows share across the top, then the
// local and the PSN configuration side by side, divided by a rule, so the two
// environments read in parallel. A terminal too narrow for both, or an output
// that is not a terminal, gets them one under the other.

const (
	topLabelWidth     = 22 // "Branch chart (locale):"
	itemLabelWidth    = 15 // "Resource group:"
	releaseLabelWidth = 13 // "Branch build:"
	columnGap         = 3  // spaces on each side of the rule
)

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Mostra la configurazione corrente",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		reposRoot, _ := logic.ResolveReposRoot(cfg.Config.ReposRoot)
		fmt.Println(joinLines(generalLines(cfg, reposRoot)))
		fmt.Println()
		fmt.Println(showColumns(localLines(cfg), psnLines(cfg.PSN), terminalWidth()))
		return nil
	},
}

func generalLines(cfg *config.Config, reposRoot string) []string {
	theme := cfg.Config.Theme
	if theme == "" {
		theme = "auto"
	}
	return []string{
		ui.SectionStyle.Render("Percorsi"),
		topLine("Config:", ui.ValueStyle.Render(config.GetFilePath())),
		topLine("Seed:", orNA(config.SeedFilePath())),
		topLine("Repo gestiti:", orNA(reposRoot)),
		topLine("Docker Root:", orNA(cfg.Config.DockerRootPath)),
		topLine("Helm Root:", orNA(cfg.Config.HelmRootPath)),
		topLine("Tema:", ui.ValueStyle.Render(theme)),
	}
}

func localLines(cfg *config.Config) []string {
	lines := []string{
		columnHeading("Locale"),
		"",
		topLine("ECR Region:", ui.ValueStyle.Render(cfg.Config.ECRRegion)),
		topLine("ECR Account:", ui.ValueStyle.Render(cfg.Config.ECRAccountID)),
		topLine("Chart Version:", ui.ValueStyle.Render(cfg.Config.ChartVersion)),
		// Named apart from the per-release branch of the PSN column.
		topLine("Branch chart (locale):", orNA(config.GetHelmSyncBranch())),
	}
	if len(cfg.Services) == 0 {
		return append(lines, "", "  "+ui.WarnStyle.Render("Nessun servizio configurato."))
	}

	// Map order changes on every run: sorted, a service is where it was last time.
	for _, name := range sortedKeys(cfg.Services) {
		svc := cfg.Services[name]
		lines = append(lines, "", "  "+ui.SelectedItemStyle.Render("["+name+"]"),
			itemLine("Last Tag:", orNA(svc.LastTag)),
			itemLine("Dockerfile:", orNA(svc.DockerfileSubpath)),
			itemLine("Helm Values:", orNA(svc.HelmValuesPath)),
			itemLine("Namespace:", orNA(svc.Namespace)),
			itemLine("Chart:", orNA(svc.ChartName)),
			itemLine("Release:", orNA(svc.ReleaseName)),
			itemLine("ECR Repo:", orNA(svc.ECRRepository)))
		if svc.HelmImagePath != "" {
			lines = append(lines, itemLine("Helm Image:", ui.ValueStyle.Render(svc.HelmImagePath)))
		}
		if svc.K8sManifestPath != "" {
			lines = append(lines,
				itemLine("K8s Manifest:", ui.ValueStyle.Render(svc.K8sManifestPath)),
				itemLine("K8s Image Ref:", ui.ValueStyle.Render(svc.K8sImageRef)))
		}
	}
	return lines
}

// psnLines shows the psn block the way a run reads it: per cluster, each
// release with the branch its chart comes from and the project — with the
// branch it builds from — that its namespace resolves to. Clusters and projects
// keep the configured order: a namespace takes the first project that matches.
func psnLines(psn config.PSNConfig) []string {
	lines := []string{columnHeading("PSN"), ""}
	if len(psn.Clusters) == 0 {
		return append(lines, "  "+ui.DimStyle.Render("Blocco psn non configurato: hub-cli psn non è disponibile."))
	}
	lines = append(lines, topLine("Tenant:", orNA(psn.TenantID)))

	for _, c := range psn.Clusters {
		lines = append(lines, "",
			"  "+ui.SelectedItemStyle.Render("["+c.Name+"]")+"  "+ui.DimStyle.Render(psnEnvLabel(c)),
			itemLine("AKS:", orNA(c.AKSName)),
			itemLine("ACR:", orNA(c.ACRName)),
			itemLine("Resource group:", orNA(c.ResourceGroup)),
			itemLine("Subscription:", orNA(c.SubscriptionID)))

		if len(c.Releases) == 0 {
			lines = append(lines, itemLine("Release:", ui.WarnStyle.Render("nessuno: il cluster non si può deployare")))
			continue
		}
		for _, r := range c.Releases {
			lines = append(lines,
				itemLine("Release:", orNA(r.Name)),
				releaseLine("Namespace:", orNA(r.Namespace)),
				releaseLine("Chart:", orNA(r.Chart)),
				releaseLine("Values:", orNA(r.Values)),
				releaseLine("Branch chart:", orNA(r.ChartsBranch)))
			lines = append(lines, projectLines(psn, c, r)...)
		}
	}

	if len(psn.Projects) > 0 {
		lines = append(lines, "", columnHeading("PSN · Progetti"))
		for _, p := range psn.Projects {
			lines = append(lines, "", "  "+ui.SelectedItemStyle.Render("["+p.Namespace+"]"),
				itemLine("Docker root:", orNA(p.DockerRoot)),
				itemLine("Branch coll:", orNA(p.BranchColl)),
				itemLine("Branch prod:", orNA(p.BranchProd)))
			for _, dep := range slices.Sorted(maps.Keys(p.Deployments)) {
				lines = append(lines, itemLine("Dockerfile:", ui.ValueStyle.Render(dep+" → "+p.Deployments[dep])))
			}
		}
	}

	if len(psn.Deployments) > 0 {
		lines = append(lines, "", columnHeading("PSN · Mapping deployment → servizio locale"), "")
		for _, dep := range slices.Sorted(maps.Keys(psn.Deployments)) {
			lines = append(lines, itemLine(dep, ui.ValueStyle.Render(psn.Deployments[dep])))
		}
	}
	return lines
}

// projectLines says where a release is built from: the project its namespace
// resolves to, and the branch that project builds from on this cluster.
func projectLines(psn config.PSNConfig, c config.PSNClusterConfig, r config.PSNReleaseConfig) []string {
	project := psn.ProjectForNamespace(r.Namespace)
	if project == nil {
		return []string{releaseLine("Progetto:", ui.DimStyle.Render("nessuno — docker_root_path e psn.deployments"))}
	}
	lines := []string{releaseLine("Progetto:", orNA(project.DockerRoot))}
	if branch := project.ExpectedBranch(c); branch != "" {
		return append(lines, releaseLine("Branch build:", ui.ValueStyle.Render(branch)))
	}
	return append(lines, releaseLine("Branch build:", ui.DimStyle.Render("nessuno — si builda dalla copia di lavoro")))
}

// showColumns sets the two blocks side by side, divided by a rule, when the
// width has room for both; otherwise one under the other, a plain list that
// also reads well in a file.
func showColumns(left, right []string, width int) string {
	lw, rw := blockWidth(left), blockWidth(right)
	// Strictly narrower: a line that fills the last column wraps on some terminals.
	if width <= 0 || lw+2*columnGap+1+rw >= width {
		return joinLines(left) + "\n\n" + joinLines(right)
	}

	rule := lipgloss.NewStyle().PaddingLeft(columnGap).PaddingRight(columnGap).
		Render(ui.RenderGradientRule(max(len(left), len(right))))
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(lw).Render(joinLines(left)), rule, joinLines(right))

	rows := strings.Split(body, "\n")
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}
	return joinLines(rows)
}

// terminalWidth is the width of the terminal stdout writes to, or 0 when it is
// not one: redirected, the output has no width to fill.
func terminalWidth() int {
	fd := os.Stdout.Fd()
	if !term.IsTerminal(fd) {
		return 0
	}
	width, _, err := term.GetSize(fd)
	if err != nil {
		return 0
	}
	return width
}

// columnHeading is a section title inside a column: the section style without
// its top margin, which inside a column would be one more blank row to align.
func columnHeading(title string) string {
	return ui.SectionStyle.MarginTop(0).Render(title)
}

// topLine, itemLine and releaseLine are the three depths of the listing, each
// with its values in one column.
func topLine(label, value string) string {
	return "  " + ui.LabelStyle.Render(fmt.Sprintf("%-*s", topLabelWidth, label)) + "  " + value
}

func itemLine(label, value string) string {
	return "    " + ui.LabelStyle.Render(fmt.Sprintf("%-*s", itemLabelWidth, label)) + "  " + value
}

func releaseLine(label, value string) string {
	return "      " + ui.LabelStyle.Render(fmt.Sprintf("%-*s", releaseLabelWidth, label)) + "  " + value
}

func blockWidth(lines []string) int {
	width := 0
	for _, l := range lines {
		width = max(width, lipgloss.Width(l))
	}
	return width
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
