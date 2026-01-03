package commandexecutor

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
	"golang.org/x/crypto/ssh"
)

// type CommandExecutor interface {
// 	Execute(command string, env []string, sudoInfo *SudoInfo, args ...string) (string, string, error)
// }

const EOF KeyPress = "\x04" // EOF character CTRL-D

type KeyPress string

type Command struct {
	Command  string
	Args     []string
	Env      map[string]string
	WithSudo bool
	Input    string
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

type CommandRunner struct {
	runner cmdrunner.Runner
}

type SudoInfo struct {
	Password string
}

func NewCommandRunner(runner cmdrunner.Runner) *CommandRunner {
	return &CommandRunner{runner: runner}
}

func (e *CommandRunner) Execute(cmd Command) (string, string, error) {
	c := cmdrunner.Command{
		Command:  cmd.Command,
		Args:     cmd.Args,
		Env:      cmd.Env,
		WithSudo: cmd.WithSudo,
	}

	session, err := e.runner.NewRunnerSession(c)
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Validate sudo password if needed
	sudoPassword := ""
	if cmd.WithSudo {
		sudoPassword = e.runner.SudoPassword()
		if sudoPassword == "" {
			return "", "", fmt.Errorf("sudo password is empty")
		}
	}

	// Get I/O pipes
	stdout, err := session.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	// Buffers to capture output
	var stdoutBuf, stderrBuf bytes.Buffer

	// Channel to signal sudo password handling completion
	sudoDone := make(chan bool, 1)

	// Goroutine to handle sudo password prompt
	if cmd.WithSudo {
		go func() {
			defer func() { sudoDone <- true }()
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				// Write the line to stderr buffer
				stderrBuf.WriteString(line + "\n")

				// Check for sudo password prompt
				if strings.Contains(line, "[sudo] password for ") {
					// Write password to stdin
					_, err := stdin.Write([]byte(sudoPassword + "\n"))
					if err != nil {
						fmt.Printf("failed to write sudo password: %v\n", err)
					}
					return
				}
			}
		}()
	}

	// Goroutine to continuously read stdout
	stdoutDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&stdoutBuf, stdout)
		stdoutDone <- err
	}()

	// Goroutine to continuously read stderr (if not handling sudo)
	stderrDone := make(chan error, 1)
	if !cmd.WithSudo {
		go func() {
			_, err := io.Copy(&stderrBuf, stderr)
			stderrDone <- err
		}()
	}

	// Start the command
	if err := session.Start(); err != nil {
		return "", "", fmt.Errorf("command start failed: %w", err)
	}

	// Wait for sudo password to be entered if needed
	if cmd.WithSudo {
		select {
		case <-sudoDone:
			// Sudo password handled, now read remaining stderr
			go func() {
				_, err := io.Copy(&stderrBuf, stderr)
				stderrDone <- err
			}()
		case <-time.After(2 * time.Second):
			// Timeout waiting for sudo prompt - might not be needed
			go func() {
				_, err := io.Copy(&stderrBuf, stderr)
				stderrDone <- err
			}()
		}
	}

	// Write input if provided
	if cmd.Input != "" {
		if _, err := stdin.Write([]byte(cmd.Input)); err != nil {
			// Don't fail the entire command, just log the error
			fmt.Printf("failed to write input: %v\n", err)
		}
	}
	stdin.Close()

	// Wait for command to complete
	waitErr := session.Wait()

	// Wait for output goroutines to finish
	<-stdoutDone
	<-stderrDone

	// Always return stdout and stderr, even on error
	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	// Extract exit code from error
	exitCode := 0
	if waitErr != nil {
		// Check if it's an SSH exit error
		if exitErr, ok := waitErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			// For other errors (e.g., connection issues), return -1
			exitCode = -1
		}
		return stdoutStr, stderrStr, fmt.Errorf("command execution failed with exit code %d: %w", exitCode, waitErr)
	}

	return stdoutStr, stderrStr, nil
}

// ExecuteWithExitCode executes a command and returns detailed results including exit code
func (e *CommandRunner) ExecuteWithExitCode(cmd Command) CommandResult {
	c := cmdrunner.Command{
		Command:  cmd.Command,
		Args:     cmd.Args,
		Env:      cmd.Env,
		WithSudo: cmd.WithSudo,
	}

	session, err := e.runner.NewRunnerSession(c)
	if err != nil {
		return CommandResult{
			ExitCode: -1,
			Err:      fmt.Errorf("failed to create session: %w", err),
		}
	}
	defer session.Close()

	// Validate sudo password if needed
	sudoPassword := ""
	if cmd.WithSudo {
		sudoPassword = e.runner.SudoPassword()
		if sudoPassword == "" {
			return CommandResult{
				ExitCode: -1,
				Err:      fmt.Errorf("sudo password is empty"),
			}
		}
	}

	// Get I/O pipes
	stdout, err := session.StdoutPipe()
	if err != nil {
		return CommandResult{
			ExitCode: -1,
			Err:      fmt.Errorf("failed to get stdout pipe: %w", err),
		}
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return CommandResult{
			ExitCode: -1,
			Err:      fmt.Errorf("failed to get stderr pipe: %w", err),
		}
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return CommandResult{
			ExitCode: -1,
			Err:      fmt.Errorf("failed to get stdin pipe: %w", err),
		}
	}

	// Buffers to capture output
	var stdoutBuf, stderrBuf bytes.Buffer

	// Channel to signal sudo password handling completion
	sudoDone := make(chan bool, 1)

	// Goroutine to handle sudo password prompt
	if cmd.WithSudo {
		go func() {
			defer func() { sudoDone <- true }()
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				// Write the line to stderr buffer
				stderrBuf.WriteString(line + "\n")

				// Check for sudo password prompt
				if strings.Contains(line, "[sudo] password for ") {
					// Write password to stdin
					_, err := stdin.Write([]byte(sudoPassword + "\n"))
					if err != nil {
						fmt.Printf("failed to write sudo password: %v\n", err)
					}
					return
				}
			}
		}()
	}

	// Goroutine to continuously read stdout
	stdoutDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&stdoutBuf, stdout)
		stdoutDone <- err
	}()

	// Goroutine to continuously read stderr (if not handling sudo)
	stderrDone := make(chan error, 1)
	if !cmd.WithSudo {
		go func() {
			_, err := io.Copy(&stderrBuf, stderr)
			stderrDone <- err
		}()
	}

	// Start the command
	if err := session.Start(); err != nil {
		return CommandResult{
			ExitCode: -1,
			Err:      fmt.Errorf("command start failed: %w", err),
		}
	}

	// Wait for sudo password to be entered if needed
	if cmd.WithSudo {
		select {
		case <-sudoDone:
			// Sudo password handled, now read remaining stderr
			go func() {
				_, err := io.Copy(&stderrBuf, stderr)
				stderrDone <- err
			}()
		case <-time.After(2 * time.Second):
			// Timeout waiting for sudo prompt - might not be needed
			go func() {
				_, err := io.Copy(&stderrBuf, stderr)
				stderrDone <- err
			}()
		}
	}

	// Write input if provided
	if cmd.Input != "" {
		if _, err := stdin.Write([]byte(cmd.Input)); err != nil {
			// Don't fail the entire command, just log the error
			fmt.Printf("failed to write input: %v\n", err)
		}
	}
	stdin.Close()

	// Wait for command to complete
	waitErr := session.Wait()

	// Wait for output goroutines to finish
	<-stdoutDone
	<-stderrDone

	// Build result
	result := CommandResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: 0,
	}

	// Extract exit code from error
	if waitErr != nil {
		// Check if it's an SSH exit error
		if exitErr, ok := waitErr.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			// For other errors (e.g., connection issues), return -1
			result.ExitCode = -1
		}
		result.Err = waitErr
	}

	return result
}

func (e *CommandRunner) ExecuteCombinedOutput(cmd Command) (string, error) {
	c := cmdrunner.Command{
		Command:  cmd.Command,
		Args:     cmd.Args,
		Env:      cmd.Env,
		WithSudo: cmd.WithSudo,
		// Input:    cmd.Input,
	}

	session, err := e.runner.NewRunnerSession(c)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	combinedOutput, err := session.CombinedOutput()
	return string(combinedOutput), err
}
