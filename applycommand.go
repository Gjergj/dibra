package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
	"github.com/spf13/cobra"
)

type ArtifactConstraintType string

const (
	ArtifactConstraintTypeIfRemoteNotExists ArtifactConstraintType = "if_remote_not_exists"
	ArtifactConstraintTypeExecutable        ArtifactConstraintType = "executable"
)

var artifactConstraintTypeMap = map[ArtifactConstraintType]string{
	ArtifactConstraintTypeIfRemoteNotExists: "if_remote_not_exists",
	ArtifactConstraintTypeExecutable:        "executable",
}

func runApply(cmd *cobra.Command, args []string) error {
	configfile, err := findConfigFile()
	if err != nil {
		return err
	}
	fmt.Printf("Applying configuration from: %s\n", configfile)

	config, err := LoadConfig(configfile)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(config.SSH.Port)
	if err != nil {
		return err
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
		return err
	}
	defer sshConnection.Close()

	commandExecutor := commandexecutor.NewCommandRunner(sshConnection)

	runWithSudo := false
	if config.Service.Systemd.User != "root" {
		runWithSudo = true
	}
	serviceManager := NewServiceManager(commandExecutor, runWithSudo)

	switch config.Service.Operation {
	case "install", "":
		return applyInstallOperation(config, sshConnection, serviceManager)
	case "stop":
		return applyStopOperation(config, serviceManager)
	default:
		return fmt.Errorf("invalid operation: %s", config.Service.Operation)
	}
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
				if status != "" {
					fmt.Printf("Service %s status: %s\n", config.Service.Systemd.Name, status)
				}
			case err := <-errChan:
				if err != nil {
					fmt.Printf("Error monitoring service %s: %v\n", config.Service.Systemd.Name, err)
				}
				return
			}
		}
	}()
	time.Sleep(5 * time.Second)
	return nil
}

func applyInstallOperation(config *Config, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager) error {

	if len(config.Artifacts) > 0 {
		handleArtifacts(config.Artifacts, sshConnection, serviceManager)
	}

	if config.Service.Systemd != nil {
		fsOperations, err := sshConnection.NewFSOPerations()
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

		// err = fsOperations.MkdirAll(config.Service.Systemd.BinPath)
		// if err != nil {
		// 	return err
		// }

		err = handleArtifacts(config.Service.Artifacts, sshConnection, serviceManager)
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

		// check if service already exists, if so, stop it
		status, err := serviceManager.getServiceStatus(config.Service.Systemd.Name)
		if err != nil {
			return err
		}
		if status == "running" {
			err = serviceManager.StopService(config.Service.Systemd.Name)
			if err != nil {
				return err
			}
		}

		// Try to create without force
		err = serviceManager.CreateServiceUnit(unit)
		if err != nil {
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
					if status != "" {
						fmt.Printf("Service %s status: %s\n", config.Service.Systemd.Name, status)
					}
				case err := <-errChan:
					if err != nil {
						fmt.Printf("Error monitoring service: %v\n", err)
					}
					return
				}
			}
		}()
		time.Sleep(5 * time.Second)
	}

	if config.Service.Artifacts != nil {

	}
	return nil
}

func handleArtifacts(artifacts []Artifact, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager) error {
	fsOperations, err := sshConnection.NewFSOPerations()
	if err != nil {
		return err
	}

	for _, artifact := range artifacts {
		upload := true
		executable := false
		//check constraints
		for key, value := range artifact.Constraints {
			switch key {
			case "if_remote_not_exists":
				upload = false

				tmpl, err := template.New("path").Option().Parse(value)
				if err != nil {
					return err
				}
				var remotePath bytes.Buffer
				err = tmpl.Execute(&remotePath, map[string]string{"remote_path": artifact.RemotePath})
				if err != nil {
					return err
				}

				_, err = fsOperations.Stat(remotePath.String())
				if err != nil {
					upload = true
				}
				// if remoteFileInfo.IsDir() {
				// 	fmt.Printf("remote file %s is a directory, expected a file\n", remotePath.String())
				// 	upload = true
				// }
			case "executable":
				executable = true
			}
		}

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
		if upload {
			fmt.Printf("Uploading artifact %s to %s\n", artifact.Path, artifact.RemotePath)
			err = fsOperations.Upload(artifact.Path, artifact.RemotePath)
			if err != nil {
				return err
			}
		}
		if executable {
			fmt.Printf("Making executable %s\n", artifact.RemotePath)
			err = serviceManager.MakeExecutable(artifact.RemotePath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
