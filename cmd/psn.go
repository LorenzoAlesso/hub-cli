package cmd

import (
	"fmt"
	"io"
	"strconv"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	"Hub-cli/internal/ui"
	"github.com/spf13/cobra"
)

var psnCmd = &cobra.Command{
	Use:          "psn",
	Short:        "Build, Push e Deploy su ambienti PSN (Azure AKS + ACR)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		PrintLogo()
		return runPSNWorkflow()
	},
}

func runPSNWorkflow() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("errore caricamento config: %w", err)
	}

	if err := cfg.PSN.Validate(); err != nil {
		ui.PrintWarn("Configurazione PSN non disponibile: " + err.Error())
		ui.PrintInfo(fmt.Sprintf(
			"Aggiungi il blocco `psn:` a %s (template: internal/config/seed.example.yaml) e rilancia.",
			config.SeedFilePath()))
		return errCancelled
	}

	if dryRun && testUI {
		return fmt.Errorf("--dry-run e --test-ui non possono essere usati insieme")
	}
	if err := ensureDockerRoot(cfg); err != nil {
		return err
	}

	cluster, err := selectPSNCluster(cfg)
	if err != nil {
		return err
	}

	if cluster.IsProd() && !dryRun {
		typed, cancelled := ui.RunInput(
			fmt.Sprintf("Ambiente di PRODUZIONE — digita %q per confermare", cluster.Name),
			cluster.Name, "")
		if cancelled {
			return errCancelled
		}
		if typed != cluster.Name {
			ui.PrintErr("Nome non corrispondente: deploy annullato.")
			return errCancelled
		}
	}

	kubeSwitched, azErr := runPSNAzurePhase(cfg, cluster)
	if kubeSwitched {
		// Never leave kubectl pointed at a PSN cluster, even on error/cancel.
		defer restoreKubeContext(cfg)
	}
	if azErr != nil {
		return azErr
	}

	results, cancelled, err := ui.RunPSNWorkflow(cfg, cluster, dryRun, testUI)
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

func selectPSNCluster(cfg *config.Config) (config.PSNClusterConfig, error) {
	items := make([]ui.Item, len(cfg.PSN.Clusters))
	for i, c := range cfg.PSN.Clusters {
		items[i] = ui.Item{
			Value: strconv.Itoa(i),
			Label: c.Name,
			Desc:  psnEnvLabel(c) + " · " + c.AKSName,
		}
	}
	selected, cancelled := ui.RunListItems("Ambiente PSN", items)
	if cancelled {
		return config.PSNClusterConfig{}, errCancelled
	}
	idx, err := strconv.Atoi(selected)
	if err != nil || idx < 0 || idx >= len(cfg.PSN.Clusters) {
		return config.PSNClusterConfig{}, fmt.Errorf("selezione cluster non valida")
	}
	return cfg.PSN.Clusters[idx], nil
}

func psnEnvLabel(c config.PSNClusterConfig) string {
	if c.IsProd() {
		return "PRODUZIONE"
	}
	return "collaudo"
}

// runPSNAzurePhase authenticates and points kubectl/docker at the selected
// cluster: az login → account set → aks get-credentials → acr login.
// AZLogin is interactive (browser), so the whole phase runs outside the TUI.
// Returns kubeSwitched=true once get-credentials has moved the kube context
// to the PSN cluster, so the caller can restore it on the way out.
func runPSNAzurePhase(cfg *config.Config, cluster config.PSNClusterConfig) (kubeSwitched bool, err error) {
	if dryRun {
		ui.PrintDryRun("az login --tenant " + cfg.PSN.TenantID)
		ui.PrintDryRun("az account set --subscription " + cluster.SubscriptionID)
		ui.PrintDryRun(fmt.Sprintf("az aks get-credentials --resource-group %s --name %s --overwrite-existing",
			cluster.ResourceGroup, cluster.AKSName))
		ui.PrintDryRun("az acr login --name " + cluster.ACRName)
		return false, nil
	}
	if testUI {
		ui.PrintOK("Fase Azure simulata (test-ui).")
		return false, nil
	}

	if logic.AZIsLoggedIn() {
		ui.PrintOK("Sessione Azure attiva.")
	} else {
		ui.PrintRunning("az login in corso (browser)...")
		if err := logic.AZLogin(cfg.PSN.TenantID); err != nil {
			return false, err
		}
	}

	if err := ui.RunSpinner("az account set", func(out io.Writer) error {
		return logic.AZSetSubscription(cluster.SubscriptionID)
	}); err != nil {
		// The active session may belong to another tenant: retry once after
		// an explicit login on the PSN tenant.
		ui.PrintWarn("Subscription non accessibile dalla sessione corrente — rieseguo az login.")
		if err := logic.AZLogin(cfg.PSN.TenantID); err != nil {
			return false, err
		}
		if err := ui.RunSpinner("az account set", func(out io.Writer) error {
			return logic.AZSetSubscription(cluster.SubscriptionID)
		}); err != nil {
			return false, err
		}
	}

	if err := ui.RunSpinner("az aks get-credentials", func(out io.Writer) error {
		return logic.AZGetAKSCredentials(cluster.ResourceGroup, cluster.AKSName, out)
	}); err != nil {
		return false, err
	}

	// From here kubectl points at the PSN cluster.
	if err := ui.RunSpinner("az acr login", func(out io.Writer) error {
		return logic.ACRLogin(cluster.ACRName, out)
	}); err != nil {
		return true, err
	}
	return true, nil
}

// restoreKubeContext detaches kubectl from the PSN cluster at the end of the
// workflow: back to the local context when configured, otherwise unset — so
// manual kubectl commands can never hit a PSN cluster by accident.
func restoreKubeContext(cfg *config.Config) {
	if local := cfg.Config.LocalKubeContext; local != "" {
		if err := logic.SwitchKubeContext(local); err != nil {
			ui.PrintWarn("Impossibile ripristinare il contesto kubectl locale: " + err.Error())
			return
		}
		ui.PrintOK("Contesto kubectl ripristinato: " + local)
		return
	}
	if err := logic.KubectlUnsetCurrentContext(); err != nil {
		ui.PrintWarn("Impossibile sganciare il contesto kubectl: " + err.Error())
		return
	}
	ui.PrintOK("Contesto kubectl sganciato (nessun contesto attivo).")
}
