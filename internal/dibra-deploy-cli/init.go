package dibra_deploy_cli

import "github.com/spf13/cobra"

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a dibra-deploy project",
		RunE:  runInitCmd,
	}
	return cmd
}

func runInitCmd(cmd *cobra.Command, args []string) error {
	return cmd.Usage()
}
