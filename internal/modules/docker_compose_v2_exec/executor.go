package docker_compose_v2_exec

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/google/shlex"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if err := validateRequest(req); err != nil {
		return failedResponse(err.Error())
	}

	projectSrc, cleanupDir, err := docker.PrepareComposeProject(req.ComposeCommonArgs, dependencies.FileSystem, dependencies.Clock)
	if err != nil {
		return failedResponse(err.Error())
	}
	if cleanupDir != "" {
		defer func() { _ = dependencies.FileSystem.RemoveAll(cleanupDir) }()
	}
	req.ProjectSrc = projectSrc
	if absolute, absErr := dependencies.FileSystem.Abs(projectSrc); absErr == nil {
		req.ProjectSrc = absolute
		projectSrc = absolute
	}

	dockerCLI := req.DockerCLI
	if dockerCLI == "" {
		dockerCLI = "docker"
	}
	if _, err := docker.CheckComposeVersionWithCLI(context.Background(), dependencies.CLIRunner, req.CommonArgs, dependencies.Environment, dockerCLI); err != nil {
		return failedResponse(err.Error())
	}

	cmdEnv, err := docker.GetComposeEnvWithEnvironment(req.ComposeCommonArgs, req.CommonArgs, dependencies.Environment)
	if err != nil {
		return failedResponse(fmt.Sprintf("invalid Docker connection options: %v", err))
	}
	args, err := docker.GetComposeProjectArgsWithProgressEnvironment(req.ComposeCommonArgs, req.CommonArgs, dependencies.Environment, "plain")
	if err != nil {
		return failedResponse(fmt.Sprintf("invalid Docker connection options: %v", err))
	}
	args = append(args, "exec")

	if req.Index != nil {
		args = append(args, "--index", strconv.Itoa(*req.Index))
	}
	if req.Chdir != nil {
		args = append(args, "--workdir", *req.Chdir)
	}
	if req.Detach {
		args = append(args, "--detach")
	}
	if req.User != nil {
		args = append(args, "--user", *req.User)
	}
	if req.Privileged {
		args = append(args, "--privileged")
	}
	if !boolDefault(req.TTY, true) {
		args = append(args, "--no-tty")
	}

	envNames := make([]string, 0, len(req.Env))
	for name := range req.Env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	for _, name := range envNames {
		args = append(args, "--env", fmt.Sprintf("%s=%s", name, req.Env[name]))
	}

	args = append(args, "--", req.Service)
	argv := req.Argv
	if req.Command != nil {
		argv, err = shlex.Split(*req.Command)
		if err != nil {
			return failedResponse(fmt.Sprintf("failed to parse command: %v", err))
		}
	}
	args = append(args, argv...)

	var stdin io.Reader
	if req.Stdin != nil {
		stdinValue := *req.Stdin
		if boolDefault(req.StdinAddNewline, true) {
			stdinValue += "\n"
		}
		stdin = strings.NewReader(stdinValue)
	}

	result, err := dependencies.CLIRunner.Run(context.Background(), docker.CLICommand{
		Name:  dockerCLI,
		Args:  args,
		Dir:   projectSrc,
		Env:   cmdEnv,
		Stdin: stdin,
	})
	if err != nil && result.ExitCode < 0 {
		return failedResponse(fmt.Sprintf("failed to execute command: %v", err))
	}
	if req.Detach {
		if result.ExitCode != 0 {
			message := strings.TrimSpace(string(result.Stderr))
			if message == "" {
				message = fmt.Sprintf("Return code %d is non-zero", result.ExitCode)
			}
			return failedResponse(message)
		}
		return Response{}
	}

	stdout := string(result.Stdout)
	stderr := string(result.Stderr)
	if boolDefault(req.StripEmptyEnds, true) {
		stdout = strings.TrimRight(stdout, "\r\n")
		stderr = strings.TrimRight(stderr, "\r\n")
	}
	rc := result.ExitCode
	return Response{
		Changed: true,
		Stdout:  &stdout,
		Stderr:  &stderr,
		RC:      &rc,
	}
}

func validateRequest(req Request) error {
	if len(req.Definition) > 0 && req.ProjectSrc != "" {
		return fmt.Errorf("parameters are mutually exclusive: definition|project_src")
	}
	if len(req.Definition) > 0 && len(req.Files) > 0 {
		return fmt.Errorf("parameters are mutually exclusive: definition|files")
	}
	if len(req.Definition) == 0 && req.ProjectSrc == "" {
		return fmt.Errorf("one of the following is required: definition, project_src")
	}
	if len(req.Definition) > 0 && req.ProjectName == "" {
		return fmt.Errorf("project_name is required when definition is used")
	}
	if req.Service == "" {
		return fmt.Errorf("service is required")
	}
	if req.Command != nil && len(req.Argv) > 0 {
		return fmt.Errorf("parameters are mutually exclusive: argv|command")
	}
	if req.Command == nil && len(req.Argv) == 0 {
		return fmt.Errorf("one of the following is required: argv, command")
	}
	if req.Detach && req.Stdin != nil {
		return fmt.Errorf("If detach=true, stdin cannot be provided.")
	}
	for name, value := range req.Env {
		if _, ok := value.(string); !ok {
			return fmt.Errorf(
				"Non-string value found for env option. Ambiguous env options must be wrapped in quotes to avoid them being interpreted when directly specified in YAML, or explicitly converted to strings when the option is templated. Key: %s",
				name,
			)
		}
	}
	return nil
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
