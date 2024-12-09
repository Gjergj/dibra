package commandexecutor

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
func (e *SSHExecutor) Execute(command string, env []string, sudoInfo *SudoInfo, params ...string) (string, string, error) {
	if e.client == nil {
		return "", "", fmt.Errorf("not connected")
	}

	// Create new session
	session, err := e.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	e.session = session

	// Set session env vars
	var ev []string
	for _, value := range env {
		ev = strings.Split(value, "=")
		if err = session.Setenv(ev[0], strings.Join(ev[1:], "=")); err != nil {
			return "", "", fmt.Errorf("failed to set env var: %w", err)
		}
	}

	defer e.session.Close()

	// Setup I/O
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Get stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	if sudoInfo != nil {
		e.sudoWriter = stdin
		command = fmt.Sprintf("sudo -S -p '' %s", command)
	}

	args := append([]string{command}, params...)
	command = strings.Join(args, " ")

	// Start command
	if err := session.Start(command); err != nil {
		return "", "", fmt.Errorf("failed to start command: %w", err)
	}

	if sudoInfo != nil {
		// Write sudo password
		if _, err := fmt.Fprintf(stdin, "%s\n", sudoInfo.Password); err != nil {
			return "", "", fmt.Errorf("failed to write sudo password: %w", err)
		}
	}
	// stdin.Close()
	// if keyPress != "" {

	// 	// _, err := stdin.Write([]byte(keyPress))
	// 	// if err != nil {
	// 	// 	return "", "", fmt.Errorf("failed to write key press: %w", err)
	// 	// }
	// 	// Write key press
	// 	// if _, err := fmt.Fprintf(stdin, "%s", keyPress); err != nil {
	// 	// 	return "", "", fmt.Errorf("failed to write key press: %w", err)
	// 	// }
	// }

	// Wait for command completion
	if err := session.Wait(); err != nil {
		exitErr, ok := err.(*ssh.ExitError)
		if ok {
			return "", "", fmt.Errorf("command failed with exit code %d: %s",
				exitErr.ExitStatus(), stderrBuf.String())
		}
		return "", "", fmt.Errorf("failed to wait for command: %w", err)
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
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
