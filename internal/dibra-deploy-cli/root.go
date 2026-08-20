package dibra_deploy_cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gjergjiramku/dibra/internal/agent"
	"github.com/gjergjiramku/dibra/internal/deploy"
	"github.com/gjergjiramku/dibra/internal/version"
	"github.com/spf13/cobra"
)

func Execute(ctx context.Context) error {
	command := NewRootCommand()
	command.SetContext(ctx)
	return command.Execute()
}

func NewRootCommand() *cobra.Command {
	var agentPath string
	var agentBuild bool
	var forceAgentUpload bool
	var endpoint string
	var token string
	var verbose bool

	command := &cobra.Command{
		Use:           "dibra-deploy",
		Short:         "Continuously pull and apply local Dibra deployment jobs",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", version.Version, version.Commit, version.Date),
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("dibra-deploy is supported only on Linux")
			}
			if os.Geteuid() != 0 {
				return fmt.Errorf("dibra-deploy must run as root")
			}
			if agentPath != "" && agentBuild {
				return fmt.Errorf("--agent-path and --agent-build are mutually exclusive")
			}
			mode := agent.ModeAuto
			if agentPath != "" {
				mode = agent.ModePath
			} else if agentBuild {
				mode = agent.ModeBuild
			}
			daemon, err := deploy.New(deploy.Config{
				Endpoint:         endpoint,
				Token:            token,
				AgentMode:        mode,
				AgentPath:        agentPath,
				Version:          version.Version,
				ProjectRoot:      findProjectRoot(),
				ForceAgentUpload: forceAgentUpload,
				Verbose:          verbose,
			})
			if err != nil {
				return err
			}
			return daemon.Run(cmd.Context())
		},
	}
	command.Flags().StringVar(&agentPath, "agent-path", "", "Path to a pre-built agent binary")
	command.Flags().BoolVar(&agentBuild, "agent-build", false, "Build the agent from source (requires Go)")
	command.Flags().BoolVar(&forceAgentUpload, "force-agent-upload", false, "Force replacement of the local runtime agent")
	command.Flags().StringVar(&endpoint, "endpoint", envOrDefault("DIBRA_DEPLOY_ENDPOINT", deploy.DefaultEndpoint), "Task server endpoint")
	token = os.Getenv("DIBRA_DEPLOY_TOKEN")
	command.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose task output")
	command.AddCommand(newCompletionCommand(command))
	return command
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion code",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
}

func findProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	workingDirectory, _ := os.Getwd()
	return workingDirectory
}
