package docker_container_exec

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/google/shlex"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if err := validateRequest(req, dependencies.Environment); err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	argv := req.Argv
	if req.argumentProvided("command", req.Command != "") {
		argv, err = shlex.Split(req.Command)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to parse command: %v", err)}
		}
	}

	var envList []string
	if req.Env != nil {
		names := make([]string, 0, len(req.Env))
		for name := range req.Env {
			names = append(names, name)
		}
		sort.Strings(names)
		envList = make([]string, 0, len(names))
		for _, name := range names {
			envList = append(envList, fmt.Sprintf("%s=%s", name, req.Env[name].(string)))
		}
	}

	stdin := ""
	if req.Stdin != nil {
		stdin = *req.Stdin
		if defaultTrue(req.StdinAddNewline) {
			stdin += "\n"
		}
	}

	execConfig := client.ExecCreateOptions{
		User:         req.User,
		Privileged:   req.Privileged,
		TTY:          req.TTY,
		AttachStdin:  stdin != "",
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          argv,
		Env:          envList,
	}
	if req.Chdir != "" {
		execConfig.WorkingDir = req.Chdir
	}

	execResp, err := cli.ExecCreate(ctx, req.Container, execConfig)
	if err != nil {
		return engineErrorResponse(req.Container, err)
	}
	execID := execResp.ID

	startOptions := client.ExecStartOptions{
		Detach: req.Detach,
		TTY:    req.TTY,
	}

	if req.Detach {
		_, err = cli.ExecStart(ctx, execID, startOptions)
		if err != nil {
			return engineErrorResponse(req.Container, err)
		}
		return Response{Changed: true, ExecID: execID}
	}

	hijack, err := cli.ExecAttach(ctx, execID, client.ExecAttachOptions{TTY: req.TTY})
	if err != nil {
		return engineErrorResponse(req.Container, err)
	}
	defer hijack.Close()

	var writeResult <-chan error
	if stdin != "" {
		result := make(chan error, 1)
		writeResult = result
		go func() {
			_, writeErr := io.WriteString(hijack.Conn, stdin)
			closeErr := hijack.CloseWrite()
			if writeErr != nil {
				result <- writeErr
				return
			}
			result <- closeErr
		}()
	}

	var stdout, stderr bytes.Buffer
	if req.TTY {
		_, err = io.Copy(&stdout, hijack.Reader)
	} else {
		_, err = stdcopy.StdCopy(&stdout, &stderr, hijack.Reader)
	}
	if err != nil && err != io.EOF {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read output: %v", err)}
	}
	if writeResult != nil {
		if err := <-writeResult; err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write stdin: %v", err)}
		}
	}

	inspectResp, err := cli.ExecInspect(ctx, execID, client.ExecInspectOptions{})
	if err != nil {
		return engineErrorResponse(req.Container, err)
	}

	stdoutStr := stdout.String()
	stderrStr := stderr.String()
	if defaultTrue(req.StripEmptyEnds) {
		stdoutStr = strings.TrimRight(stdoutStr, "\r\n")
		stderrStr = strings.TrimRight(stderrStr, "\r\n")
	}
	exitCode := inspectResp.ExitCode

	return Response{
		Changed: true,
		Stdout:  &stdoutStr,
		Stderr:  &stderrStr,
		RC:      &exitCode,
	}
}

func engineErrorResponse(container string, err error) Response {
	switch {
	case docker.IsNotFoundError(err):
		return Response{Failed: true, Msg: fmt.Sprintf("Could not find container %q", container)}
	case docker.IsConflictError(err):
		return Response{Failed: true, Msg: fmt.Sprintf("The container %q has been paused (%v)", container, err)}
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("An unexpected Docker error occurred: %v", err)}
	}
}
