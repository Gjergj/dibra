package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"golang.org/x/crypto/ssh"
)

type ServiceManager struct {
	exec     commandexecutor.CommandExecutor
	sudoInfo *commandexecutor.SudoInfo
}

// NewServiceManager creates a new ServiceManager instance
func NewServiceManager(exec commandexecutor.CommandExecutor, sudoInfo *commandexecutor.SudoInfo) *ServiceManager {
	return &ServiceManager{exec: exec, sudoInfo: sudoInfo}
}

// MonitorService monitors a service status via SSH
func (s *ServiceManager) MonitorService(serviceName string, interval time.Duration) (<-chan string, <-chan error) {
	statusChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(statusChan)
		defer close(errChan)

		for {
			status, err := s.getServiceStatus(serviceName)
			if err != nil {
				errChan <- err
				return
			}

			select {
			case statusChan <- status:
			default:
				// Channel is full, skip this update
			}

			time.Sleep(interval)
		}
	}()

	return statusChan, errChan
}

// getServiceStatus gets the current status of a service
func (s *ServiceManager) getServiceStatus(serviceName string) (string, error) {

	// command := fmt.Sprintf("systemctl status %s", serviceName)
	// err := session.Run(fmt.Sprintf("sudo -S -p '' %s", command));
	output, stderr, err := s.exec.Execute("systemctl", "", s.sudoInfo, "status", serviceName)
	if err != nil {
		// Don't return error if service is just inactive
		if strings.Contains(stderr, "could not be found") {
			return "not-found", nil
		}
		if exitErr, ok := err.(*ssh.ExitError); ok && exitErr.ExitStatus() == 3 {
			return "inactive", nil
		}
		return "", fmt.Errorf("failed to get status: %w", err)
	}

	return s.parseServiceStatus(output), nil
}

// parseServiceStatus parses the systemctl status output
func (s *ServiceManager) parseServiceStatus(output string) string {
	if strings.Contains(output, "Active: active (running)") {
		return "running"
	}
	if strings.Contains(output, "Active: inactive") {
		return "stopped"
	}
	if strings.Contains(output, "Active: failed") {
		return "failed"
	}
	return "unknown"
}

// ListServices returns a list of all systemd services
func (s *ServiceManager) ListServices() ([]string, error) {
	// // Create new session
	// session, err := e.client.NewSession()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create session: %w", err)
	// }
	// defer session.Close()

	// var stdoutBuf, stderrBuf bytes.Buffer
	// session.Stdout = &stdoutBuf
	// session.Stderr = &stderrBuf

	// List all services using systemctl command
	// command := "systemctl list-units --type=service --all --no-pager --plain"
	// if err := session.Run(fmt.Sprintf("sudo -S -p '' %s", command)); err != nil {
	// 	return nil, fmt.Errorf("failed to list services: %w", err)
	// }
	output, _, err := s.exec.Execute("systemctl", "", s.sudoInfo, "list-units", "--type=service", "--all", "--no-pager", "--plain")
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	// Parse the output
	services := []string{}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Each line has format: "unit-name.service loaded active running Description"
		fields := strings.Fields(line)
		if len(fields) >= 1 && strings.HasSuffix(fields[0], ".service") {
			// Remove the .service suffix
			serviceName := strings.TrimSuffix(fields[0], ".service")
			services = append(services, serviceName)
		}
	}

	return services, nil
}
