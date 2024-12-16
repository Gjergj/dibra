package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath         string
	defaultConfigPaths = []string{
		"dibra.yaml", // Current directory first
		"dibra.yml",
		"config.yaml",
		"config.yml",
	}

	rootCmd = &cobra.Command{
		Use:   "dibra",
		Short: "Dibra is a service management tool",
		Long: `A service management tool that helps you manage systemd services 
across remote machines via SSH.`,
	}

	applyCmd = &cobra.Command{
		Use:   "apply",
		Short: "Apply service configuration",
		RunE:  runApply,
	}

	validateCmd = &cobra.Command{
		Use:   "validate",
		Short: "Validate service configuration",
		RunE:  runValidate,
	}
)

func findConfigFile() (string, error) {
	// If config path is provided, use it
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
		return "", fmt.Errorf("specified config file not found: %s", configPath)
	}

	// Try default locations in current directory
	for _, path := range defaultConfigPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no configuration file found in current directory. Expected one of: %v", defaultConfigPaths)
}

func runValidate(cmd *cobra.Command, args []string) error {
	config, err := findConfigFile()
	if err != nil {
		return err
	}

	fmt.Printf("Validating configuration from: %s\n", config)
	// Your validation logic here
	return nil
}

func initCmd() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to the configuration file (default: dibra.yaml or dibra.yml in current directory)")
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(validateCmd)
}

func cmd() {
	initCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func main() {

	sess, err := unlockBitwarden()
	if err != nil {
		fmt.Printf("failed to unlock bitwarden: %v\n", err)
		os.Exit(1)
	}

	ssh, err := getBitwardenItem(sess, "vps_ssd_nodes")
	if err != nil {
		fmt.Printf("failed to get bitwarden item: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("got bitwarden item: %+v\n", ssh)
	return
	cmd()
}
