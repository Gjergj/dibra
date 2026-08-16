package docker_stack_info

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type scriptedCLI struct {
	commands []docker.CLICommand
	results  []docker.CLIResult
}

func (runner *scriptedCLI) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	if len(runner.results) == 0 {
		return docker.CLIResult{ExitCode: -1}, fmt.Errorf("unexpected command: %v", command.Args)
	}
	result := runner.results[0]
	runner.results = runner.results[1:]
	if result.ExitCode < 0 {
		return result, fmt.Errorf("exit status %d", result.ExitCode)
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("exit status %d", result.ExitCode)
	}
	return result, nil
}

func execute(req Request, runner *scriptedCLI) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		CLIRunner:   runner,
	})
}

func hasSequence(args []string, want ...string) bool {
	for index := 0; index+len(want) <= len(args); index++ {
		match := true
		for offset, value := range want {
			if args[index+offset] != value {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func stackJSON(name, services string) []byte {
	return []byte(fmt.Sprintf(`{"Name":%q,"Namespace":"","Orchestrator":"Swarm","Services":%q}`+"\n", name, services))
}

func TestExecuteEmptySwarmHasEmptyResults(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{{}}}
	response := execute(Request{}, runner)
	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if response.Results == nil || len(response.Results) != 0 {
		t.Fatalf("results = %#v", response.Results)
	}
	if response.RC == nil || *response.RC != 0 {
		t.Fatalf("rc = %#v", response.RC)
	}
	if response.Stdout != "" || response.Stderr != "" {
		t.Fatalf("stdio = %#v", response)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	command := runner.commands[0]
	if command.Name != "docker" {
		t.Fatalf("cli = %s", command.Name)
	}
	if !hasSequence(command.Args, "stack", "ls", "--format={{json .}}") {
		t.Fatalf("args = %#v", command.Args)
	}
}

func TestExecuteParsesStackListAndIgnoresNonJSONLines(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{{
		Stdout: []byte("NAME      SERVICES\n" +
			`{"Name":"test_stack","Namespace":"","Orchestrator":"Swarm","Services":"1"}` + "\n" +
			"\n" +
			`{"Name":"other","Namespace":"","Orchestrator":"Swarm","Services":"2"}` + "\n"),
		Stderr: []byte(" warning \n"),
	}}}
	response := execute(Request{}, runner)
	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if len(response.Results) != 2 {
		t.Fatalf("results = %#v", response.Results)
	}
	if response.Results[0]["Name"] != "test_stack" || response.Results[0]["Services"] != "1" {
		t.Fatalf("first = %#v", response.Results[0])
	}
	if response.Results[1]["Name"] != "other" || response.Results[1]["Services"] != "2" {
		t.Fatalf("second = %#v", response.Results[1])
	}
	if response.Results[0]["Orchestrator"] != "Swarm" || response.Results[0]["Namespace"] != "" {
		t.Fatalf("orchestrator/namespace = %#v", response.Results[0])
	}
	if response.Stderr != "warning" {
		t.Fatalf("stderr = %q", response.Stderr)
	}
	if !strings.Contains(response.Stdout, `"Name":"test_stack"`) || !strings.Contains(response.Stdout, `"Name":"other"`) {
		t.Fatalf("stdout = %q", response.Stdout)
	}
}

func TestExecuteOffSwarmFailsWithDaemonMessage(t *testing.T) {
	stderr := "Error response from daemon: This node is not a swarm manager. Use \"docker swarm init\" or \"docker swarm join\" to connect this node to swarm and try again\n"
	runner := &scriptedCLI{results: []docker.CLIResult{{
		ExitCode: 1,
		Stderr:   []byte(stderr),
	}}}
	response := execute(Request{}, runner)
	if !response.Failed || !strings.Contains(response.Msg, "Error response from daemon: This node is not a swarm manager") {
		t.Fatalf("response = %#v", response)
	}
	if response.RC == nil || *response.RC != 1 {
		t.Fatalf("rc = %#v", response.RC)
	}
}

func TestExecuteJSONParseFailureIncludesOutput(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{{
		Stdout: []byte("{not json\n"),
		Stderr: []byte("cli warning"),
	}}}
	response := execute(Request{DockerCLI: "/usr/bin/docker"}, runner)
	if !response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if !strings.Contains(response.Msg, "Error while parsing JSON output of /usr/bin/docker") {
		t.Fatalf("msg = %q", response.Msg)
	}
	if !strings.Contains(response.Msg, "stack ls --format={{json .}}") {
		t.Fatalf("msg missing command: %q", response.Msg)
	}
	if !strings.Contains(response.Msg, "JSON output: {not json") {
		t.Fatalf("msg missing stdout: %q", response.Msg)
	}
	if !strings.Contains(response.Msg, "Error output:\ncli warning") {
		t.Fatalf("msg missing stderr: %q", response.Msg)
	}
}

func TestExecuteCustomCLIAndConnectionFlags(t *testing.T) {
	host := "unix:///tmp/docker.sock"
	tlsTrue := true
	ca := "/tmp/ca.pem"
	cert := "/tmp/cert.pem"
	key := "/tmp/key.pem"
	runner := &scriptedCLI{results: []docker.CLIResult{{Stdout: stackJSON("web", "3")}}}
	response := execute(Request{
		CommonArgs: docker.CommonArgs{
			DockerHost:    &host,
			ValidateCerts: &tlsTrue,
			CAPath:        &ca,
			ClientCert:    &cert,
			ClientKey:     &key,
		},
		DockerCLI: "/usr/local/bin/docker",
	}, runner)
	if response.Failed || len(response.Results) != 1 || response.Results[0]["Name"] != "web" {
		t.Fatalf("response = %#v", response)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	command := runner.commands[0]
	if command.Name != "/usr/local/bin/docker" {
		t.Fatalf("cli = %s", command.Name)
	}
	for _, want := range [][]string{
		{"--host", host},
		{"--tlsverify"},
		{"--tlscacert", ca},
		{"--tlscert", cert},
		{"--tlskey", key},
		{"stack", "ls", "--format={{json .}}"},
	} {
		if !hasSequence(command.Args, want...) {
			t.Errorf("missing %#v in %#v", want, command.Args)
		}
	}
}

func TestExecuteConnectionConflict(t *testing.T) {
	host := "unix:///tmp/docker.sock"
	contextName := "production"
	response := execute(Request{
		CommonArgs: docker.CommonArgs{DockerHost: &host, CLIContext: &contextName},
	}, &scriptedCLI{})
	if !response.Failed || !strings.Contains(response.Msg, "docker_host and cli_context are mutually exclusive") {
		t.Fatalf("response = %#v", response)
	}
	if !strings.Contains(response.Msg, "invalid Docker connection options") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteClientCertWithoutKey(t *testing.T) {
	cert := "/tmp/cert.pem"
	response := execute(Request{
		CommonArgs: docker.CommonArgs{ClientCert: &cert},
	}, &scriptedCLI{})
	if !response.Failed || !strings.Contains(response.Msg, "client_cert and client_key must be specified together") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteUnexpectedCLIStartFailure(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{{ExitCode: -1, Stderr: []byte("exec")}}}
	response := execute(Request{}, runner)
	if !response.Failed || !strings.Contains(response.Msg, "An unexpected Docker error occurred") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteTLSWithoutVerify(t *testing.T) {
	tlsTrue := true
	runner := &scriptedCLI{results: []docker.CLIResult{{Stdout: stackJSON("web", "1")}}}
	response := execute(Request{
		CommonArgs: docker.CommonArgs{TLS: &tlsTrue},
	}, runner)
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if !hasSequence(runner.commands[0].Args, "--tls") {
		t.Fatalf("args = %#v", runner.commands[0].Args)
	}
	if hasSequence(runner.commands[0].Args, "--tlsverify") {
		t.Fatalf("unexpected tlsverify in %#v", runner.commands[0].Args)
	}
}

func TestExecuteCLIContextFlag(t *testing.T) {
	contextName := "desktop-linux"
	runner := &scriptedCLI{results: []docker.CLIResult{{Stdout: stackJSON("app", "1")}}}
	response := execute(Request{
		CommonArgs: docker.CommonArgs{CLIContext: &contextName},
	}, runner)
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if !hasSequence(runner.commands[0].Args, "--context", contextName) {
		t.Fatalf("args = %#v", runner.commands[0].Args)
	}
	if hasSequence(runner.commands[0].Args, "--host") {
		t.Fatalf("unexpected host flag in %#v", runner.commands[0].Args)
	}
}
