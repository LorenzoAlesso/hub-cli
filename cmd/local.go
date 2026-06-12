package cmd

import (
	"fmt"
	"sort"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	"Hub-cli/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var localCmd = &cobra.Command{
	Use:          "local",
	Short:        "Build, Push e Deploy su cluster locale",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		PrintLogo()
		return runLocalWorkflow()
	},
}

func runLocalWorkflow() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("errore caricamento config: %w", err)
	}

	if len(cfg.Services) == 0 {
		ui.PrintWarn("Nessun servizio configurato.")
		return nil
	}

	if err := ensureRootPaths(cfg); err != nil {
		return err
	}
	if err := ensureLocalKubeContext(cfg); err != nil {
		return err
	}
	cfg, err = config.Load()
	if err != nil {
		return err
	}

	if dryRun && testUI {
		return fmt.Errorf("--dry-run e --test-ui non possono essere usati insieme")
	}

	results, cancelled, err := ui.RunWorkflow(cfg, dryRun, testUI)
	if err != nil {
		return err
	}
	if cancelled {
		return errCancelled
	}

	if len(results) == 1 {
		r := results[0]
		if !r.Skipped {
			ui.PrintDeploySummary(r.Service, r.OldTag, r.NewTag, r.Elapsed)
		}
	} else if len(results) > 1 {
		ui.PrintMultiDeploySummary(results)
	}
	return nil
}

func ensureLocalKubeContext(cfg *config.Config) error {
	if cfg.Config.LocalKubeContext == "" {
		contexts, err := logic.ListKubeContexts()
		if err != nil {
			ui.PrintWarn(fmt.Sprintf("Impossibile leggere i contesti kubectl: %v", err))
			return nil
		}
		ui.PrintWarn("Contesto kubectl locale non configurato.")
		selected, cancelled := ui.RunList("Seleziona contesto kubectl per l'ambiente locale", contexts)
		if cancelled {
			return errCancelled
		}
		viper.Set("config.local_kube_context", selected)
		if err := config.Save(); err != nil {
			ui.PrintWarn(fmt.Sprintf("impossibile salvare il contesto: %v", err))
		}
		cfg.Config.LocalKubeContext = selected
	}

	current, err := logic.CurrentKubeContext()
	if err != nil {
		ui.PrintWarn(fmt.Sprintf("Impossibile leggere il contesto kubectl corrente: %v", err))
		return nil
	}
	if current == cfg.Config.LocalKubeContext {
		return nil
	}

	ui.PrintWarn(fmt.Sprintf("Contesto kubectl attivo: %s — cambio a: %s", current, cfg.Config.LocalKubeContext))
	if err := logic.SwitchKubeContext(cfg.Config.LocalKubeContext); err != nil {
		return fmt.Errorf("impossibile cambiare contesto kubectl: %w", err)
	}
	ui.PrintOK(fmt.Sprintf("Contesto: %s", cfg.Config.LocalKubeContext))
	return nil
}

func sortedKeys(m map[string]config.ServiceConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func ensureRootPaths(cfg *config.Config) error {
	if err := ensureDockerRoot(cfg); err != nil {
		return err
	}
	return ensureHelmRoot(cfg)
}

func ensureDockerRoot(cfg *config.Config) error {
	if cfg.Config.DockerRootPath != "" {
		return nil
	}
	ui.PrintWarn("Percorso base Docker non configurato.")
	path, cancelled := ui.RunInput("Percorso base progetti Docker", "", "")
	if cancelled {
		return errCancelled
	}
	if path == "" {
		return fmt.Errorf("percorso Docker obbligatorio")
	}
	if err := config.SetDockerRootPath(path); err != nil {
		return fmt.Errorf("errore salvataggio docker root: %w", err)
	}
	cfg.Config.DockerRootPath = path
	ui.PrintOK(fmt.Sprintf("Percorso salvato in %s", config.GetFilePath()))
	return nil
}

func ensureHelmRoot(cfg *config.Config) error {
	if cfg.Config.HelmRootPath != "" {
		return nil
	}
	ui.PrintWarn("Percorso base Helm non configurato.")
	path, cancelled := ui.RunInput("Percorso base charts Helm", "", "")
	if cancelled {
		return errCancelled
	}
	if path == "" {
		return fmt.Errorf("percorso Helm obbligatorio")
	}
	if err := config.SetHelmRootPath(path); err != nil {
		return fmt.Errorf("errore salvataggio helm root: %w", err)
	}
	cfg.Config.HelmRootPath = path
	ui.PrintOK(fmt.Sprintf("Percorso salvato in %s", config.GetFilePath()))
	return nil
}
