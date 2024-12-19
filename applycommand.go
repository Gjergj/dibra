package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
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

var artifactConstraintTypeMap = map[ArtifactConstraintType]struct{}{
	ArtifactConstraintTypeIfRemoteNotExists: {},
	ArtifactConstraintTypeExecutable:        {},
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

	runWithSudo := config.Service.Systemd.User != "root"
	serviceManager := NewServiceManager(commandExecutor, runWithSudo)
	userManager := NewUserService(commandExecutor, runWithSudo)

	if len(config.Artifacts) > 0 {
		handleArtifacts(config.Artifacts, sshConnection, serviceManager, map[string]string{})
	}
	if config.Service != nil {
		switch config.Service.Operation {
		case "install", "":
			return applyInstallOperation(config, sshConnection, serviceManager, userManager)
		case "stop":
			return applyStopOperation(config, serviceManager)
		default:
			return fmt.Errorf("invalid operation: %s", config.Service.Operation)
		}
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

func applyInstallOperation(config *Config, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager, userManager *UserService) error {
	if config.Service.Systemd != nil {
		fsOperations, err := sshConnection.NewFSOPerations()
		if err != nil {
			return err
		}

		if config.Service.Systemd.User == "" {
			if config.SSH.User == "root" {
				config.Service.Systemd.User = config.Service.Systemd.Name
			} else {
				config.Service.Systemd.User = config.SSH.User
			}
		}

		if config.Service.Systemd.ExecStart == "" {
			if config.SSH.User == "root" {
				config.Service.Systemd.ExecStart = filepath.Join("/usr/local/bin/", config.Service.Systemd.Name)
			} else {
				config.Service.Systemd.ExecStart = filepath.Join("/home/", config.SSH.User, config.Service.Systemd.Name, config.Service.Systemd.Name)
			}
		}

		if config.Service.Systemd.WorkingDir == "" {
			if config.SSH.User == "root" {
				config.Service.Systemd.WorkingDir = filepath.Join("/var/lib/", config.Service.Systemd.Name)
			} else {
				config.Service.Systemd.WorkingDir = filepath.Dir(config.Service.Systemd.ExecStart)
			}
		}
		fmt.Printf("Installing service %s with user %s\n", config.Service.Systemd.Name, config.Service.Systemd.User)
		fmt.Printf("Installing service at %s with working dir %s\n", config.Service.Systemd.ExecStart, config.Service.Systemd.WorkingDir)

		if config.Service.Systemd.User != config.SSH.User {
			if config.SSH.User == "root" {
				fmt.Printf("Creating user %s\n", config.Service.Systemd.User)
				err = userManager.Create(User{
					Username: config.Service.Systemd.User,
					Groups:   []string{config.Service.Systemd.User},
					System:   true,
				})
				if err != nil {
					return err
				}
			}
		}
		// TODO: Set permissions:
		// sudo chown -R <service-username>:<service-groupname> /path/to/service-directory
		// Change ownership of files and directories to the service user.
		// Restrict permissions by setting them to read and write only where needed, such as chmod 600 for files and chmod 700 for directories.

		// if config.Service.Systemd.BinPath == "" {
		// 	config.Service.Systemd.BinPath = "/usr/local/bin" + config.Service.Systemd.Name
		// }

		// Create a new service unit
		unit := ServiceUnit{
			Name:        config.Service.Systemd.Name,
			Description: config.Service.Systemd.Description,
			ExecStart:   config.Service.Systemd.ExecStart,
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

		err = handleArtifacts(config.Service.Artifacts, sshConnection, serviceManager, map[string]string{
			"SERVICE_EXEC_PATH": config.Service.Systemd.ExecStart,
			"SERVICE_WORKDIR":   config.Service.Systemd.WorkingDir,
		})
		if err != nil {
			return err
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

func handleArtifacts(artifacts []Artifact, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager, variables map[string]string) error {
	fsOperations, err := sshConnection.NewFSOPerations()
	if err != nil {
		return err
	}

	for _, artifact := range artifacts {

		artifact.Path = os.Expand(artifact.Path, func(s string) string {
			if s == "" {
				return ""
			}
			if _, ok := variables[s]; ok {
				return variables[s]
			}
			return fmt.Sprintf("${%s}", s)
		})
		artifact.RemotePath = os.Expand(artifact.RemotePath, func(s string) string {
			if s == "" {
				return ""
			}
			if _, ok := variables[s]; ok {
				return variables[s]
			}
			return fmt.Sprintf("${%s}", s)
		})

		remoteMustExist := true
		executable := false
		//check constraints
		for key := range artifact.Constraints {
			switch key {
			case "if_remote_not_exists":
				remoteMustExist = false

				_, err = fsOperations.Stat(artifact.RemotePath)
				if err != nil {
					remoteMustExist = true
				}
				// if remoteFileInfo.IsDir() {
				// 	fmt.Printf("remote file %s is a directory, expected a file\n", remotePath.String())
				// 	upload = true
				// }
			case "executable":
				executable = true
			}
		}
		if artifact.Path != "" {
			//check if local file exists and is a file and not directory
			fileInfo, err := os.Stat(artifact.Path)
			if err != nil {
				return fmt.Errorf("local file %s does not exist: %v", artifact.Path, err)
			}
			if fileInfo.IsDir() {
				return fmt.Errorf("local path %s is a directory, expected a file", artifact.Path)
			}
		}

		// no need to create remote directory if it does not exist, sftp client will create it
		// // create remote directory if it does not exist
		// err = fsOperations.MkdirAll(filepath.Dir(artifact.RemotePath))
		// if err != nil {
		// 	return err
		// }
		if remoteMustExist && artifact.Path != "" {
			fmt.Printf("Uploading artifact %s to %s\n", artifact.Path, artifact.RemotePath)
			err = fsOperations.Upload(artifact.Path, artifact.RemotePath)
			if err != nil {
				return err
			}
		} else if remoteMustExist && artifact.Content != "" {
			fmt.Printf("Uploading artifact content to %s\n", artifact.RemotePath)
			err = fsOperations.UploadContent(artifact.Content, artifact.RemotePath)
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
