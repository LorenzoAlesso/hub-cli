package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"Hub-cli/internal/config"
	"Hub-cli/internal/ui"
	"github.com/spf13/cobra"
)

var errCancelled = errors.New("annullato")

var (
	cfgFile string
	verbose bool
	dryRun  bool
	testUI  bool
)

var rootCmd = &cobra.Command{
	Use:           "hub-cli",
	Short:         "Hub CLI - Smart deploy automation per Kubernetes",
	Long:          `Hub CLI automatizza il ciclo Build → Push → Deploy su cluster Kubernetes locali.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err == nil {
			ui.InitTheme(cfg.Config.Theme)
		}
		if config.WasSeeded() {
			if config.SeededFromFile() {
				ui.PrintWarn("Configurazione creata dal seed file. Imposta i percorsi con 'hub-cli config set-root'.")
			} else {
				ui.PrintWarn(fmt.Sprintf(
					"Configurazione creata senza servizi. Aggiungili con 'hub-cli config add-service', "+
						"oppure crea %s partendo dal template internal/config/seed.example.yaml e rilancia hub-cli per importarli.",
					config.SeedFilePath(),
				))
			}
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		PrintLogo()
		return runEnvironmentDispatch()
	},
}

// runEnvironmentDispatch lets bare `hub-cli` pick the target environment.
// Without a configured PSN block it goes straight to the local workflow,
// exactly as before PSN existed.
func runEnvironmentDispatch() error {
	cfg, err := config.Load()
	if err == nil && cfg.PSN.Validate() == nil {
		items := []ui.Item{
			{Value: "local", Label: "Locale", Desc: "ECR + Helm (cluster locale)"},
			{Value: "psn", Label: "PSN", Desc: "Azure AKS + ACR (Collaudo - Produzione)"},
		}
		selected, cancelled := ui.RunListItems("Ambiente", items)
		if cancelled {
			return errCancelled
		}
		if selected == "psn" {
			return runPSNWorkflow()
		}
	}
	return runLocalWorkflow()
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, errCancelled) {
			fmt.Println()
			ui.PrintWarn("Operazione annullata! Grazie per aver usato Hub-cli")
			os.Exit(0)
		}
		if isInterrupt(err) {
			fmt.Println()
			fmt.Println(ui.WarnStyle.Render("Arrivederci!"))
			os.Exit(0)
		}
		fmt.Println(ui.ErrStyle.Render("Errore: " + err.Error()))
		os.Exit(1)
	}
}

func isInterrupt(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return msg == "interrupt" || strings.Contains(msg, "interrupted")
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "file di configurazione custom (default: ~/.hub-cli.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "output dettagliato dei comandi")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "mostra i comandi senza eseguirli")
	rootCmd.PersistentFlags().BoolVar(&testUI, "test-ui", false, "simula il deploy con spinner reali ma senza eseguire nulla")

	rootCmd.AddCommand(localCmd)
	rootCmd.AddCommand(psnCmd)
	rootCmd.AddCommand(configCmd)
}

func initConfig() {
	if err := config.Init(cfgFile); err != nil {
		fmt.Println(ui.WarnStyle.Render("Attenzione config: " + err.Error()))
	}
}

func PrintLogo() {
	ui.RunLogo()
	fmt.Println(ui.RenderGradientSeparator(60))
	fmt.Println()
}
