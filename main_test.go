package main

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	// Run all tests
	os.Exit(m.Run())
}

func TestCLICommands(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "validate command without config",
			args:    []string{"validate"},
			wantErr: true,
		},
		{
			name:    "apply command without config",
			args:    []string{"apply"},
			wantErr: true,
		},
		{
			name:    "validate command with config",
			args:    []string{"validate", "--config", "testdata/test_config.yml"},
			wantErr: false,
		},
		{
			name:    "apply command with config",
			args:    []string{"apply", "--config", "testdata/test_config.yml"},
			wantErr: false,
		},
		{
			name:    "invalid command",
			args:    []string{"invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original os.Args
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()

			// Set up test args
			os.Args = append([]string{"dibra"}, tt.args...)

			// Create a new root command for each test
			rootCmd = &cobra.Command{
				Use:   "dibra",
				Short: "Dibra is a service management tool",
			}

			// Reinitialize commands
			initCmd()

			// Execute the command
			err := rootCmd.Execute()

			if (err != nil) != tt.wantErr {
				t.Errorf("command execution error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
