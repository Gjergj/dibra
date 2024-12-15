package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"golang.org/x/crypto/ssh"
)

type ServiceManager struct {
	exec     *commandexecutor.CommandRunner
	WithSudo bool
}

type ServiceUnit struct {
	Name        string
	Description string
	ExecStart   string
	WorkingDir  string
	User        string
	RestartSec  int
	// Additional systemd unit options
	Environment map[string]string
	Restart     string // e.g., "always", "on-failure"
	WantedBy    string // e.g., "multi-user.target"
}

// NewServiceManager creates a new ServiceManager instance
func NewServiceManager(exec *commandexecutor.CommandRunner, withSudo bool) *ServiceManager {
	return &ServiceManager{exec: exec, WithSudo: withSudo}
}

// MonitorService monitors a service status via SSH
func (s *ServiceManager) MonitorService(serviceName string, interval time.Duration, iterations int) (<-chan string, <-chan error) {
	statusChan := make(chan string)
	errChan := make(chan error, 1)

	if iterations == 0 {
		//indefinitely
		iterations = 1000000000
	}

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

			iterations--
			if iterations == 0 {
				if len(statusChan) > 0 || len(errChan) > 0 {
					// wait for the channel to be empty before returning
					time.Sleep(1 * time.Millisecond)
				}
				return
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
	output, stderr, err := s.exec.Execute(commandexecutor.Command{
		Command:  "systemctl",
		Args:     []string{"status", serviceName},
		WithSudo: s.WithSudo,
	})
	if err != nil {
		// Don't return error if service is just inactive
		if strings.Contains(stderr, "could not be found") {
			return "not-found", nil
		}
		// replace the following with errors.As(err, &exitErr)
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitStatus() == 3 {
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

	cmd := commandexecutor.Command{
		Command:  "systemctl",
		Args:     []string{"list-units", "--type=service", "--all", "--no-pager", "--plain"},
		WithSudo: s.WithSudo,
	}

	output, _, err := s.exec.Execute(cmd)
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

// CreateServiceUnit only creates a new systemd service unit file if it doesn't exist, doesn't install it
func (s *ServiceManager) CreateServiceUnit(unit ServiceUnit, force bool) error {
	if unit.Name == "" || unit.ExecStart == "" {
		return fmt.Errorf("service name and ExecStart are required")
	}

	existingUnitFile, _ := s.getServiceUnitFile(unit.Name)

	fileName := fmt.Sprintf("%s.service", unit.Name)
	filePath := fmt.Sprintf("/etc/systemd/system/%s", fileName)

	// Check if service file already exists
	checkCmd := commandexecutor.Command{
		Command:  "test",
		Args:     []string{"-f", filePath},
		WithSudo: s.WithSudo,
	}

	_, _, err := s.exec.Execute(checkCmd)
	if err == nil && !force {
		return fmt.Errorf("service unit file %s already exists. Use force=true to overwrite", fileName)
	}

	// Create unit file content
	unitContent := fmt.Sprintf(`[Unit]
Description=%s

[Service]
ExecStart=%s
`, unit.Description, unit.ExecStart)

	if unit.WorkingDir != "" {
		unitContent += fmt.Sprintf("WorkingDirectory=%s\n", unit.WorkingDir)
	}
	if unit.User != "" {
		unitContent += fmt.Sprintf("User=%s\n", unit.User)
	}
	if len(unit.Environment) > 0 {
		for _, env := range unit.Environment {
			unitContent += fmt.Sprintf("Environment=%s\n", env)
		}
	}
	if unit.Restart != "" {
		unitContent += fmt.Sprintf("Restart=%s\n", unit.Restart)
	}
	if unit.RestartSec != 0 {
		unitContent += fmt.Sprintf("RestartSec=%d\n", unit.RestartSec)
	}

	unitContent += "\n[Install]\n"
	if unit.WantedBy != "" {
		unitContent += fmt.Sprintf("WantedBy=%s\n", unit.WantedBy)
	} else {
		unitContent += "WantedBy=multi-user.target\n"
	}

	if existingUnitFile != unitContent {
		// Write unit file to /etc/systemd/system/
		fmt.Printf("Writing unit file to %s\n", filePath)
		cmd := commandexecutor.Command{
			Command:  "tee",
			Args:     []string{filePath},
			Input:    unitContent,
			WithSudo: s.WithSudo,
		}

		stdOut, stderr, err := s.exec.Execute(cmd)
		if err != nil {
			return fmt.Errorf("failed to create service unit file: %w %s %s", err, stdOut, stderr)
		}
		if stderr != "" {
			return fmt.Errorf("error creating service unit file: %s", stderr)
		}
	} else {
		fmt.Printf("Service unit file %s already exists and is up to date\n", filePath)
	}

	return nil
}

// InstallService installs and enables a service, but doesn't create the unit file
func (s *ServiceManager) InstallService(serviceName string) error {
	// Reload systemd daemon first
	if err := s.reloadDaemon(); err != nil {
		return fmt.Errorf("failed to reload daemon: %w", err)
	}

	// Enable the service
	cmd := commandexecutor.Command{
		Command:  "systemctl",
		Args:     []string{"enable", serviceName},
		WithSudo: s.WithSudo,
	}

	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error enabling service: %s", stderr)
	}

	return nil
}

// StartService starts a systemd service
func (s *ServiceManager) MakeExecutable(path string) error {
	cmd := commandexecutor.Command{
		Command:  "chmod",
		Args:     []string{"+x", path},
		WithSudo: s.WithSudo,
	}

	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("failed to make executable: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error making executable: %s", stderr)
	}

	return nil
}

// StartService starts a systemd service
func (s *ServiceManager) StartService(serviceName string) error {
	cmd := commandexecutor.Command{
		Command:  "systemctl",
		Args:     []string{"start", serviceName},
		WithSudo: s.WithSudo,
	}

	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error starting service: %s", stderr)
	}

	return nil
}

// StopService stops a systemd service
func (s *ServiceManager) StopService(serviceName string) error {
	cmd := commandexecutor.Command{
		Command:  "systemctl",
		Args:     []string{"stop", serviceName},
		WithSudo: s.WithSudo,
	}

	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error stopping service: %s", stderr)
	}

	return nil
}

// reloadDaemon reloads the systemd daemon
func (s *ServiceManager) reloadDaemon() error {
	fmt.Println("Reloading systemd daemon")
	cmd := commandexecutor.Command{
		Command:  "systemctl",
		Args:     []string{"daemon-reload"},
		WithSudo: s.WithSudo,
	}

	_, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return fmt.Errorf("failed to reload daemon: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("error reloading daemon: %s", stderr)
	}

	return nil
}

func (s *ServiceManager) getServiceUnitFile(serviceName string) (string, error) {

	fileName := fmt.Sprintf("%s.service", serviceName)
	filePath := fmt.Sprintf("/etc/systemd/system/%s", fileName)

	// Check if service file exists
	checkCmd := commandexecutor.Command{
		Command:  "test",
		Args:     []string{"-f", filePath},
		WithSudo: s.WithSudo,
	}

	_, _, err := s.exec.Execute(checkCmd)
	if err != nil {
		return "", fmt.Errorf("service unit file %s does not exist", filePath)
	}

	// Read the service file
	cmd := commandexecutor.Command{
		Command:  "cat",
		Args:     []string{filePath},
		WithSudo: s.WithSudo,
	}

	output, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to read service unit file: %w", err)
	}
	if stderr != "" {
		return "", fmt.Errorf("error reading service unit file: %s", stderr)
	}

	return output, nil
}

// GetServiceUnit reads and parses an existing systemd service unit file
func (s *ServiceManager) GetServiceUnit(serviceName string) (*ServiceUnit, error) {

	output, err := s.getServiceUnitFile(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to read service unit file: %w", err)
	}

	// Parse the service file content
	unit := &ServiceUnit{
		Name: serviceName,
	}

	var currentSection string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for section headers [Unit], [Service], [Install]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.Trim(line, "[]")
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch currentSection {
		case "Unit":
			if key == "Description" {
				unit.Description = value
			}
		case "Service":
			switch key {
			case "ExecStart":
				unit.ExecStart = value
			case "WorkingDirectory":
				unit.WorkingDir = value
			case "User":
				unit.User = value
			case "Environment":
				envs := strings.Split(value, "=")
				if len(envs) > 1 {
					envs[0] = strings.TrimSpace(envs[0])
					envs[1] = strings.TrimSpace(envs[1])
					unit.Environment[envs[0]] = envs[1]
				}
			case "Restart":
				unit.Restart = value
			}
		case "Install":
			if key == "WantedBy" {
				unit.WantedBy = value
			}
		}
	}

	// Validate required fields
	if unit.ExecStart == "" {
		return nil, fmt.Errorf("invalid service unit file: missing ExecStart")
	}

	return unit, nil
}

// LogOptions represents options for retrieving service logs
type LogOptions struct {
	Since time.Time // Show logs since this time
	Until time.Time // Show logs until this time
	Lines int       // Number of lines to show (tail)
	// Follow bool      // Follow log output
}

// GetServiceLogs retrieves logs for a service with the specified options
func (s *ServiceManager) GetServiceLogs(serviceName string, options LogOptions) (string, error) {
	args := []string{"-u", serviceName}

	// Add time range filters if specified
	if !options.Since.IsZero() {
		args = append(args, fmt.Sprintf("--since=%s", options.Since.Format("2006-01-02 15:04:05")))
	}
	if !options.Until.IsZero() {
		args = append(args, fmt.Sprintf("--until=%s", options.Until.Format("2006-01-02 15:04:05")))
	}

	// Add line limit if specified
	if options.Lines > 0 {
		args = append(args, fmt.Sprintf("-n%d", options.Lines))
	}

	// Add follow flag if specified
	// if options.Follow {
	// 	args = append(args, "-f")
	// }

	cmd := commandexecutor.Command{
		Command: "journalctl",
		Args:    args,
		// WithSudo: s.WithSudo,
	}

	output, stderr, err := s.exec.Execute(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to get service logs: %w: %s", err, stderr)
	}

	return output, nil
}

// GetRecentLogs is a convenience method to get the last n lines of logs
func (s *ServiceManager) GetRecentLogs(serviceName string, lines int) (string, error) {
	return s.GetServiceLogs(serviceName, LogOptions{
		Lines: lines,
	})
}

// GetLogsInTimeRange is a convenience method to get logs within a time range
func (s *ServiceManager) GetLogsInTimeRange(serviceName string, since, until time.Time) (string, error) {
	return s.GetServiceLogs(serviceName, LogOptions{
		Since: since,
		Until: until,
	})
}

// // FollowLogs is a convenience method to follow service logs in real-time
// func (s *ServiceManager) FollowLogs(serviceName string) (string, error) {
// 	return s.GetServiceLogs(serviceName, LogOptions{
// 		Follow: true,
// 	})
// }
