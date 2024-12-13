package commandexecutor

import (
	"bytes"
	"fmt"
	"io"

	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner/cmdsession"
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
	SudoInfo *SudoInfo
	Input    string
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
		Command: cmd.Command,
		Args:    cmd.Args,
		Env:     cmd.Env,
	}
	if cmd.SudoInfo != nil {
		c.SudoInfo = &cmdsession.SudoInfo{Password: cmd.SudoInfo.Password}
	}
	session, err := e.runner.NewRunnerSession(c)
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Setup I/O
	var stdoutBuf, stderrBuf bytes.Buffer
	// session.Stdout = &stdoutBuf
	// session.Stderr = &stderrBuf

	outpipe, err := session.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	defer outpipe.Close()

	errPipe, err := session.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}
	defer errPipe.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	defer stdin.Close()

	if err := session.Start(); err != nil {
		return "", "", fmt.Errorf("command failed to start: %w", err)
	}

	if cmd.Input != "" {
		stdin.Write([]byte(cmd.Input))
		stdin.Close()
	}

	if err := session.Wait(); err != nil {
		return "", "", fmt.Errorf("command failed: %w", err)
	}

	_, err = io.Copy(&stdoutBuf, outpipe)
	if err != nil {
		return "", "", fmt.Errorf("failed to copy stdout: %w", err)
	}
	_, err = io.Copy(&stderrBuf, errPipe)
	if err != nil {
		return "", "", fmt.Errorf("failed to copy stderr: %w", err)
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
}
