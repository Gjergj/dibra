package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
	"github.com/spf13/cobra"
)

func runApply(cmd *cobra.Command, args []string) error {
	configfile, err := findConfigFile()
	if err != nil {
		return err
	}

	config, err := LoadConfig(configfile)
	if err != nil {
		return err
	}

	fmt.Printf("Applying configuration from: %s\n", configfile)
	sshConfig := &cmdrunner.SSHConfig{
		Host:          config.SSH.Host,
		Port:          config.SSH.Port,
		User:          config.SSH.User,
		PrivateKey:    config.SSH.KeyPath,
		Password:      config.SSH.Password,
		Timeout:       time.Duration(config.SSH.Timeout) * time.Second,
		AllowInsecure: config.SSH.AllowInsecure,
	}

	sshConnection := cmdrunner.NewSSHConnection(sshConfig)
	err = sshConnection.Connect()
	if err != nil {
		return err
	}
	defer sshConnection.Close()

	commandExecutor := commandexecutor.NewCommandRunner(sshConnection)
	serviceManager := NewServiceManager(commandExecutor, true)

	switch config.Service.Operation {
	case "install", "":
		return applyInstallOperation(config, sshConnection, serviceManager)
	case "stop":
		return applyStopOperation(config, serviceManager)
	}
	return nil
}

func applyStopOperation(config *Config, serviceManager *ServiceManager) error {
	fmt.Printf("Stopping service %s\n", config.Service.Systemd.Name)
	err := serviceManager.StopService(config.Service.Systemd.Name)
	if err != nil {
		return err
	}

	fmt.Printf("Monitoring service %s\n", config.Service.Systemd.Name)
	// Monitor service status
	statusChan, errChan := serviceManager.MonitorService(config.Service.Systemd.Name, 5*time.Second, 1)

	// Handle status updates and errors
	go func() {
		for {
			select {
			case status := <-statusChan:
				fmt.Printf("Service %s status: %s\n", config.Service.Systemd.Name, status)
			case err := <-errChan:
				fmt.Printf("Error monitoring service %s: %v\n", config.Service.Systemd.Name, err)
				return
			}
		}
	}()
	time.Sleep(5 * time.Second)
	return nil
}

func applyInstallOperation(config *Config, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager) error {
	fsOperations, err := sshConnection.NewFSOPerations()
	if err != nil {
		return err
	}

	err = fsOperations.MkdirAll(config.Service.Systemd.BinPath)
	if err != nil {
		return err
	}

	err = uploadArtifacts(config, sshConnection, serviceManager)
	if err != nil {
		return err
	}

	if config.Service.Systemd.WorkingDir == "" {
		config.Service.Systemd.WorkingDir = config.Service.Systemd.BinPath
	}
	// make sure workdir exists
	err = fsOperations.MkdirAll(config.Service.Systemd.WorkingDir)
	if err != nil {
		return err
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
				fmt.Printf("Service %s status: %s\n", config.Service.Systemd.Name, status)
			case err := <-errChan:
				fmt.Printf("Error monitoring service: %v\n", err)
				return
			}
		}
	}()
	time.Sleep(5 * time.Second)

	// // Wait for some time
	// time.Sleep(5 * time.Minute)

	// Get logs with custom options
	// logs, err := serviceManager.GetServiceLogs(config.Service.Systemd.Name, LogOptions{
	// 	// Since:  time.Now().Add(-24 * time.Hour), // Last 24 hours
	// 	Lines: 50, // Maximum 1000 lines
	// 	// Follow: true, // Follow new logs
	// })
	// if err != nil {
	// 	return err
	// }
	// log.Printf("Service logs: %s", logs)

	// time.Sleep(5 * time.Minute)

	// // ... later, if you need to stop the service ...
	// if err := serviceManager.StopService(config.Service.Systemd.Name); err != nil {
	// 	log.Fatalf("Failed to stop service: %v", err)
	// }
	return nil
}

func uploadArtifacts(config *Config, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager) error {
	fsOperations, err := sshConnection.NewFSOPerations()
	if err != nil {
		return err
	}

	for _, artifact := range config.Service.Artifacts {
		//check if local file exists and is a file and not directory
		fileInfo, err := os.Stat(artifact.Path)
		if err != nil {
			return fmt.Errorf("local file %s does not exist: %v", artifact.Path, err)
		}
		if fileInfo.IsDir() {
			return fmt.Errorf("local path %s is a directory, expected a file", artifact.Path)
		}

		err = fsOperations.MkdirAll(filepath.Dir(artifact.RemotePath))
		if err != nil {
			return err
		}
		err = fsOperations.Upload(artifact.Path, artifact.RemotePath)
		if err != nil {
			return err
		}
		if artifact.Type == "localbinary" {
			err = serviceManager.MakeExecutable(artifact.RemotePath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
