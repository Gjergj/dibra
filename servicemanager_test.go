package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
)

func TestServiceManager(t *testing.T) {

	config, err := LoadConfig("testdata/test_config.yml")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	port, err := strconv.Atoi(config.SSH.Port)
	if err != nil {
		t.Fatalf("Failed to convert port to int: %v", err)
	}
	sshConfig := &cmdrunner.SSHConfig{
		Host:          config.SSH.Host,
		Port:          port,
		User:          config.SSH.User,
		PrivateKey:    config.SSH.KeyPath,
		Password:      config.SSH.Password,
		Timeout:       time.Duration(config.SSH.Timeout) * time.Second,
		AllowInsecure: config.SSH.AllowInsecure,
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

	err = fsOperations.MkdirAll(config.Service.Systemd.BinPath)
	if err != nil {
		t.Fatalf("Failed to create bin path: %v", err)
	}

	err = fsOperations.Upload("/Users/gjergjiramku/projekte/social_posts/bin/social_posts", filepath.Join(config.Service.Systemd.BinPath, config.Service.Systemd.Name))
	if err != nil {
		t.Fatalf("Failed to copy file: %v", err)
	}

	if config.Service.Systemd.WorkingDir == "" {
		config.Service.Systemd.WorkingDir = config.Service.Systemd.BinPath
	}
	// make sure workdir exists
	err = fsOperations.MkdirAll(config.Service.Systemd.WorkingDir)
	if err != nil {
		t.Fatalf("Failed to create WorkingDir path: %v", err)
	}

	// Create a new service unit
	unit := ServiceUnit{
		Name:        config.Service.Systemd.Name,
		Description: config.Service.Systemd.Description,
		ExecStart:   filepath.Join(config.Service.Systemd.BinPath, config.Service.Systemd.Name),
		WorkingDir:  config.Service.Systemd.WorkingDir,
		User:        config.Service.Systemd.User,
		Environment: config.Service.Systemd.Env,
		RestartSec:  10,
		Restart:     "always",
		WantedBy:    "multi-user.target",
	}

	// Try to create without force
	err = serviceManager.CreateServiceUnit(unit)
	if err != nil {
		log.Fatalf("Failed to create service unit: %v", err)
	}
	if err := serviceManager.InstallService(config.Service.Systemd.Name); err != nil {
		log.Fatalf("Failed to install service: %v", err)
	}

	// Start the service
	if err := serviceManager.StartService(config.Service.Systemd.Name); err != nil {
		log.Fatalf("Failed to start service: %v", err)
	}
	// Monitor service status
	statusChan, errChan := serviceManager.MonitorService(config.Service.Systemd.Name, 5*time.Second, 1)

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
	logs, err := serviceManager.GetServiceLogs(config.Service.Systemd.Name, LogOptions{
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
	if err := serviceManager.StopService(config.Service.Systemd.Name); err != nil {
		log.Fatalf("Failed to stop service: %v", err)
	}
}
