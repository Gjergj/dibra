package commandexecutor

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
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
		// Input:    cmd.Input,
	}

	session, err := e.runner.NewRunnerSession(c)
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	sudoPassword := ""
	if cmd.WithSudo {
		sudoPassword = e.runner.SudoPassword()
		if sudoPassword == "" {
			return "", "", fmt.Errorf("sudo password is empty")
		}
	}

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

	go func(in io.Writer, output io.ReadCloser) {
		var (
			line string
			r    = bufio.NewReader(output)
		)
		for {
			line, err = r.ReadString(':')
			if err != nil {
				break
			}
			if strings.Contains(line, "[sudo] password for ") {
				_, err = in.Write([]byte(sudoPassword + "\n"))
				if err != nil {
					fmt.Println("failed to write password: %w", err)
					break
				}
				fmt.Println("put the password ---  end .")
				break
			}
		}
	}(stdin, errPipe)

	if err := session.Start(); err != nil {
		return "", "", fmt.Errorf("command failed to start: %w", err)
	}

	if cmd.WithSudo {
		time.Sleep(100 * time.Millisecond)
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
