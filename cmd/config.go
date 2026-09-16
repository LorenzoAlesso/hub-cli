package cmd

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	"Hub-cli/internal/ui"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Gestione configurazione hub-cli",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Mostra la configurazione corrente",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		reposRoot, _ := logic.ResolveReposRoot(cfg.Config.ReposRoot)

		fmt.Println(ui.SectionStyle.Render("Percorsi"))
		printTopLine("Config:", ui.ValueStyle.Render(config.GetFilePath()))
		printTopLine("Seed:", orNA(config.SeedFilePath()))
		printTopLine("Repo gestiti:", orNA(reposRoot))
		printTopLine("Docker Root:", orNA(cfg.Config.DockerRootPath))
		printTopLine("Helm Root:", orNA(cfg.Config.HelmRootPath))

		fmt.Println(ui.SectionStyle.Render("Configurazione Globale"))
		printTopLine("ECR Region:", ui.ValueStyle.Render(cfg.Config.ECRRegion))
		printTopLine("ECR Account:", ui.ValueStyle.Render(cfg.Config.ECRAccountID))
		printTopLine("Chart Version:", ui.ValueStyle.Render(cfg.Config.ChartVersion))
		themeVal := cfg.Config.Theme
		if themeVal == "" {
			themeVal = "auto"
		}
		printTopLine("Tema:", ui.ValueStyle.Render(themeVal))
		// Named apart from the per-release branch of the PSN section below.
		printTopLine("Branch chart (locale):", orNA(config.GetHelmSyncBranch()))

		printLocalServices(cfg.Services)
		printPSNConfig(cfg.PSN)
		return nil
	},
}

// printTopLine is a line of the top-level blocks, its value in the column the
// longest of their labels needs.
func printTopLine(label, value string) {
	fmt.Printf("  %s  %s\n", ui.LabelStyle.Render(fmt.Sprintf("%-22s", label)), value)
}

func printLocalServices(services map[string]config.ServiceConfig) {
	if len(services) == 0 {
		fmt.Println(ui.WarnStyle.Render("\nNessun servizio configurato."))
		return
	}

	fmt.Println(ui.SectionStyle.Render("\nServizi"))
	// Map order changes on every run: sorted, a service is where it was last time.
	for _, name := range sortedKeys(services) {
		svc := services[name]
		fmt.Printf("\n  %s\n", ui.SelectedItemStyle.Render("["+name+"]"))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Last Tag:      "), orNA(svc.LastTag))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Dockerfile:    "), orNA(svc.DockerfileSubpath))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Helm Values:   "), orNA(svc.HelmValuesPath))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Namespace:     "), orNA(svc.Namespace))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Chart:         "), orNA(svc.ChartName))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Release:       "), orNA(svc.ReleaseName))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("ECR Repo:      "), orNA(svc.ECRRepository))
		if svc.HelmImagePath != "" {
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Helm Image:    "), ui.ValueStyle.Render(svc.HelmImagePath))
		}
		if svc.K8sManifestPath != "" {
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("K8s Manifest:  "), ui.ValueStyle.Render(svc.K8sManifestPath))
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("K8s Image Ref: "), ui.ValueStyle.Render(svc.K8sImageRef))
		}
	}
}

// printPSNConfig shows the psn block the way a run reads it: per cluster, each
// release with the branch its chart comes from and the project — with the
// branch it builds from — that its namespace resolves to.
func printPSNConfig(psn config.PSNConfig) {
	fmt.Println(ui.SectionStyle.Render("PSN"))
	if len(psn.Clusters) == 0 {
		fmt.Println("  " + ui.DimStyle.Render("Blocco psn non configurato: hub-cli psn non è disponibile."))
		return
	}
	printTopLine("Tenant:", orNA(psn.TenantID))

	for _, c := range psn.Clusters {
		fmt.Printf("\n  %s  %s\n", ui.SelectedItemStyle.Render("["+c.Name+"]"), ui.DimStyle.Render(psnEnvLabel(c)))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("AKS:           "), orNA(c.AKSName))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("ACR:           "), orNA(c.ACRName))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Resource group:"), orNA(c.ResourceGroup))
		fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Subscription:  "), orNA(c.SubscriptionID))

		if len(c.Releases) == 0 {
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Release:       "),
				ui.WarnStyle.Render("nessuno: il cluster non si può deployare"))
			continue
		}
		for _, r := range c.Releases {
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Release:       "), orNA(r.Name))
			fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Namespace:   "), orNA(r.Namespace))
			fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Chart:       "), orNA(r.Chart))
			fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Values:      "), orNA(r.Values))
			fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Branch chart:"), orNA(r.ChartsBranch))

			project := psn.ProjectForNamespace(r.Namespace)
			if project == nil {
				fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Progetto:    "),
					ui.DimStyle.Render("nessuno — docker_root_path e psn.deployments"))
				continue
			}
			fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Progetto:    "), orNA(project.DockerRoot))
			branch := project.ExpectedBranch(c)
			if branch == "" {
				fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Branch build:"),
					ui.DimStyle.Render("nessuno — si builda dalla copia di lavoro"))
				continue
			}
			fmt.Printf("      %s  %s\n", ui.LabelStyle.Render("Branch build:"), ui.ValueStyle.Render(branch))
		}
	}

	if len(psn.Projects) > 0 {
		fmt.Println(ui.SectionStyle.Render("PSN · Progetti"))
		for _, p := range psn.Projects {
			fmt.Printf("\n  %s\n", ui.SelectedItemStyle.Render("["+p.Namespace+"]"))
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Docker root:   "), orNA(p.DockerRoot))
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Branch coll:   "), orNA(p.BranchColl))
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Branch prod:   "), orNA(p.BranchProd))
			for _, dep := range slices.Sorted(maps.Keys(p.Deployments)) {
				fmt.Printf("    %s  %s\n", ui.LabelStyle.Render("Dockerfile:    "),
					ui.ValueStyle.Render(dep+" → "+p.Deployments[dep]))
			}
		}
	}

	if len(psn.Deployments) > 0 {
		fmt.Println(ui.SectionStyle.Render("PSN · Mapping deployment → servizio locale"))
		for _, dep := range slices.Sorted(maps.Keys(psn.Deployments)) {
			fmt.Printf("    %s  %s\n", ui.LabelStyle.Render(fmt.Sprintf("%-15s", dep)),
				ui.ValueStyle.Render(psn.Deployments[dep]))
		}
	}
}

var configSetRootCmd = &cobra.Command{
	Use:   "set-root",
	Short: "Aggiorna i percorsi base Docker e Helm",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		dockerPath, cancelled := ui.RunInput("Percorso base progetti Docker", cfg.Config.DockerRootPath, cfg.Config.DockerRootPath)
		if cancelled {
			return errCancelled
		}

		helmPath, cancelled := ui.RunInput("Percorso base charts Helm", cfg.Config.HelmRootPath, cfg.Config.HelmRootPath)
		if cancelled {
			return errCancelled
		}

		if err := config.SetDockerRootPath(dockerPath); err != nil {
			return fmt.Errorf("errore salvataggio docker root: %w", err)
		}
		if err := config.SetHelmRootPath(helmPath); err != nil {
			return fmt.Errorf("errore salvataggio helm root: %w", err)
		}

		ui.PrintOK("Percorsi aggiornati.")
		return nil
	},
}

var configAddServiceCmd = &cobra.Command{
	Use:   "add-service",
	Short: "Aggiunge un nuovo servizio alla configurazione",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		fmt.Println(ui.SectionStyle.Render("Nuovo Servizio"))

		mode, cancelled := ui.RunList("Modalità inserimento", []string{
			"Guidato — wizard passo-passo",
			"Manuale — incolla YAML",
		})
		if cancelled {
			return errCancelled
		}

		if mode == "Manuale — incolla YAML" {
			return addServiceManual(cfg)
		}
		return addServiceGuided(cfg, err)
	},
}

const serviceYAMLTemplate = `nome-servizio:
  ecr_repository: "123456789012.dkr.ecr.eu-west-1.amazonaws.com/project/service"
  helm_set_key: "servizio.image.tag"
  last_tag: "1.0.0-dev"
  dockerfile_subpath: "folder/Dockerfile"
  helm_values_path: "chart/values.yaml"
  namespace: "my-namespace"
  chart_name: "repo/chart"
  chart_version: "1.0.0"
  release_name: "my-release"
  helm_image_path: ""
  k8s_manifest_path: ""
  k8s_image_ref: ""`

func addServiceManual(cfg *config.Config) error {
	fmt.Printf("\n%s\n%s\n\n",
		ui.LabelStyle.Render("  Formato atteso (campi obbligatori: ecr_repository, helm_set_key):"),
		ui.BoxStyle.Render(ui.DimStyle.Render(serviceYAMLTemplate)),
	)

	raw, cancelled := ui.RunTextArea("Incolla il blocco YAML del servizio", "")
	if cancelled {
		return errCancelled
	}

	name, svc, err := config.ParseServiceYAML(raw)
	if err != nil {
		return fmt.Errorf("formato non valido: %w", err)
	}

	if _, exists := cfg.Services[name]; exists {
		fmt.Println(ui.WarnStyle.Render(fmt.Sprintf("Il servizio %q esiste già.", name)))
		overwrite, cancelled := ui.RunConfirm("Sovrascrivere?", "")
		if cancelled || !overwrite {
			return nil
		}
	}

	saveServiceToViper(name, svc)
	if err := config.Save(); err != nil {
		return fmt.Errorf("errore salvataggio: %w", err)
	}

	ui.PrintOK(fmt.Sprintf("Servizio %q aggiunto.", name))
	return nil
}

func addServiceGuided(cfg *config.Config, prevErr error) error {
	_ = prevErr
	var err error

	name, cancelled := ui.RunInput("Nome servizio:", "my-service", "my-service")
	if cancelled || name == "" {
		return fmt.Errorf("nome servizio obbligatorio")
	}
	if _, exists := cfg.Services[name]; exists {
		fmt.Println(ui.WarnStyle.Render(fmt.Sprintf("Il servizio %q esiste già.", name)))
		overwrite, cancelled := ui.RunConfirm("Sovrascrivere?", "")
		if cancelled || !overwrite {
			return nil
		}
	}

	ecrRepo, cancelled := ui.RunInput(
		"ECR repository",
		"123456789012.dkr.ecr.eu-west-1.amazonaws.com/project/service",
		"",
	)
	if cancelled {
		return errCancelled
	}

	initialTag, cancelled := ui.RunInput("Tag iniziale", "1.0.0-dev", "1.0.0-dev")
	if cancelled {
		return errCancelled
	}

	dockerfilePath := ""
	if cfg.Config.DockerRootPath != "" {
		useDisc, cancelled := ui.RunConfirm(
			fmt.Sprintf("Cerco il Dockerfile in %s?", cfg.Config.DockerRootPath),
			"",
		)
		if !cancelled && useDisc {
			dockerfilePath, err = discoverDockerfile(cfg.Config.DockerRootPath)
			if err != nil {
				ui.PrintWarn(fmt.Sprintf("Discovery fallita: %v", err))
			}
		}
	}
	if dockerfilePath == "" {
		dockerfilePath, cancelled = ui.RunInput("Percorso Dockerfile (relativo alla docker root)", "", "")
		if cancelled {
			return errCancelled
		}
	}

	helmValuesPath := ""
	if cfg.Config.HelmRootPath != "" {
		useDisc, cancelled := ui.RunConfirm(
			fmt.Sprintf("Cerco il values file in %s?", cfg.Config.HelmRootPath),
			"",
		)
		if !cancelled && useDisc {
			helmValuesPath, err = discoverHelmValues(cfg.Config.HelmRootPath)
			if err != nil {
				ui.PrintWarn(fmt.Sprintf("Discovery fallita: %v", err))
			}
		}
	}
	if helmValuesPath == "" {
		helmValuesPath, cancelled = ui.RunInput("Percorso Helm values (relativo alla helm root)", "", "")
		if cancelled {
			return errCancelled
		}
	}

	namespace, cancelled := ui.RunInput("Namespace Kubernetes", "service-dev", "service-dev")
	if cancelled {
		return errCancelled
	}

	chartName, cancelled := ui.RunInput("Chart name (es. my-charts/my-app)", "", "")
	if cancelled {
		return errCancelled
	}

	chartVersion, cancelled := ui.RunInput("Chart version (es. 0.4.0)", "", "")
	if cancelled {
		return errCancelled
	}

	releaseName, cancelled := ui.RunInput("Release name (es. my-app-dev)", "", "")
	if cancelled {
		return errCancelled
	}

	helmSetKey, cancelled := ui.RunInput("Helm set key (es. gateway.image.tag)", "", "")
	if cancelled {
		return errCancelled
	}

	helmImagePath, cancelled := ui.RunInput("Helm image path (opzionale, es. my-org/my-service)", "", "")
	if cancelled {
		return errCancelled
	}

	k8sManifestPath := ""
	k8sImageRef := ""
	hasManifest, cancelled := ui.RunConfirm("Configurare il k8s manifest da aggiornare dopo il deploy?", "")
	if !cancelled && hasManifest {
		k8sManifestPath, cancelled = ui.RunInput("Path k8s manifest (relativo alla docker root)", "k8s/nome-deployment.yaml", "")
		if cancelled {
			return errCancelled
		}
		k8sImageRef, cancelled = ui.RunInput("Image ref da cercare nel manifest (es. my-org/my-service)", "", "")
		if cancelled {
			return errCancelled
		}
	}

	svc := config.ServiceConfig{
		LastTag:           initialTag,
		DockerfileSubpath: dockerfilePath,
		HelmValuesPath:    helmValuesPath,
		Namespace:         namespace,
		ChartName:         chartName,
		ChartVersion:      chartVersion,
		ReleaseName:       releaseName,
		ECRRepository:     ecrRepo,
		HelmSetKey:        helmSetKey,
		HelmImagePath:     helmImagePath,
		K8sManifestPath:   k8sManifestPath,
		K8sImageRef:       k8sImageRef,
	}
	saveServiceToViper(name, svc)

	if err := config.Save(); err != nil {
		return fmt.Errorf("errore salvataggio: %w", err)
	}

	ui.PrintOK(fmt.Sprintf("Servizio %q aggiunto.", name))
	return nil
}

func saveServiceToViper(name string, svc config.ServiceConfig) {
	prefix := fmt.Sprintf("services.%s", name)
	viper.Set(prefix+".last_tag", svc.LastTag)
	viper.Set(prefix+".dockerfile_subpath", svc.DockerfileSubpath)
	viper.Set(prefix+".helm_values_path", svc.HelmValuesPath)
	viper.Set(prefix+".namespace", svc.Namespace)
	viper.Set(prefix+".chart_name", svc.ChartName)
	viper.Set(prefix+".chart_version", svc.ChartVersion)
	viper.Set(prefix+".release_name", svc.ReleaseName)
	viper.Set(prefix+".ecr_repository", svc.ECRRepository)
	viper.Set(prefix+".helm_set_key", svc.HelmSetKey)
	if svc.HelmImagePath != "" {
		viper.Set(prefix+".helm_image_path", svc.HelmImagePath)
	}
	if svc.K8sManifestPath != "" {
		viper.Set(prefix+".k8s_manifest_path", svc.K8sManifestPath)
		viper.Set(prefix+".k8s_image_ref", svc.K8sImageRef)
	}
}

var configRemoveServiceCmd = &cobra.Command{
	Use:          "remove-service",
	Short:        "Rimuove un servizio dalla configurazione",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(cfg.Services) == 0 {
			ui.PrintWarn("Nessun servizio configurato.")
			return nil
		}

		name, cancelled := ui.RunList("Servizio da rimuovere", sortedKeys(cfg.Services))
		if cancelled {
			return errCancelled
		}

		confirmed, cancelled := ui.RunConfirm(
			fmt.Sprintf("Rimuovere %q?", name),
			ui.WarnStyle.Render("  Questa operazione non è reversibile."),
		)
		if cancelled || !confirmed {
			return errCancelled
		}

		if err := config.RemoveService(name); err != nil {
			return fmt.Errorf("errore rimozione: %w", err)
		}

		ui.PrintOK(fmt.Sprintf("Servizio %q rimosso.", name))
		return nil
	},
}

var configRefreshCmd = &cobra.Command{
	Use:          "refresh",
	Short:        "Sincronizza i last_tag con i tag deployati sul cluster",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Read from the local cluster, not whatever context happens to be active.
		if err := ensureLocalKubeContext(cfg); err != nil {
			return err
		}

		fmt.Printf("\n%s\n\n", ui.SectionStyle.Render("  Sincronizzazione dal cluster"))

		names := sortedKeys(cfg.Services)
		updated := 0
		errCount := 0
		// Cache chart version per release; multiple services may share the same release.
		chartCache := make(map[string]string)

		for _, name := range names {
			svc := cfg.Services[name]
			paddedName := fmt.Sprintf("%-22s", name)

			if svc.ReleaseName == "" || svc.Namespace == "" || svc.HelmSetKey == "" {
				fmt.Printf("  %s  %s  %s\n",
					ui.DimStyle.Render("·"),
					ui.DimStyle.Render(paddedName),
					ui.DimStyle.Render("nessuna config Helm"),
				)
				continue
			}

			tag, err := logic.GetDeployedTag(svc.ReleaseName, svc.Namespace, svc.HelmSetKey)
			if err != nil {
				fmt.Printf("  %s  %s  %s\n",
					ui.WarnStyle.Render("⚠"),
					ui.LabelStyle.Render(paddedName),
					ui.DimStyle.Render(err.Error()),
				)
				errCount++
				continue
			}

			// Chart version — cache hit avoids duplicate calls for the same release.
			cacheKey := svc.ReleaseName + "/" + svc.Namespace
			chartVer, ok := chartCache[cacheKey]
			if !ok {
				chartVer, _ = logic.GetDeployedChartVersion(svc.ReleaseName, svc.Namespace)
				chartCache[cacheKey] = chartVer
			}

			tagChanged := tag != svc.LastTag
			chartChanged := chartVer != "" && chartVer != svc.ChartVersion

			if !tagChanged && !chartChanged {
				fmt.Printf("  %s  %s  %s\n",
					ui.SuccessStyle.Render("✓"),
					ui.DimStyle.Render(paddedName),
					ui.DimStyle.Render(tag),
				)
				continue
			}

			// Build the human-readable change description.
			var changes []string
			if tagChanged {
				changes = append(changes, fmt.Sprintf("tag %s → %s",
					ui.DimStyle.Render(svc.LastTag),
					ui.SuccessStyle.Render(tag),
				))
			}
			if chartChanged {
				changes = append(changes, fmt.Sprintf("chart %s → %s",
					ui.DimStyle.Render(svc.ChartVersion),
					ui.SuccessStyle.Render(chartVer),
				))
			}

			if tagChanged {
				if err := config.UpdateServiceTag(name, tag); err != nil {
					fmt.Printf("  %s  %s  errore salvataggio tag: %s\n",
						ui.WarnStyle.Render("⚠"), ui.LabelStyle.Render(paddedName),
						ui.DimStyle.Render(err.Error()),
					)
					errCount++
					continue
				}
			}
			if chartChanged {
				if err := config.UpdateServiceChartVersion(name, chartVer); err != nil {
					fmt.Printf("  %s  %s  errore salvataggio chart: %s\n",
						ui.WarnStyle.Render("⚠"), ui.LabelStyle.Render(paddedName),
						ui.DimStyle.Render(err.Error()),
					)
					errCount++
					continue
				}
			}

			fmt.Printf("  %s  %s  %s\n",
				ui.SuccessStyle.Render("↑"),
				ui.SelectedItemStyle.Render(paddedName),
				strings.Join(changes, ui.DimStyle.Render("  ·  ")),
			)
			updated++
		}

		fmt.Println()
		switch {
		case updated == 0 && errCount == 0:
			ui.PrintOK("Tutto già aggiornato.")
		case errCount > 0:
			ui.PrintWarn(fmt.Sprintf("%d aggiornati · %d errori di lettura", updated, errCount))
		default:
			ui.PrintOK(fmt.Sprintf("%d servizi aggiornati.", updated))
		}
		return nil
	},
}

var configSetChartCmd = &cobra.Command{
	Use:          "set-chart <versione> [servizio...]",
	Short:        "Imposta la chart version su uno o più servizi",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		version := strings.TrimSpace(args[0])
		if version == "" {
			return fmt.Errorf("versione obbligatoria")
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(cfg.Services) == 0 {
			ui.PrintWarn("Nessun servizio configurato.")
			return nil
		}

		var targets []string
		if len(args) > 1 {
			// Explicit service list: validate against config (viper keys are lowercased).
			for _, name := range args[1:] {
				name = strings.ToLower(name)
				if _, ok := cfg.Services[name]; !ok {
					return fmt.Errorf("servizio %q non trovato", name)
				}
				targets = append(targets, name)
			}
		} else {
			names := sortedKeys(cfg.Services)
			items := make([]ui.Item, len(names))
			for i, n := range names {
				cur := cfg.Services[n].ChartVersion
				if cur == "" {
					cur = "(usa globale)"
				}
				items[i] = ui.Item{Value: n, Label: n, Desc: "attuale: " + cur}
			}
			selected, cancelled := ui.RunMultiSelect(
				fmt.Sprintf("Servizi a cui impostare chart %s", version), items)
			if cancelled {
				return errCancelled
			}
			targets = selected
		}

		if err := config.SetChartVersionForServices(version, targets); err != nil {
			return fmt.Errorf("errore salvataggio chart version: %w", err)
		}

		ui.PrintOK(fmt.Sprintf("Chart version %q impostata su %d servizi.", version, len(targets)))
		return nil
	},
}

var configSetThemeCmd = &cobra.Command{
	Use:   "set-theme",
	Short: "Cambia il tema dell'interfaccia",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, cancelled := ui.RunThemePicker()
		if cancelled {
			return errCancelled
		}
		if err := config.SetTheme(name); err != nil {
			return fmt.Errorf("errore salvataggio tema: %w", err)
		}
		ui.InitTheme(name)
		ui.PrintOK(fmt.Sprintf("Tema impostato: %s", name))
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetRootCmd)
	configCmd.AddCommand(configAddServiceCmd)
	configCmd.AddCommand(configRemoveServiceCmd)
	configCmd.AddCommand(configRefreshCmd)
	configCmd.AddCommand(configSetChartCmd)
	configCmd.AddCommand(configSetThemeCmd)
}

func discoverDockerfile(root string) (string, error) {
	files, err := logic.FindDockerfiles(root)
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("nessun Dockerfile trovato in %s", root)
	}
	selected, cancelled := ui.RunList("Seleziona Dockerfile", files)
	if cancelled {
		return "", errCancelled
	}
	rel, err := filepath.Rel(root, selected)
	if err != nil {
		return selected, nil
	}
	return rel, nil
}

func discoverHelmValues(root string) (string, error) {
	files, err := logic.FindHelmValues(root)
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("nessun values file trovato in %s", root)
	}
	selected, cancelled := ui.RunList("Seleziona Helm values file", files)
	if cancelled {
		return "", errCancelled
	}
	rel, err := filepath.Rel(root, selected)
	if err != nil {
		return selected, nil
	}
	return rel, nil
}

func orNA(s string) string {
	if s == "" {
		return ui.WarnStyle.Render("(non impostato)")
	}
	return ui.ValueStyle.Render(s)
}
