package dibra_deploy_cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dibra-deploy",
	Short: "A CLI tool for deploying applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Usage()
	},
}

func Execute() error {
	rootCmd.AddCommand(newInitCmd())
	return rootCmd.Execute()
}
