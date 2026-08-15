package docker_stack

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"gopkg.in/yaml.v3"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if strings.TrimSpace(req.Name) == "" {
		return failed("missing required arguments: name")
	}
	state := req.State
	if state == "" {
		state = "present"
	}
	switch state {
	case "present", "absent":
	default:
		return failed(fmt.Sprintf("value of state must be one of: absent, present, got: %s", state))
	}
	if req.ResolveImage != "" {
		switch req.ResolveImage {
		case "always", "changed", "never":
		default:
			return failed(fmt.Sprintf("value of resolve_image must be one of: always, changed, never, got: %s", req.ResolveImage))
		}
	}

	cmdEnv, err := docker.DockerCLIEnvWithEnvironment(req.CommonArgs, dependencies.Environment)
	if err != nil {
		return failed(fmt.Sprintf("invalid Docker connection options: %v", err))
	}

	manager := &stackManager{
		req:          req,
		dependencies: dependencies,
		env:          cmdEnv,
		dockerCLI:    req.DockerCLI,
		name:         req.Name,
		detach:       true,
		retries:      0,
		interval:     1,
	}
	if manager.dockerCLI == "" {
		manager.dockerCLI = "docker"
	}
	if req.Detach != nil {
		manager.detach = *req.Detach
	}
	if req.AbsentRetries != nil {
		manager.retries = *req.AbsentRetries
	}
	if req.AbsentRetriesInterval != nil {
		manager.interval = *req.AbsentRetriesInterval
	}

	if state == "absent" {
		return manager.remove()
	}
	return manager.deploy()
}

type stackManager struct {
	req          Request
	dependencies docker.Dependencies
	env          []string
	dockerCLI    string
	name         string
	detach       bool
	retries      int
	interval     int
}

func (manager *stackManager) deploy() Response {
	composeFiles, cleanup, err := manager.composeFiles()
	if err != nil {
		return failed(err.Error())
	}
	defer func() {
		for _, path := range cleanup {
			_ = manager.dependencies.FileSystem.RemoveAll(path)
		}
	}()

	before, fail := manager.inspect()
	if fail != nil {
		return *fail
	}

	command := []string{"stack", "deploy"}
	if manager.req.Prune {
		command = append(command, "--prune")
	}
	if !manager.detach {
		command = append(command, "--detach=false")
	}
	if manager.req.WithRegistryAuth {
		command = append(command, "--with-registry-auth")
	}
	if manager.req.ResolveImage != "" {
		command = append(command, "--resolve-image", manager.req.ResolveImage)
	}
	for _, file := range composeFiles {
		command = append(command, "--compose-file", file)
	}
	command = append(command, manager.name)

	result, fail := manager.call(command...)
	if fail != nil {
		return *fail
	}

	after, fail := manager.inspect()
	if fail != nil {
		return *fail
	}
	if result.ExitCode != 0 {
		return failedCLI("docker stack up deploy command failed", result)
	}

	changedDiff := specDiff(before, after)
	stripVolatileSpecDiff(changedDiff)
	if len(changedDiff) == 0 {
		return successCLI(false, result, nil)
	}
	return successCLI(true, result, specDiff(before, after))
}

func (manager *stackManager) remove() Response {
	services, fail := manager.serviceNames()
	if fail != nil {
		return *fail
	}
	if len(services) == 0 {
		return Response{Changed: false}
	}

	command := []string{"stack", "rm", manager.name}
	if !manager.detach {
		command = append(command, "--detach=false")
	}

	result, fail := manager.call(command...)
	if fail != nil {
		return *fail
	}
	retries := manager.retries
	for !isNothingFound(manager.name, string(result.Stderr)) && retries > 0 {
		manager.dependencies.Clock.Sleep(time.Duration(manager.interval) * time.Second)
		retries--
		result, fail = manager.call(command...)
		if fail != nil {
			return *fail
		}
	}
	if result.ExitCode != 0 {
		return failedCLI("'docker stack down' command failed", result)
	}
	response := successCLI(true, result, nil)
	response.Msg = string(result.Stdout)
	return response
}

func (manager *stackManager) composeFiles() ([]string, []string, error) {
	if len(manager.req.Compose) > 0 && strings.TrimSpace(manager.req.ComposeFile) != "" {
		return nil, nil, fmt.Errorf("parameters are mutually exclusive: compose, compose_file")
	}
	entries := manager.req.Compose
	if len(entries) == 0 && strings.TrimSpace(manager.req.ComposeFile) != "" {
		entries = ComposeList{{Path: manager.req.ComposeFile}}
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("compose parameter must be a list containing at least one element")
	}

	var files []string
	var cleanup []string
	for _, entry := range entries {
		if entry.Invalid != "" {
			return nil, cleanup, fmt.Errorf("compose element '%s' must be a string or a dictionary", entry.Invalid)
		}
		if entry.Dict != nil {
			payload, err := yaml.Marshal(entry.Dict)
			if err != nil {
				return nil, cleanup, fmt.Errorf("compose dictionary could not be encoded: %v", err)
			}
			path := filepath.Join(
				manager.dependencies.FileSystem.TempDir(),
				fmt.Sprintf("dibra-stack-compose-%d-%d.yml", manager.dependencies.Clock.Now().UnixNano(), len(cleanup)),
			)
			if err := manager.dependencies.FileSystem.WriteFile(path, payload, 0o600); err != nil {
				return nil, cleanup, fmt.Errorf("compose dictionary could not be written: %v", err)
			}
			files = append(files, path)
			cleanup = append(cleanup, path)
			continue
		}
		files = append(files, entry.Path)
	}
	return files, cleanup, nil
}

func (manager *stackManager) inspect() (map[string]any, *Response) {
	specs := map[string]any{}
	names, fail := manager.serviceNames()
	if fail != nil {
		return nil, fail
	}
	for _, name := range names {
		spec, fail := manager.serviceSpec(name)
		if fail != nil {
			return nil, fail
		}
		specs[name] = spec
	}
	return specs, nil
}

func (manager *stackManager) serviceNames() ([]string, *Response) {
	result, fail := manager.call("stack", "services", manager.name, "--format", "{{.Name}}")
	if fail != nil {
		return nil, fail
	}
	if isNothingFound(manager.name, string(result.Stderr)) {
		return nil, nil
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func (manager *stackManager) serviceSpec(name string) (any, *Response) {
	result, fail := manager.call("service", "inspect", name)
	if fail != nil {
		return nil, fail
	}
	if result.ExitCode != 0 {
		return nil, nil
	}
	var payload []map[string]any
	if err := json.Unmarshal(result.Stdout, &payload); err != nil {
		response := unexpected(fmt.Errorf("decode service inspect for %s: %w", name, err))
		return nil, &response
	}
	if len(payload) == 0 {
		return nil, nil
	}
	return payload[0]["Spec"], nil
}

func (manager *stackManager) call(args ...string) (docker.CLIResult, *Response) {
	full, err := docker.DockerCLIArgsWithEnvironment(manager.req.CommonArgs, manager.dependencies.Environment, args...)
	if err != nil {
		response := failed(fmt.Sprintf("invalid Docker connection options: %v", err))
		return docker.CLIResult{}, &response
	}
	ctx, cancel := docker.GetContextWithEnvironment(manager.req.CommonArgs, manager.dependencies.Environment)
	defer cancel()
	result, runErr := manager.dependencies.CLIRunner.Run(ctx, docker.CLICommand{
		Name: manager.dockerCLI,
		Args: full,
		Env:  manager.env,
	})
	if runErr != nil && result.ExitCode < 0 {
		response := unexpected(runErr)
		return result, &response
	}
	return result, nil
}

func isNothingFound(name, stderr string) bool {
	return strings.TrimRight(stderr, "\r\n") == "Nothing found in stack: "+name
}

func specDiff(before, after map[string]any) map[string]any {
	diff, _ := jsonDiff(before, after).(map[string]any)
	if diff == nil {
		return map[string]any{}
	}
	return diff
}

func stripVolatileSpecDiff(diff map[string]any) {
	for key, value := range diff {
		nested, ok := value.(map[string]any)
		if !ok {
			continue
		}
		delete(nested, "UpdatedAt")
		delete(nested, "Version")
		if len(nested) == 0 {
			delete(diff, key)
		}
	}
}

func jsonDiff(before, after any) any {
	if deepEqual(before, after) {
		return nil
	}
	beforeMap, beforeIsMap := asMap(before)
	afterMap, afterIsMap := asMap(after)
	if beforeIsMap && afterIsMap {
		diff := map[string]any{}
		var deleted []any
		for key, previous := range beforeMap {
			next, found := afterMap[key]
			if !found {
				deleted = append(deleted, key)
				continue
			}
			if nested := jsonDiff(previous, next); nested != nil {
				diff[key] = nested
			}
		}
		for key, next := range afterMap {
			if _, found := beforeMap[key]; !found {
				diff[key] = next
			}
		}
		if len(deleted) > 0 {
			sort.Slice(deleted, func(i, j int) bool {
				return fmt.Sprint(deleted[i]) < fmt.Sprint(deleted[j])
			})
			diff["delete"] = deleted
		}
		if len(diff) == 0 {
			return nil
		}
		return diff
	}
	return after
}

func asMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case nil:
		return map[string]any{}, true
	default:
		return nil, false
	}
}

func deepEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}

func failed(msg string) Response {
	return Response{Failed: true, Msg: msg}
}

func unexpected(err error) Response {
	return failed(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
}

func failedCLI(msg string, result docker.CLIResult) Response {
	response := failed(msg)
	rc := result.ExitCode
	response.RC = &rc
	response.Stdout = string(result.Stdout)
	response.Stderr = string(result.Stderr)
	return response
}

func successCLI(changed bool, result docker.CLIResult, diff map[string]any) Response {
	rc := result.ExitCode
	response := Response{
		Changed: changed,
		RC:      &rc,
		Stdout:  string(result.Stdout),
		Stderr:  string(result.Stderr),
	}
	if changed && len(diff) > 0 {
		response.StackSpecDiff = diff
	}
	return response
}
