package docker_compose_v2_run

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/google/shlex"
)

func Execute(req Request) Response {
	// Check if directory exists
	if _, err := os.Stat(req.ProjectSrc); os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("project_src does not exist: %s", req.ProjectSrc)}
	}

	cmdEnv := docker.GetComposeEnv(req.ComposeCommonArgs, req.CommonArgs)

	// Base args
	args := docker.GetComposeBaseArgs(req.ComposeCommonArgs)
	args = append(args, "run")

	if req.Build {
		args = append(args, "--build")
	}
	for _, cap := range req.CapAdd {
		args = append(args, "--cap-add", cap)
	}
	for _, cap := range req.CapDrop {
		args = append(args, "--cap-drop", cap)
	}
	if req.EntryPoint != "" {
		args = append(args, "--entrypoint", req.EntryPoint)
	}
	if req.Interactive != nil && !*req.Interactive {
		args = append(args, "--no-interactive")
	}
	for _, label := range req.Labels {
		args = append(args, "--label", label)
	}
	if req.Name != "" {
		args = append(args, "--name", req.Name)
	}
	if req.NoDeps {
		args = append(args, "--no-deps")
	}
	for _, publish := range req.Publish {
		args = append(args, "--publish", publish)
	}
	if req.QuietPull {
		args = append(args, "--quiet-pull")
	}
	if req.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	if req.Cleanup {
		args = append(args, "--rm")
	}
	if req.ServicePorts {
		args = append(args, "--service-ports")
	}
	if req.UseAliases {
		args = append(args, "--use-aliases")
	}
	for _, volume := range req.Volumes {
		args = append(args, "--volume", volume)
	}
	if req.Chdir != "" {
		args = append(args, "--workdir", req.Chdir)
	}
	if req.Detach {
		args = append(args, "--detach")
	}
	if req.User != "" {
		args = append(args, "--user", req.User)
	}
	if req.TTY != nil && !*req.TTY {
		args = append(args, "--no-TTY")
	}

	// Environment variables for the container
	for k, v := range req.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, "--", req.Service)

	// Command
	argv := req.Argv
	if len(argv) == 0 && req.Command != "" {
		var err error
		argv, err = shlex.Split(req.Command)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to parse command: %v", err)}
		}
	}
	args = append(args, argv...)

	cmd := exec.Command("docker", args...)
	cmd.Dir = req.ProjectSrc
	cmd.Env = cmdEnv

	if req.Stdin != "" && !req.Detach {
		stdin := req.Stdin
		if req.StdinAddNewline {
			stdin += "\n"
		}
		cmd.Stdin = strings.NewReader(stdin)
	}

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return Response{
				Failed:  false, // It didn't fail to run, the command just returned non-zero
				Changed: true,
				RC:      exitErr.ExitCode(),
				Stdout:  outputStr,
				Msg:     "command executed with non-zero exit code",
			}
		}
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("failed to execute command: %v", err),
			Stdout: outputStr,
		}
	}

	if req.Detach {
		return Response{
			Changed:     true,
			ContainerID: strings.TrimSpace(outputStr),
			Msg:         "command started in detached mode",
		}
	}

	stdout := outputStr
	if req.StripEmptyEnds {
		stdout = strings.TrimRight(stdout, "\r\n")
	}

	return Response{
		Changed: true,
		Stdout:  stdout,
		RC:      0,
		Msg:     "command executed successfully",
	}
}
