package docker_compose_v2_pull

import (
	"context"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type pullManager struct {
	request      Request
	dependencies docker.Dependencies
	checkMode    bool
	dockerCLI    string
	projectSrc   string
	cleanupDir   string
	cmdEnv       []string
	baseArgs     []string
}

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()
	manager, failure := newPullManager(req, dependencies, state)
	if failure != nil {
		return *failure
	}
	if manager.cleanupDir != "" {
		defer func() { _ = dependencies.FileSystem.RemoveAll(manager.cleanupDir) }()
	}
	return manager.run()
}

func newPullManager(req Request, dependencies docker.Dependencies, state execution.State) (*pullManager, *Response) {
	if err := validateRequest(&req); err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	projectSrc, cleanupDir, err := docker.PrepareComposeProject(req.ComposeCommonArgs, dependencies.FileSystem, dependencies.Clock)
	if err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	req.ProjectSrc = projectSrc
	if abs, absErr := dependencies.FileSystem.Abs(projectSrc); absErr == nil {
		req.ProjectSrc = abs
		projectSrc = abs
	}

	dockerCLI := req.DockerCLI
	if dockerCLI == "" {
		dockerCLI = "docker"
	}
	if _, err := docker.CheckComposeVersionWithCLI(context.Background(), dependencies.CLIRunner, req.CommonArgs, dependencies.Environment, dockerCLI); err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	cmdEnv, err := docker.GetComposeEnvWithEnvironment(req.ComposeCommonArgs, req.CommonArgs, dependencies.Environment)
	if err != nil {
		result := failedResponse(fmt.Sprintf("invalid Docker connection options: %v", err))
		return nil, &result
	}
	baseArgs, err := docker.GetComposeProjectArgsWithEnvironment(req.ComposeCommonArgs, req.CommonArgs, dependencies.Environment)
	if err != nil {
		result := failedResponse(fmt.Sprintf("invalid Docker connection options: %v", err))
		return nil, &result
	}
	return &pullManager{
		request:      req,
		dependencies: dependencies,
		checkMode:    state.CheckMode,
		dockerCLI:    dockerCLI,
		projectSrc:   projectSrc,
		cleanupDir:   cleanupDir,
		cmdEnv:       cmdEnv,
		baseArgs:     baseArgs,
	}, nil
}

func validateRequest(req *Request) error {
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
	if req.Policy == "" {
		req.Policy = "always"
	}
	switch req.Policy {
	case "always", "missing":
	default:
		return fmt.Errorf("policy must be one of always, missing, got %q", req.Policy)
	}
	return nil
}

func (manager *pullManager) run() Response {
	args := manager.pullArgs()
	result, err := manager.dependencies.CLIRunner.Run(context.Background(), docker.CLICommand{
		Name: manager.dockerCLI,
		Args: args,
		Dir:  manager.projectSrc,
		Env:  manager.cmdEnv,
	})
	stdout := string(result.Stdout)
	stderr := string(result.Stderr)
	parsed := docker.ParseComposeJSONEvents(result.Stderr)
	if len(parsed.Events) == 0 && len(result.Output) > 0 {
		parsed = docker.ParseComposeJSONEvents(result.Output)
	}
	ignorePullEvents := manager.request.Policy != "missing" && !manager.checkMode
	response := Response{
		Changed:  docker.ComposeHasChanges(parsed.Events, docker.ComposeChangeOptions{IgnoreServicePullEvents: ignorePullEvents}),
		Actions:  docker.ComposeEventActionRecords(parsed.Events),
		Stdout:   stdout,
		Stderr:   stderr,
		Cmd:      quoteCommand(manager.dockerCLI, args),
		RC:       result.ExitCode,
		Warnings: parsed.Warnings,
	}
	if response.Actions == nil {
		response.Actions = []docker.ComposeAction{}
	}
	if err != nil && result.ExitCode <= 0 {
		response.Failed = true
		response.Msg = fmt.Sprintf("command failed: %v", err)
		if result.ExitCode < 0 {
			response.RC = result.ExitCode
		} else {
			response.RC = 1
		}
		return response
	}
	if result.ExitCode != 0 {
		response.Failed = true
		response.Msg = docker.ComposeFailureMessage(parsed.Events, result.ExitCode)
		response.RC = result.ExitCode
	}
	return response
}

func (manager *pullManager) pullArgs() []string {
	args := append(append([]string{}, manager.baseArgs...), "pull")
	if manager.request.Policy != "always" {
		args = append(args, "--policy", manager.request.Policy)
	}
	if manager.request.IgnoreBuildable {
		args = append(args, "--ignore-buildable")
	}
	if manager.request.IgnorePullFailures {
		args = append(args, "--ignore-pull-failures")
	}
	if manager.request.IncludeDeps {
		args = append(args, "--include-deps")
	}
	if manager.checkMode {
		args = append(args, "--dry-run")
	}
	args = append(args, "--")
	return append(args, manager.request.Services...)
}

func quoteCommand(cli string, args []string) string {
	parts := append([]string{cli}, args...)
	quoted := make([]string, len(parts))
	for index, part := range parts {
		quoted[index] = quoteArg(part)
	}
	return strings.Join(quoted, " ")
}

func quoteArg(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t\n'\"\\$`") {
		return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
	}
	return value
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message, Actions: []docker.ComposeAction{}}
}
