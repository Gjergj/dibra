package commandexecutor

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type OSCommandExecutor struct{}

func (e *OSCommandExecutor) Execute(command string, keyPress KeyPress, sudoInfo *SudoInfo, args ...string) (string, string, error) {
	args = append([]string{command}, args...)
	command = strings.Join(args, " ")

	var cmd *exec.Cmd
	if sudoInfo == nil {
		if keyPress != "" {
			cmd = exec.Command(command)
			cmd.Stdin = strings.NewReader(string(keyPress))
		} else {
			cmd = exec.Command(command)
		}
	} else {
		cmd = exec.Command("sudo", "-S", "-p", "", command)
		if keyPress != "" {
			cmd.Stdin = strings.NewReader(sudoInfo.Password + string(keyPress))
		} else {
			cmd.Stdin = strings.NewReader(sudoInfo.Password + "\n")
		}
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("command failed to start: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return "", "", fmt.Errorf("command failed: %w", err)
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
}
