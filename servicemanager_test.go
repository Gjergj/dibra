package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
	yaml "gopkg.in/yaml.v2"
)

func TestServiceManager(t *testing.T) {

	testYML := `service:
  type: systemd
  name: myapp
  description: My Custom Application Service
  bin_path: /home/testuser
  user: testuser
  working_dir: /home/testuser
  `
	type Config struct {
		Service struct {
			Type        string `yaml:"type"`
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
			BinPath     string `yaml:"bin_path"`
			User        string `yaml:"user"`
			WorkingDir  string `yaml:"working_dir"`
		} `yaml:"service"`
	}

	var config Config
	err := yaml.Unmarshal([]byte(testYML), &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	sshConfig := &cmdrunner.SSHConfig{
		Host:       "localhost",
		Port:       32222,
		User:       "default",
		PrivateKey: "/Users/gjergjiramku/.orbstack/ssh/id_ed25519",
		Password:   "1234",
		// KeyPassphrase: "optional-passphrase", // Leave empty if key is not encrypted
		Timeout: 30 * time.Second,
		// KnownHosts:    "/Users/gjergjiramku/.orbstack/ssh/authorized_keys",
		AllowInsecure: true,
	}

	sshConnection := cmdrunner.NewSSHConnection(sshConfig)
	err = sshConnection.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to SSH: %v", err)
	}
	defer sshConnection.Close()

	commandExecutor := commandexecutor.NewCommandRunner(sshConnection)
	serviceManager := NewServiceManager(commandExecutor, true)

	fsOperations, err := sshConnection.NewFSOPerations()
	if err != nil {
		t.Fatalf("Failed to create FSOperations: %v", err)
	}

	err = fsOperations.MkdirAll(config.Service.BinPath)
	if err != nil {
		t.Fatalf("Failed to create bin path: %v", err)
	}

	err = fsOperations.Upload("/Users/gjergjiramku/projekte/social_posts/bin/social_posts", filepath.Join(config.Service.BinPath, config.Service.Name))
	if err != nil {
		t.Fatalf("Failed to copy file: %v", err)
	}

	if config.Service.WorkingDir == "" {
		config.Service.WorkingDir = config.Service.BinPath
	}
	// make sure workdir exists
	err = fsOperations.MkdirAll(config.Service.WorkingDir)
	if err != nil {
		t.Fatalf("Failed to create WorkingDir path: %v", err)
	}

	// Create a new service unit
	unit := ServiceUnit{
		Name:        config.Service.Name,
		Description: config.Service.Description,
		ExecStart:   filepath.Join(config.Service.BinPath, config.Service.Name),
		WorkingDir:  config.Service.WorkingDir,
		User:        config.Service.User,
		Environment: []string{"PORT=8080", "ENV=production"},
		RestartSec:  10,
		Restart:     "always",
		WantedBy:    "multi-user.target",
	}

	// Try to create without force
	err = serviceManager.CreateServiceUnit(unit, true)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			// Handle existing service case
			log.Printf("Service already exists: %v", err)
			// Optionally retry with force
			err = serviceManager.CreateServiceUnit(unit, true)
		}
		if err != nil {
			log.Fatalf("Failed to create service unit: %v", err)
		}
	}
	if err := serviceManager.InstallService(config.Service.Name); err != nil {
		log.Fatalf("Failed to install service: %v", err)
	}

	// Start the service
	if err := serviceManager.StartService(config.Service.Name); err != nil {
		log.Fatalf("Failed to start service: %v", err)
	}
	// Monitor service status
	statusChan, errChan := serviceManager.MonitorService(config.Service.Name, 5*time.Second)

	// Handle status updates and errors
	go func() {
		for {
			select {
			case status := <-statusChan:
				fmt.Printf("Service status: %s\n", status)
			case err := <-errChan:
				fmt.Printf("Error monitoring service: %v\n", err)
				return
			}
		}
	}()

	// // Wait for some time
	// time.Sleep(5 * time.Minute)

	// Get logs with custom options
	logs, err := serviceManager.GetServiceLogs(config.Service.Name, LogOptions{
		// Since:  time.Now().Add(-24 * time.Hour), // Last 24 hours
		Lines: 50, // Maximum 1000 lines
		// Follow: true, // Follow new logs
	})
	if err != nil {
		t.Fatalf("Failed to get service logs: %v", err)
	}
	log.Printf("Service logs: %s", logs)

	time.Sleep(5 * time.Minute)

	// ... later, if you need to stop the service ...
	if err := serviceManager.StopService(config.Service.Name); err != nil {
		log.Fatalf("Failed to stop service: %v", err)
	}
}
