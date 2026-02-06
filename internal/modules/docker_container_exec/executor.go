package docker_container_exec

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	if req.Container == "" {
		return Response{Failed: true, Msg: "container is required"}
	}

	// Parse command into argv if provided
	argv := req.Argv
	if len(argv) == 0 && req.Command != "" {
		// Simple split by spaces (proper shell parsing would need a library)
		argv = strings.Fields(req.Command)
	}

	if len(argv) == 0 {
		return Response{Failed: true, Msg: "either argv or command must be provided"}
	}

	if req.Detach && req.Stdin != "" {
		return Response{Failed: true, Msg: "if detach=true, stdin cannot be provided"}
	}

	// Build environment variables
	var envList []string
	if req.Env != nil {
		for k, v := range req.Env {
			envList = append(envList, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Create exec configuration
	execConfig := types.ExecConfig{
		User:         req.User,
		Privileged:   req.Privileged,
		Tty:          req.TTY,
		AttachStdin:  req.Stdin != "",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          argv,
		Env:          envList,
	}
	if req.Chdir != "" {
		execConfig.WorkingDir = req.Chdir
	}

	// Create exec instance
	execResp, err := cli.ContainerExecCreate(ctx, req.Container, execConfig)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create exec: %v", err)}
	}
	execID := execResp.ID

	// Start exec
	startCheck := types.ExecStartCheck{
		Detach: req.Detach,
		Tty:    req.TTY,
	}

	if req.Detach {
		err = cli.ContainerExecStart(ctx, execID, startCheck)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to start exec: %v", err)}
		}
		return Response{Changed: true, ExecID: execID, Msg: "exec started in detached mode"}
	}

	// Attach to exec
	hijack, err := cli.ContainerExecAttach(ctx, execID, startCheck)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to attach to exec: %v", err)}
	}
	defer hijack.Close()

	// Send stdin if provided
	if req.Stdin != "" {
		stdin := req.Stdin
		if req.StdinAddNewline {
			stdin += "\n"
		}
		_, err = hijack.Conn.Write([]byte(stdin))
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write stdin: %v", err)}
		}
		// Close write side to signal EOF
		hijack.CloseWrite()
	}

	// Read output
	var stdout, stderr bytes.Buffer

	if req.TTY {
		// For TTY, stdout and stderr are combined
		_, err = io.Copy(&stdout, hijack.Reader)
	} else {
		// Demux stdout and stderr using Docker's stdcopy
		_, err = stdcopy.StdCopy(&stdout, &stderr, hijack.Reader)
	}
	if err != nil && err != io.EOF {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read output: %v", err)}
	}

	// Get exit code
	inspectResp, err := cli.ContainerExecInspect(ctx, execID)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect exec: %v", err)}
	}

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	if req.StripEmptyEnds {
		stdoutStr = strings.TrimRight(stdoutStr, "\r\n")
		stderrStr = strings.TrimRight(stderrStr, "\r\n")
	}

	return Response{
		Changed: true,
		Stdout:  stdoutStr,
		Stderr:  stderrStr,
		RC:      inspectResp.ExitCode,
	}
}
