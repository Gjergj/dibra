package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConfig holds SSH connection configuration
type SSHConfig struct {
	Host         string
	Port         int
	User         string
	Password     string
	SudoPassword string
	Timeout      time.Duration
}

// SSHExecutor handles SSH connections and command execution
type SSHExecutor struct {
	config     *SSHConfig
	client     *ssh.Client
	session    *ssh.Session
	sudoWriter io.WriteCloser
}

// NewSSHExecutor creates a new SSH executor
func NewSSHExecutor(config *SSHConfig) *SSHExecutor {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Port == 0 {
		config.Port = 22
	}
	return &SSHExecutor{config: config}
}

// Connect establishes SSH connection
func (e *SSHExecutor) Connect() error {
	sshConfig := &ssh.ClientConfig{
		User: e.config.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(e.config.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Note: In production, use proper host key verification
		Timeout:         e.config.Timeout,
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", e.config.Host, e.config.Port), sshConfig)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	e.client = client
	return nil
}

// ExecuteSudoCommand executes a command with sudo
func (e *SSHExecutor) ExecuteSudoCommand(command string) error {
	if e.client == nil {
		return fmt.Errorf("not connected")
	}

	// Create new session
	session, err := e.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	e.session = session
	defer e.session.Close()

	// Setup I/O
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Get stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	e.sudoWriter = stdin

	// Prepare sudo command
	sudoCmd := fmt.Sprintf("sudo -S -p '' %s", command)

	// Start command
	if err := session.Start(sudoCmd); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Write sudo password
	if _, err := fmt.Fprintf(stdin, "%s\n", e.config.SudoPassword); err != nil {
		return fmt.Errorf("failed to write sudo password: %w", err)
	}

	// Wait for command completion
	if err := session.Wait(); err != nil {
		exitErr, ok := err.(*ssh.ExitError)
		if ok {
			return fmt.Errorf("command failed with exit code %d: %s",
				exitErr.ExitStatus(), stderrBuf.String())
		}
		return fmt.Errorf("failed to wait for command: %w", err)
	}

	return nil
}

// ExecuteSudoCommand executes a command with sudo
func (e *SSHExecutor) Execute(command string, sudoInfo *SudoInfo, params ...string) (string, error) {
	if e.client == nil {
		return "", fmt.Errorf("not connected")
	}

	// Create new session
	session, err := e.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	e.session = session
	defer e.session.Close()

	// Setup I/O
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Get stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	e.sudoWriter = stdin

	// Prepare sudo command
	sudoCmd := fmt.Sprintf("sudo -S -p '' %s", command)

	// Start command
	if err := session.Start(sudoCmd); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	// Write sudo password
	if _, err := fmt.Fprintf(stdin, "%s\n", e.config.SudoPassword); err != nil {
		return "", fmt.Errorf("failed to write sudo password: %w", err)
	}

	// Wait for command completion
	if err := session.Wait(); err != nil {
		exitErr, ok := err.(*ssh.ExitError)
		if ok {
			return "", fmt.Errorf("command failed with exit code %d: %s",
				exitErr.ExitStatus(), stderrBuf.String())
		}
		return "", fmt.Errorf("failed to wait for command: %w", err)
	}

	return stdoutBuf.String(), nil
}

// Close closes the SSH connection
func (e *SSHExecutor) Close() error {
	if e.session != nil {
		e.session.Close()
	}
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}

// ExecuteLongRunningService executes a long-running systemctl command
func (e *SSHExecutor) ExecuteLongRunningService(serviceName, action string) error {
	command := fmt.Sprintf("systemctl %s %s", action, serviceName)

	// Create new session
	session, err := e.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Setup terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // Disable echoing
		ssh.TTY_OP_ISPEED: 14400, // Input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // Output speed = 14.4kbaud
	}

	// Request pseudo terminal
	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		return fmt.Errorf("failed to request pty: %w", err)
	}

	// Setup I/O pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Start command
	if err := session.Start(fmt.Sprintf("sudo -S -p '' %s", command)); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Write sudo password
	time.Sleep(100 * time.Millisecond) // Small delay to ensure prompt is ready
	if _, err := fmt.Fprintf(stdin, "%s\n", e.config.SudoPassword); err != nil {
		return fmt.Errorf("failed to write sudo password: %w", err)
	}

	// Wait for command completion
	if err := session.Wait(); err != nil {
		exitErr, ok := err.(*ssh.ExitError)
		if ok {
			return fmt.Errorf("command failed with exit code %d: %s",
				exitErr.ExitStatus(), stderrBuf.String())
		}
		return fmt.Errorf("failed to wait for command: %w", err)
	}

	return nil
}

// MonitorService monitors a service status via SSH
func (e *SSHExecutor) MonitorService(serviceName string, interval time.Duration) (<-chan string, <-chan error) {
	statusChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(statusChan)
		defer close(errChan)

		for {
			status, err := e.getServiceStatus(serviceName)
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
func (e *SSHExecutor) getServiceStatus(serviceName string) (string, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	command := fmt.Sprintf("systemctl status %s", serviceName)
	if err := session.Run(fmt.Sprintf("sudo -S -p '' %s", command)); err != nil {
		// Don't return error if service is just inactive
		if strings.Contains(stderrBuf.String(), "could not be found") {
			return "not-found", nil
		}
		if exitErr, ok := err.(*ssh.ExitError); ok && exitErr.ExitStatus() == 3 {
			return "inactive", nil
		}
		return "", fmt.Errorf("failed to get status: %w", err)
	}

	return parseServiceStatus(stdoutBuf.String()), nil
}

// parseServiceStatus parses the systemctl status output
func parseServiceStatus(output string) string {
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
func (e *SSHExecutor) ListServices() ([]string, error) {
	// Create new session
	session, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// List all services using systemctl command
	command := "systemctl list-units --type=service --all --no-pager --plain"
	if err := session.Run(fmt.Sprintf("sudo -S -p '' %s", command)); err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	// Parse the output
	services := []string{}
	for _, line := range strings.Split(stdoutBuf.String(), "\n") {
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
