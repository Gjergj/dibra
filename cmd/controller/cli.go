package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type controllerFlags struct {
	ConfigPath        string
	ForceUpload       bool
	Verbose           bool
	ShowVersion       bool
	AgentPath         string
	AgentBuild        bool
	InventoryPath     string
	InventoryPathLong string
	ExtraVars         string
	ExtraVarsLong     string
	Validate          bool
	CheckMode         bool
	DiffMode          bool
}

func newControllerFlagSet(options *controllerFlags, errorHandling flag.ErrorHandling, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet("dibra", errorHandling)
	flags.SetOutput(output)
	flags.StringVar(&options.ConfigPath, "config", "playbook.yaml", "Path to playbook config file (YAML)")
	flags.BoolVar(&options.ForceUpload, "force-agent-upload", false, "Force upload of agent binary")
	flags.BoolVar(&options.Verbose, "v", false, "Verbose output")
	flags.BoolVar(&options.ShowVersion, "version", false, "Print version and exit")
	flags.StringVar(&options.AgentPath, "agent-path", "", "Path to a pre-built agent binary")
	flags.BoolVar(&options.AgentBuild, "agent-build", false, "Build agent from source (requires Go)")
	flags.StringVar(&options.InventoryPath, "i", "", "Path to YAML inventory file")
	flags.StringVar(&options.InventoryPathLong, "inventory", "", "Path to YAML inventory file")
	flags.StringVar(&options.ExtraVars, "e", "", "Extra variables (key=value or @file.yaml)")
	flags.StringVar(&options.ExtraVarsLong, "extra-vars", "", "Extra variables (key=value or @file.yaml)")
	flags.BoolVar(&options.Validate, "validate", false, "Validate config and exit (no execution)")
	flags.BoolVar(&options.CheckMode, "check", false, "Run in check mode without changing targets when supported")
	flags.BoolVar(&options.DiffMode, "diff", false, "Show structured changes when supported")
	return flags
}

func runCompletion(args []string, flags *flag.FlagSet, output io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dibra completion <bash|zsh|fish|powershell>")
	}

	root := completionRoot(flags)
	switch args[0] {
	case "bash":
		return root.GenBashCompletion(output)
	case "zsh":
		return root.GenZshCompletion(output)
	case "fish":
		return root.GenFishCompletion(output, true)
	case "powershell":
		return root.GenPowerShellCompletion(output)
	default:
		return fmt.Errorf("unsupported shell %q", args[0])
	}
}

func isCompletionRequest(args []string) bool {
	return len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd)
}

func runCompletionRequest(args []string, flags *flag.FlagSet, output, errorOutput io.Writer) error {
	root := completionRoot(flags)
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(errorOutput)
	root.SilenceErrors = true
	root.SilenceUsage = true
	return root.Execute()
}

func completionRoot(flags *flag.FlagSet) *cobra.Command {
	root := &cobra.Command{
		Use:                   "dibra",
		Short:                 "Run Dibra playbooks",
		DisableFlagsInUseLine: true,
	}
	root.PersistentFlags().AddGoFlagSet(flags)
	root.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate configuration without executing it",
	})
	root.AddCommand(&cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion code",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	})
	return root
}
