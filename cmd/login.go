package cmd

import (
	"io"

	"Hub-cli/internal/config"
	"Hub-cli/internal/logic"
	"Hub-cli/internal/ui"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Autenticazione AWS ECR",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return ui.RunSpinner("ECR Login", func(out io.Writer) error {
			return logic.ECRLogin(cfg.Config.ECRRegion, cfg.Config.ECRAccountID, out)
		})
	},
}
