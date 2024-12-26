package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
type ArtifactFileType string

const (
	ArtifactConstraintTypeForce      ArtifactConstraintType = "force"
	ArtifactConstraintTypeExecutable ArtifactConstraintType = "executable"

	ArtifactFileTypeFile ArtifactFileType = "file"
	ArtifactFileTypeDir  ArtifactFileType = "dir"
)

var artifactConstraintTypeMap = map[ArtifactConstraintType]struct{}{
	ArtifactConstraintTypeForce:      {},
	ArtifactConstraintTypeExecutable: {},
}

type sshInventoryMachine map[string]*cmdrunner.SSHConnection

type sshInventoryGroup map[string]sshInventoryMachine

func (s sshInventoryGroup) GetSShConnections(hosts []string) []*cmdrunner.SSHConnection {
	connections := []*cmdrunner.SSHConnection{}
	for _, host := range hosts {
		if _, ok := s[host]; ok {
			for _, connection := range s[host] {
				connections = append(connections, connection)
			}
		} else {
			for _, group := range s {
				if connection, ok := group[host]; ok {
					connections = append(connections, connection)
				}
			}
		}
	}

	return connections
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

	var sshInventory = sshInventoryGroup{}
	for groupName, group := range config.Inventory {
		sshInventory[groupName] = sshInventoryMachine{}
		for hostName, host := range group {

			port, err := strconv.Atoi(host.Port)
			if err != nil {
				return err
			}

			sshConfig := &cmdrunner.SSHConfig{
				Host:          host.Host,
				Port:          port,
				User:          host.User,
				PrivateKey:    host.KeyPath,
				Password:      host.Password,
				Timeout:       time.Duration(host.Timeout) * time.Second,
				AllowInsecure: host.AllowInsecure,
			}
			sshInventory[groupName][hostName] = cmdrunner.NewSSHConnection(sshConfig)
		}
	}

	for _, task := range config.Tasks {
		if task.Disabled {
			fmt.Printf("Skipping task %s because it is disabled\n", task.Name)
			continue
		}
		sshConnections := sshInventory.GetSShConnections(task.Hosts)
		if len(sshConnections) == 0 {
			return fmt.Errorf("no ssh connections found for hosts: %v", task.Hosts)
		}
		for _, sshConnection := range sshConnections {
			fmt.Printf("Connecting to %s\n", sshConnection.HostName())
			err = sshConnection.Connect()
			if err != nil {
				return fmt.Errorf("failed to connect to %s: %w", sshConnection.HostName(), err)
			}
			defer sshConnection.Close()

			commandExecutor := commandexecutor.NewCommandRunner(sshConnection)
			runWithSudo := (sshConnection.User() != "root")

			if runWithSudo && sshConnection.SudoPassword() == "" {
				return fmt.Errorf("some commands require sudo, but no password is provided")
			}

			serviceManager := NewServiceManager(commandExecutor, runWithSudo)
			userManager := NewUserService(commandExecutor, runWithSudo)

			if len(task.Artifacts) > 0 {
				err = handleArtifacts(task.Artifacts, sshConnection, serviceManager, map[string]string{})
				if err != nil {
					return fmt.Errorf("failed to handle artifacts on %s: %w", sshConnection.HostName(), err)
				}
			}
			if task.Systemd != nil {
				switch task.Systemd.Operation {
				case "install", "":
					err = applyInstallOperation(&task, sshConnection.User(), sshConnection, serviceManager, userManager)
					if err != nil {
						return fmt.Errorf("failed to install service %s on %s: %w", task.Systemd.Name, sshConnection.HostName(), err)
					}
				case "stop":
					err = applyStopOperation(&task, serviceManager)
					if err != nil {
						return fmt.Errorf("failed to stop service %s on %s: %w", task.Systemd.Name, sshConnection.HostName(), err)
					}
				default:
					return fmt.Errorf("invalid operation: %s", task.Systemd.Operation)
				}
			}
		}
	}
	return nil
}

func applyStopOperation(task *Task, serviceManager *ServiceManager) error {
	if installed, err := serviceManager.IsServiceInstalled(task.Systemd.Name); err != nil || !installed {
		fmt.Printf("Service %s is not installed, skipping stop\n", task.Systemd.Name)
		return nil
	}
	err := serviceManager.StopService(task.Systemd.Name)
	if err != nil {
		return err
	}

	fmt.Printf("Monitoring service %s\n", task.Systemd.Name)
	// Monitor service status
	statusChan, errChan := serviceManager.MonitorService(task.Systemd.Name, 5*time.Second, 1)

	// Handle status updates and errors
	go func() {
		for {
			select {
			case status := <-statusChan:
				if status != "" {
					fmt.Printf("Service %s status: %s\n", task.Systemd.Name, status)
				}
			case err := <-errChan:
				if err != nil {
					fmt.Printf("Error monitoring service %s: %v\n", task.Systemd.Name, err)
				}
				return
			}
		}
	}()
	time.Sleep(5 * time.Second)
	return nil
}

func applyInstallOperation(task *Task, user string, sshConnection *cmdrunner.SSHConnection, serviceManager *ServiceManager, userManager *UserService) error {
	if task.Systemd != nil && !task.Disabled {
		fsOperations, err := sshConnection.NewFSOPerations()
		if err != nil {
			return err
		}

		if task.Systemd.User == "" {
			if user == "root" {
				task.Systemd.User = task.Systemd.Name
			} else {
				task.Systemd.User = user
			}
		}
		if task.Systemd.Group == "" {
			task.Systemd.Group = task.Systemd.User
		}

		if task.Systemd.ExecStart == "" {
			if user == "root" {
				task.Systemd.ExecStart = filepath.Join("/usr/local/bin/", task.Systemd.Name)
			} else {
				task.Systemd.ExecStart = filepath.Join("/home/", user, task.Systemd.Name, task.Systemd.Name)
			}
		}

		if task.Systemd.WorkingDir == "" {
			if user == "root" {
				task.Systemd.WorkingDir = filepath.Join("/var/lib/", task.Systemd.Name)
			} else {
				task.Systemd.WorkingDir = filepath.Dir(task.Systemd.ExecStart)
			}
		}
		fmt.Printf("Installing service %s with user %s\n", task.Systemd.Name, task.Systemd.User)
		fmt.Printf("Installing service at %s with working dir %s\n", task.Systemd.ExecStart, task.Systemd.WorkingDir)

		if task.Systemd.User != user {
			if user == "root" {
				fmt.Printf("Creating user %s\n", task.Systemd.User)
				err = userManager.Create(User{
					Username: task.Systemd.User,
					Groups:   []string{task.Systemd.User},
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

		// if config.Task.Systemd.BinPath == "" {
		// 	config.Task.Systemd.BinPath = "/usr/local/bin" + config.Task.Systemd.Name
		// }

		// Create a new service unit
		unit := ServiceUnit{
			Name:        task.Systemd.Name,
			Description: task.Systemd.Description,
			ExecStart:   task.Systemd.ExecStart,
			WorkingDir:  task.Systemd.WorkingDir,
			User:        task.Systemd.User,
			Environment: task.Systemd.Env,
			Group:       task.Systemd.Group,
			RestartSec:  10,
			Restart:     "always",
			WantedBy:    "multi-user.target",
		}

		// err = fsOperations.MkdirAll(config.Task.Systemd.BinPath)
		// if err != nil {
		// 	return err
		// }

		// make sure workdir exists
		err = fsOperations.MkdirAll(task.Systemd.WorkingDir)
		if err != nil {
			return err
		}

		err = serviceManager.StopService(task.Systemd.Name)
		if err != nil {
			return err
		}
		// handle artifacts only after stopping the service
		err = handleArtifacts(task.Systemd.Artifacts, sshConnection, serviceManager, map[string]string{
			"TASK_EXEC_PATH": task.Systemd.ExecStart,
			"TASK_WORKDIR":   task.Systemd.WorkingDir,
		})
		if err != nil {
			return err
		}

		// Try to create without force
		err = serviceManager.CreateServiceUnit(unit)
		if err != nil {
			log.Fatalf("Failed to create service unit: %v", err)
		}
		if err := serviceManager.InstallService(task.Systemd.Name); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}

		// Start the service
		if err := serviceManager.StartService(task.Systemd.Name); err != nil {
			log.Fatalf("Failed to start service: %v", err)
		}
		// Monitor service status
		statusChan, errChan := serviceManager.MonitorService(task.Systemd.Name, 5*time.Second, 1)

		// Handle status updates and errors
		go func() {
			for {
				select {
				case status := <-statusChan:
					if status != "" {
						fmt.Printf("Service %s status: %s\n", task.Systemd.Name, status)
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
		remoteMustExist := false
		sha256Hash := ""
		remoteFileType := ArtifactFileTypeFile
		if artifact.Type == "local" && artifact.Path != "" {
			// detect if it's file or directory
			fileInfo, err := os.Stat(artifact.Path)
			if err != nil {
				return fmt.Errorf("failed to stat local file %s: %w", artifact.Path, err)
			}
			if fileInfo.IsDir() {
				remoteFileType = ArtifactFileTypeDir
			} else {
				remoteFileType = ArtifactFileTypeFile
				sha256Hash, err = hashFile(artifact.Path)
				if err != nil {
					return err
				}

				// check if remote file exists
				remoteFileInfo, err := fsOperations.Stat(artifact.RemotePath)
				if err != nil {
					remoteMustExist = true
				} else {
					if remoteFileInfo.IsDir() {
						return fmt.Errorf("remote file %s is a directory, expected a file", artifact.RemotePath)
					}
					remoteSha256Hash, err := serviceManager.HashRemoteFile(artifact.RemotePath)
					if err != nil {
						return err
					}
					if sha256Hash != remoteSha256Hash {
						remoteMustExist = true
					}
				}
			}
		} else if artifact.Type == "local" && artifact.Content != "" {
			//get remote file content
			remoteContent, err := serviceManager.ReadRemoteTextFile(artifact.RemotePath)
			if err != nil {
				return err
			}
			if remoteContent != artifact.Content {
				remoteMustExist = true
			}
		}

		// remoteMustExist := true
		executable := false
		//check constraints
		for key := range artifact.Constraints {
			switch ArtifactConstraintType(key) {
			case ArtifactConstraintTypeForce:
				remoteMustExist = true

				// _, err = fsOperations.Stat(artifact.RemotePath)
				// if err != nil {
				// 	remoteMustExist = true
				// }
				// if remoteFileInfo.IsDir() {
				// 	fmt.Printf("remote file %s is a directory, expected a file\n", remotePath.String())
				// 	upload = true
				// }
			case ArtifactConstraintTypeExecutable:
				executable = true
			default:
				return fmt.Errorf("unsupported artifact constraint type: %s", key)
			}
		}

		// no need to create remote directory if it does not exist, sftp client will create it
		// // create remote directory if it does not exist
		// err = fsOperations.MkdirAll(filepath.Dir(artifact.RemotePath))
		// if err != nil {
		// 	return err
		// }
		if remoteMustExist && artifact.Path != "" {
			if remoteFileType == ArtifactFileTypeDir {
				fmt.Printf("Uploading artifact %s to %s\n", artifact.Path, artifact.RemotePath)
				remoteTmpFilePath, err := fsOperations.UploadDir(artifact.Path, artifact.RemotePath)
				if err != nil {
					return err
				}
				err = serviceManager.UntarRemoteFile(remoteTmpFilePath, artifact.RemotePath)
				if err != nil {
					return err
				}
			} else {
				fmt.Printf("Uploading artifact %s to %s\n", artifact.Path, artifact.RemotePath)
				err = fsOperations.Upload(artifact.Path, artifact.RemotePath)
				if err != nil {
					return err
				}
			}
		} else if remoteMustExist && artifact.Content != "" {
			fmt.Printf("Uploading artifact content to %s\n", artifact.RemotePath)
			if remoteFileType == ArtifactFileTypeDir {
				return fmt.Errorf("can not upload content to a directory")
			}
			err = fsOperations.UploadContent(artifact.Content, artifact.RemotePath)
			if err != nil {
				return err
			}
		}
		if executable {
			remoteFileInfo, err := fsOperations.Stat(artifact.RemotePath)
			if err != nil {
				return fmt.Errorf("can not make executable remote file %s does not exist: %v", artifact.RemotePath, err)
			}
			if remoteFileInfo.IsDir() {
				// fmt.Printf("remote file %s is a directory, expected a file\n", artifact.RemotePath)
				return fmt.Errorf("can not make executable remote file %s is a directory", artifact.RemotePath)
			}
			fmt.Printf("Making executable %s\n", artifact.RemotePath)
			err = serviceManager.MakeExecutable(artifact.RemotePath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
