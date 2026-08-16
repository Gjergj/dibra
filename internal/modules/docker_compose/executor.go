package docker_compose

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type composeManager struct {
	request      Request
	dependencies docker.Dependencies
	checkMode    bool
	dockerCLI    string
	projectSrc   string
	cleanupDir   string
	cmdEnv       []string
	baseArgs     []string
}

type commandOutcome struct {
	args    []string
	stdout  string
	stderr  string
	rc      int
	events  []docker.ComposeEvent
	changed bool
	actions []docker.ComposeAction
	failed  bool
	msg     string
	cmd     string
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
	manager, failure := newComposeManager(req, dependencies, state)
	if failure != nil {
		return *failure
	}
	if manager.cleanupDir != "" {
		defer func() { _ = dependencies.FileSystem.RemoveAll(manager.cleanupDir) }()
	}
	return manager.run()
}

func newComposeManager(req Request, dependencies docker.Dependencies, state execution.State) (*composeManager, *Response) {
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
	return &composeManager{
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
	if req.State == "" {
		req.State = "present"
	}
	switch req.State {
	case "absent", "present", "stopped", "restarted":
	default:
		return fmt.Errorf("state must be one of absent, present, stopped, restarted, got %q", req.State)
	}
	if req.Pull == "" {
		req.Pull = "policy"
	}
	switch req.Pull {
	case "always", "missing", "never", "policy":
	default:
		return fmt.Errorf("pull must be one of always, missing, never, policy, got %q", req.Pull)
	}
	if req.Build == "" {
		req.Build = "policy"
	}
	switch req.Build {
	case "always", "never", "policy":
	default:
		return fmt.Errorf("build must be one of always, never, policy, got %q", req.Build)
	}
	if req.Recreate == "" {
		if req.ForceRecreate {
			req.Recreate = "always"
		} else if req.NoRecreate {
			req.Recreate = "never"
		} else {
			req.Recreate = "auto"
		}
	}
	switch req.Recreate {
	case "always", "never", "auto":
	default:
		return fmt.Errorf("recreate must be one of always, never, auto, got %q", req.Recreate)
	}
	if req.RemoveImages != "" && req.RemoveImages != "all" && req.RemoveImages != "local" {
		return fmt.Errorf("remove_images must be one of all, local, got %q", req.RemoveImages)
	}
	return nil
}

func (manager *composeManager) run() Response {
	var outcome commandOutcome
	switch manager.request.State {
	case "present":
		outcome = manager.cmdUp(false)
	case "stopped":
		outcome = manager.cmdStop()
	case "restarted":
		outcome = manager.cmdRestart()
	case "absent":
		outcome = manager.cmdDown()
	}
	result := Response{
		Changed:  outcome.changed,
		Failed:   outcome.failed,
		Msg:      outcome.msg,
		Stdout:   outcome.stdout,
		Stderr:   outcome.stderr,
		Actions:  outcome.actions,
		Cmd:      outcome.cmd,
		RC:       outcome.rc,
		Warnings: nil,
	}
	if result.Actions == nil {
		result.Actions = []docker.ComposeAction{}
	}
	containers, containerErr := manager.listContainers()
	if containerErr != nil {
		if !result.Failed {
			return *containerErr
		}
	} else {
		result.Containers = containers
	}
	images, imageErr := manager.listImages()
	if imageErr != nil {
		return *imageErr
	}
	result.Images = images
	if !result.Failed {
		if result.Stdout == "" {
			result.Stdout = ""
		}
		if result.Stderr == "" {
			result.Stderr = ""
		}
	}
	return result
}

func (manager *composeManager) cmdUp(noStart bool) commandOutcome {
	args := manager.upArgs(noStart)
	return manager.runCompose(args, docker.ComposeChangeOptions{
		IgnoreServicePullEvents: true,
		IgnoreBuildEvents:       boolDefault(manager.request.IgnoreBuildEvents, true),
	})
}

func (manager *composeManager) cmdStop() commandOutcome {
	created := manager.cmdUp(true)
	if created.failed || manager.containersStopped() {
		return created
	}
	stopped := manager.runCompose(manager.stopArgs(), docker.ComposeChangeOptions{})
	return mergeOutcomes(created, stopped)
}

func (manager *composeManager) cmdRestart() commandOutcome {
	return manager.runCompose(manager.restartArgs(), docker.ComposeChangeOptions{})
}

func (manager *composeManager) cmdDown() commandOutcome {
	return manager.runCompose(manager.downArgs(), docker.ComposeChangeOptions{})
}

func (manager *composeManager) upArgs(noStart bool) []string {
	args := append([]string{}, manager.baseArgs...)
	args = append(args, "up", "--detach", "--no-color", "--quiet-pull")
	if manager.request.Pull != "policy" {
		args = append(args, "--pull", string(manager.request.Pull))
	}
	if manager.request.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	switch manager.request.Recreate {
	case "always":
		args = append(args, "--force-recreate")
	case "never":
		args = append(args, "--no-recreate")
	}
	if manager.request.RenewAnonVolumes {
		args = append(args, "--renew-anon-volumes")
	}
	if !manager.dependenciesEnabled() {
		args = append(args, "--no-deps")
	}
	if manager.request.Timeout != nil {
		args = append(args, "--timeout", strconv.Itoa(*manager.request.Timeout))
	}
	switch manager.request.Build {
	case "always":
		args = append(args, "--build")
	case "never":
		args = append(args, "--no-build")
	}
	for _, service := range sortedScaleKeys(manager.request.Scale) {
		args = append(args, "--scale", fmt.Sprintf("%s=%d", service, manager.request.Scale[service]))
	}
	if manager.request.Wait {
		args = append(args, "--wait")
		if manager.request.WaitTimeout != nil {
			args = append(args, "--wait-timeout", strconv.Itoa(*manager.request.WaitTimeout))
		}
	}
	if noStart {
		args = append(args, "--no-start")
	}
	if manager.checkMode {
		args = append(args, "--dry-run")
	}
	if manager.request.AssumeYes {
		args = append(args, "--yes")
	}
	return appendServiceArgs(args, manager.request.Services)
}

func (manager *composeManager) stopArgs() []string {
	args := append(append([]string{}, manager.baseArgs...), "stop")
	if manager.request.Timeout != nil {
		args = append(args, "--timeout", strconv.Itoa(*manager.request.Timeout))
	}
	if manager.checkMode {
		args = append(args, "--dry-run")
	}
	return appendServiceArgs(args, manager.request.Services)
}

func (manager *composeManager) restartArgs() []string {
	args := append(append([]string{}, manager.baseArgs...), "restart")
	if !manager.dependenciesEnabled() {
		args = append(args, "--no-deps")
	}
	if manager.request.Timeout != nil {
		args = append(args, "--timeout", strconv.Itoa(*manager.request.Timeout))
	}
	if manager.checkMode {
		args = append(args, "--dry-run")
	}
	return appendServiceArgs(args, manager.request.Services)
}

func (manager *composeManager) downArgs() []string {
	args := append(append([]string{}, manager.baseArgs...), "down")
	if manager.request.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	if manager.request.RemoveImages != "" {
		args = append(args, "--rmi", manager.request.RemoveImages)
	}
	if manager.request.RemoveVolumes {
		args = append(args, "--volumes")
	}
	if manager.request.Timeout != nil {
		args = append(args, "--timeout", strconv.Itoa(*manager.request.Timeout))
	}
	if manager.checkMode {
		args = append(args, "--dry-run")
	}
	return appendServiceArgs(args, manager.request.Services)
}

func appendServiceArgs(args []string, services []string) []string {
	args = append(args, "--")
	return append(args, services...)
}

func (manager *composeManager) runCompose(args []string, options docker.ComposeChangeOptions) commandOutcome {
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
	outcome := commandOutcome{
		args:    args,
		stdout:  stdout,
		stderr:  stderr,
		rc:      result.ExitCode,
		events:  parsed.Events,
		changed: docker.ComposeHasChanges(parsed.Events, options),
		actions: docker.ComposeEventActionRecords(parsed.Events),
		cmd:     quoteCommand(manager.dockerCLI, args),
	}
	if outcome.actions == nil {
		outcome.actions = []docker.ComposeAction{}
	}
	if err != nil && result.ExitCode <= 0 {
		outcome.failed = true
		outcome.msg = fmt.Sprintf("command failed: %v", err)
		if result.ExitCode < 0 {
			outcome.rc = result.ExitCode
		} else {
			outcome.rc = 1
		}
		return outcome
	}
	if result.ExitCode != 0 {
		outcome.failed = true
		outcome.msg = docker.ComposeFailureMessage(parsed.Events, result.ExitCode)
		outcome.rc = result.ExitCode
	}
	return outcome
}

func (manager *composeManager) listContainers() ([]map[string]any, *Response) {
	args := append(append([]string{}, manager.baseArgs...), "ps", "--format", "json", "--all", "--no-trunc")
	result, runErr := manager.dependencies.CLIRunner.Run(context.Background(), docker.CLICommand{
		Name: manager.dockerCLI,
		Args: args,
		Dir:  manager.projectSrc,
		Env:  manager.cmdEnv,
	})
	if result.ExitCode != 0 || (runErr != nil && result.ExitCode < 0) {
		parsed := docker.ParseComposeJSONEvents(result.Stderr)
		response := failedResponse(docker.ComposeFailureMessage(parsed.Events, result.ExitCode))
		response.Stdout = string(result.Stdout)
		response.Stderr = string(result.Stderr)
		response.Cmd = quoteCommand(manager.dockerCLI, args)
		response.RC = result.ExitCode
		return nil, &response
	}
	documents, err := docker.ParseComposeJSONDocuments(preferStdout(result))
	if err != nil {
		response := failedResponse(err.Error())
		return nil, &response
	}
	containers := make([]map[string]any, 0, len(documents))
	for _, document := range documents {
		container, err := docker.NormalizeComposeContainer(document)
		if err != nil {
			response := failedResponse(err.Error())
			return nil, &response
		}
		containers = append(containers, container)
	}
	return containers, nil
}

func (manager *composeManager) listImages() ([]any, *Response) {
	args := append(append([]string{}, manager.baseArgs...), "images", "--format", "json")
	result, runErr := manager.dependencies.CLIRunner.Run(context.Background(), docker.CLICommand{
		Name: manager.dockerCLI,
		Args: args,
		Dir:  manager.projectSrc,
		Env:  manager.cmdEnv,
	})
	if result.ExitCode != 0 || (runErr != nil && result.ExitCode < 0) {
		parsed := docker.ParseComposeJSONEvents(result.Stderr)
		response := failedResponse(docker.ComposeFailureMessage(parsed.Events, result.ExitCode))
		response.Stdout = string(result.Stdout)
		response.Stderr = string(result.Stderr)
		response.Cmd = quoteCommand(manager.dockerCLI, args)
		response.RC = result.ExitCode
		return nil, &response
	}
	images, err := docker.ParseComposeJSONDocuments(preferStdout(result))
	if err != nil {
		response := failedResponse(err.Error())
		return nil, &response
	}
	if images == nil {
		images = []any{}
	}
	return images, nil
}

func (manager *composeManager) containersStopped() bool {
	containers, err := manager.listContainers()
	if err != nil {
		return false
	}
	if len(containers) == 0 {
		return true
	}
	for _, container := range containers {
		state, _ := container["State"].(string)
		switch strings.ToLower(state) {
		case "created", "exited", "stopped", "killed":
		default:
			return false
		}
	}
	return true
}

func (manager *composeManager) dependenciesEnabled() bool {
	if manager.request.Dependencies != nil {
		return *manager.request.Dependencies
	}
	return !manager.request.NoDeps
}

func preferStdout(result docker.CLIResult) []byte {
	if len(bytesOrNil(result.Stdout)) > 0 {
		return result.Stdout
	}
	return result.Output
}

func bytesOrNil(value []byte) []byte {
	return value
}

func mergeOutcomes(first, second commandOutcome) commandOutcome {
	merged := commandOutcome{
		args:    second.args,
		stdout:  joinText(first.stdout, second.stdout),
		stderr:  joinText(first.stderr, second.stderr),
		rc:      second.rc,
		events:  append(append([]docker.ComposeEvent{}, first.events...), second.events...),
		changed: first.changed || second.changed,
		actions: append(append([]docker.ComposeAction{}, first.actions...), second.actions...),
		failed:  first.failed || second.failed,
		cmd:     second.cmd,
	}
	if first.failed {
		merged.args = first.args
		merged.rc = first.rc
		merged.msg = first.msg
		merged.cmd = first.cmd
	} else if second.failed {
		merged.msg = second.msg
	}
	return merged
}

func joinText(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n")
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

func sortedScaleKeys(scale ScaleMap) []string {
	keys := make([]string, 0, len(scale))
	for key := range scale {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message, Actions: []docker.ComposeAction{}}
}
