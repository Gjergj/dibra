package docker_stack_task_info

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

const stackPSFormat = "--format={{json .}}"

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return failed("missing required arguments: name")
	}

	cmdEnv, err := docker.DockerCLIEnvWithEnvironment(req.CommonArgs, dependencies.Environment)
	if err != nil {
		return failed(fmt.Sprintf("invalid Docker connection options: %v", err))
	}

	dockerCLI := req.DockerCLI
	if dockerCLI == "" {
		dockerCLI = "docker"
	}

	full, err := docker.DockerCLIArgsWithEnvironment(req.CommonArgs, dependencies.Environment, "stack", "ps", name, stackPSFormat)
	if err != nil {
		return failed(fmt.Sprintf("invalid Docker connection options: %v", err))
	}

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()
	result, runErr := dependencies.CLIRunner.Run(ctx, docker.CLICommand{
		Name: dockerCLI,
		Args: full,
		Env:  cmdEnv,
	})
	if runErr != nil && result.ExitCode < 0 {
		return unexpected(runErr)
	}
	if result.ExitCode != 0 {
		return failedCLI(cliFailureMessage(result), result)
	}

	entries, err := parseJSONStream(result.Stdout)
	if err != nil {
		return failedCLI(fmt.Sprintf(
			"Error while parsing JSON output of %s: %v\nJSON output: %s\n\nError output:\n%s",
			composeCmdStr(dockerCLI, full),
			err,
			string(result.Stdout),
			string(result.Stderr),
		), result)
	}

	stdout, err := encodeResults(entries)
	if err != nil {
		return unexpected(err)
	}
	rc := result.ExitCode
	return Response{
		Changed: false,
		RC:      &rc,
		Stdout:  stdout,
		Stderr:  strings.TrimSpace(string(result.Stderr)),
		Results: entries,
	}
}

func parseJSONStream(stdout []byte) ([]map[string]any, error) {
	results := make([]map[string]any, 0)
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("{")) {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, err
		}
		results = append(results, entry)
	}
	return results, nil
}

func encodeResults(results []map[string]any) (string, error) {
	if len(results) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(results))
	for _, entry := range results {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return "", err
		}
		lines = append(lines, string(encoded))
	}
	return strings.Join(lines, "\n"), nil
}

func composeCmdStr(name string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, name)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

func cliFailureMessage(result docker.CLIResult) string {
	msg := strings.TrimSpace(string(result.Stderr))
	if msg == "" {
		msg = strings.TrimSpace(string(result.Stdout))
	}
	if msg == "" {
		return "non-zero return code"
	}
	return msg
}

func failed(msg string) Response {
	return Response{Failed: true, Msg: msg, Results: []map[string]any{}}
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
