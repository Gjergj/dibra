package docker_stack

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type fileInfo struct{ name string }

func (info fileInfo) Name() string       { return info.name }
func (info fileInfo) Size() int64        { return 1 }
func (info fileInfo) Mode() fs.FileMode  { return 0644 }
func (info fileInfo) ModTime() time.Time { return time.Time{} }
func (info fileInfo) IsDir() bool        { return false }
func (info fileInfo) Sys() any           { return nil }

type memoryFS struct {
	docker.FileSystem
	files   map[string][]byte
	removed []string
}

func newMemoryFS() *memoryFS {
	return &memoryFS{files: map[string][]byte{}}
}

func (filesystem *memoryFS) Stat(path string) (fs.FileInfo, error) {
	if _, found := filesystem.files[path]; found {
		return fileInfo{name: filepath.Base(path)}, nil
	}
	return nil, fs.ErrNotExist
}
func (filesystem *memoryFS) Abs(path string) (string, error) { return path, nil }
func (filesystem *memoryFS) TempDir() string                 { return "/tmp" }
func (filesystem *memoryFS) WriteFile(path string, data []byte, _ fs.FileMode) error {
	filesystem.files[path] = append([]byte(nil), data...)
	return nil
}
func (filesystem *memoryFS) RemoveAll(path string) error {
	filesystem.removed = append(filesystem.removed, path)
	delete(filesystem.files, path)
	return nil
}

type recordingClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (clock *recordingClock) Now() time.Time { return clock.now }
func (clock *recordingClock) Sleep(delay time.Duration) {
	clock.sleeps = append(clock.sleeps, delay)
}

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
	return result, nil
}

func nothingFound(name string) docker.CLIResult {
	return docker.CLIResult{Stderr: []byte("Nothing found in stack: " + name + "\n")}
}

func servicesNamed(names ...string) docker.CLIResult {
	return docker.CLIResult{Stdout: []byte(strings.Join(names, "\n") + "\n")}
}

func inspectSpec(spec map[string]any) docker.CLIResult {
	payload, err := json.Marshal([]map[string]any{{"Spec": spec}})
	if err != nil {
		panic(err)
	}
	return docker.CLIResult{Stdout: payload}
}

func busyboxSpec(env ...string) map[string]any {
	container := map[string]any{"Image": "alpine:latest"}
	if len(env) > 0 {
		values := make([]any, 0, len(env))
		for _, item := range env {
			values = append(values, item)
		}
		container["Env"] = values
	}
	return map[string]any{
		"Name": "test_stack_busybox",
		"TaskTemplate": map[string]any{
			"ContainerSpec": container,
		},
	}
}

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }


func execute(req Request, runner *scriptedCLI) Response {
	return executeFS(req, runner, newMemoryFS(), &recordingClock{now: time.Unix(0, 123)})
}

func executeFS(req Request, runner *scriptedCLI, filesystem *memoryFS, clock *recordingClock) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  filesystem,
		Clock:       clock,
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

func commandByAction(t *testing.T, runner *scriptedCLI, action string) docker.CLICommand {
	t.Helper()
	for _, command := range runner.commands {
		if containsArg(command.Args, action) {
			return command
		}
	}
	t.Fatalf("missing %s command in %#v", action, runner.commands)
	return docker.CLICommand{}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestExecuteValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "missing name", request: Request{Compose: ComposeList{{Path: "stack.yml"}}}, want: "missing required arguments: name"},
		{name: "empty name", request: Request{Name: "  ", Compose: ComposeList{{Path: "stack.yml"}}}, want: "missing required arguments: name"},
		{name: "missing compose", request: Request{Name: "test_stack"}, want: "compose parameter must be a list containing at least one element"},
		{name: "empty compose", request: Request{Name: "test_stack", Compose: ComposeList{}}, want: "compose parameter must be a list containing at least one element"},
		{name: "invalid compose", request: Request{Name: "test_stack", Compose: ComposeList{{Invalid: "1"}}}, want: "compose element '1' must be a string or a dictionary"},
		{name: "exclusive compose", request: Request{Name: "test_stack", ComposeFile: "a.yml", Compose: ComposeList{{Path: "b.yml"}}}, want: "parameters are mutually exclusive: compose, compose_file"},
		{name: "bad state", request: Request{Name: "test_stack", State: "running", Compose: ComposeList{{Path: "stack.yml"}}}, want: "value of state must be one of: absent, present, got: running"},
		{name: "bad resolve_image", request: Request{Name: "test_stack", ResolveImage: "sometimes", Compose: ComposeList{{Path: "stack.yml"}}}, want: "value of resolve_image must be one of: always, changed, never, got: sometimes"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := execute(test.request, &scriptedCLI{})
			if !response.Failed || response.Msg != test.want {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestComposeListDecodesPathsDictsAndInvalidValues(t *testing.T) {
	var list ComposeList
	if err := json.Unmarshal([]byte(`["/opt/stack.yml", {"version":"3"}, 1, ["x"]]`), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || list[0].Path != "/opt/stack.yml" || list[1].Dict["version"] != "3" {
		t.Fatalf("list = %#v", list)
	}
	if list[2].Invalid != "1" || list[3].Invalid != "[x]" {
		t.Fatalf("invalid entries = %#v", list)
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `["/opt/stack.yml",{"version":"3"},1,["x"]]` {
		t.Fatalf("marshal = %s", encoded)
	}
}

func TestExecutePresentCreatesAndReturnsSpecDiff(t *testing.T) {
	spec := busyboxSpec()
	runner := &scriptedCLI{results: []docker.CLIResult{
		nothingFound("test_stack"),
		{Stdout: []byte("Creating service test_stack_busybox\n")},
		servicesNamed("test_stack_busybox"),
		inspectSpec(spec),
	}}
	response := execute(Request{
		Name:    "test_stack",
		Compose: ComposeList{{Path: "/opt/stack.yml"}},
	}, runner)
	if response.Failed || !response.Changed || response.RC == nil || *response.RC != 0 {
		t.Fatalf("response = %#v", response)
	}
	if response.StackSpecDiff["test_stack_busybox"] == nil {
		t.Fatalf("stack_spec_diff = %#v", response.StackSpecDiff)
	}
	deploy := commandByAction(t, runner, "deploy")
	if deploy.Name != "docker" {
		t.Fatalf("cli = %s", deploy.Name)
	}
	if !hasSequence(deploy.Args, "--host", "unix:///var/run/docker.sock") {
		t.Fatalf("missing default host: %#v", deploy.Args)
	}
	if !hasSequence(deploy.Args, "--compose-file", "/opt/stack.yml", "test_stack") {
		t.Fatalf("deploy args = %#v", deploy.Args)
	}
	if containsArg(deploy.Args, "--prune") || containsArg(deploy.Args, "--detach=false") || containsArg(deploy.Args, "--with-registry-auth") {
		t.Fatalf("unexpected flags: %#v", deploy.Args)
	}
}

func TestExecutePresentIsIdempotentWhenSpecsMatch(t *testing.T) {
	spec := busyboxSpec()
	specWithMeta := busyboxSpec()
	specWithMeta["UpdatedAt"] = "2026-08-15T00:00:01Z"
	specWithMeta["Version"] = map[string]any{"Index": 2}
	runner := &scriptedCLI{results: []docker.CLIResult{
		servicesNamed("test_stack_busybox"),
		inspectSpec(spec),
		{Stdout: []byte("Updating service test_stack_busybox\n")},
		servicesNamed("test_stack_busybox"),
		inspectSpec(specWithMeta),
	}}
	response := execute(Request{
		Name:        "test_stack",
		ComposeFile: "/opt/stack.yml",
	}, runner)
	if response.Failed || response.Changed {
		t.Fatalf("idempotent = %#v", response)
	}
	if response.StackSpecDiff != nil {
		t.Fatalf("unexpected stack_spec_diff = %#v", response.StackSpecDiff)
	}
}

func TestExecutePresentOverrideReturnsEnvDiff(t *testing.T) {
	before := busyboxSpec()
	after := busyboxSpec("envvar=value")
	runner := &scriptedCLI{results: []docker.CLIResult{
		servicesNamed("test_stack_busybox"),
		inspectSpec(before),
		{Stdout: []byte("Updating service test_stack_busybox\n")},
		servicesNamed("test_stack_busybox"),
		inspectSpec(after),
	}}
	response := execute(Request{
		Name: "test_stack",
		Compose: ComposeList{
			{Path: "/opt/stack.yml"},
			{Dict: map[string]any{
				"version": "3",
				"services": map[string]any{
					"busybox": map[string]any{
						"environment": map[string]any{"envvar": "value"},
					},
				},
			}},
		},
	}, runner)
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	service, _ := response.StackSpecDiff["test_stack_busybox"].(map[string]any)
	task, _ := service["TaskTemplate"].(map[string]any)
	container, _ := task["ContainerSpec"].(map[string]any)
	env, _ := container["Env"].([]any)
	if len(env) != 1 || env[0] != "envvar=value" {
		t.Fatalf("stack_spec_diff = %#v", response.StackSpecDiff)
	}
}

func TestExecutePresentWritesDictComposeAndCleansUp(t *testing.T) {
	filesystem := newMemoryFS()
	clock := &recordingClock{now: time.Unix(0, 123)}
	spec := busyboxSpec()
	runner := &scriptedCLI{results: []docker.CLIResult{
		nothingFound("mystack"),
		{},
		servicesNamed("mystack_web"),
		inspectSpec(spec),
	}}
	response := executeFS(Request{
		Name: "mystack",
		Compose: ComposeList{{Dict: map[string]any{
			"version": "3",
			"services": map[string]any{
				"web": map[string]any{"image": "nginx:latest"},
			},
		}}},
	}, runner, filesystem, clock)
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	want := "/tmp/dibra-stack-compose-123-0.yml"
	deploy := commandByAction(t, runner, "deploy")
	if !hasSequence(deploy.Args, "--compose-file", want, "mystack") {
		t.Fatalf("deploy args = %#v", deploy.Args)
	}
	if _, found := filesystem.files[want]; found {
		t.Fatal("temp compose file was not cleaned up")
	}
	if len(filesystem.removed) != 1 || filesystem.removed[0] != want {
		t.Fatalf("removed = %#v", filesystem.removed)
	}
}

func TestExecutePresentDeployFlagsAndCustomCLI(t *testing.T) {
	host := "unix:///tmp/docker.sock"
	runner := &scriptedCLI{results: []docker.CLIResult{
		nothingFound("mystack"),
		{},
		servicesNamed("mystack_web"),
		inspectSpec(busyboxSpec()),
	}}
	response := execute(Request{
		CommonArgs:       docker.CommonArgs{DockerHost: &host},
		Name:             "mystack",
		Compose:          ComposeList{{Path: "one.yml"}, {Path: "two.yml"}},
		Prune:            true,
		Detach:           boolPtr(false),
		WithRegistryAuth: true,
		ResolveImage:     "never",
		DockerCLI:        "/usr/local/bin/docker",
	}, runner)
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	deploy := commandByAction(t, runner, "deploy")
	if deploy.Name != "/usr/local/bin/docker" {
		t.Fatalf("cli = %s", deploy.Name)
	}
	for _, want := range [][]string{
		{"--host", host},
		{"--prune"},
		{"--detach=false"},
		{"--with-registry-auth"},
		{"--resolve-image", "never"},
		{"--compose-file", "one.yml"},
		{"--compose-file", "two.yml", "mystack"},
	} {
		if !hasSequence(deploy.Args, want...) {
			t.Errorf("missing %#v in %#v", want, deploy.Args)
		}
	}
}

func TestExecutePresentDeployFailureIncludesCLIOutput(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{
		nothingFound("test_stack"),
		{ExitCode: 1, Stdout: []byte("out"), Stderr: []byte("bad compose")},
		nothingFound("test_stack"),
	}}
	response := execute(Request{
		Name:    "test_stack",
		Compose: ComposeList{{Path: "missing.yml"}},
	}, runner)
	if !response.Failed || response.Msg != "docker stack up deploy command failed" {
		t.Fatalf("response = %#v", response)
	}
	if response.RC == nil || *response.RC != 1 || response.Stdout != "out" || response.Stderr != "bad compose" {
		t.Fatalf("cli fields = %#v", response)
	}
}

func TestExecuteAbsentIdempotentWhenMissing(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{nothingFound("test_stack")}}
	response := execute(Request{Name: "test_stack", State: "absent", AbsentRetries: intPtr(30)}, runner)
	if response.Failed || response.Changed || response.RC != nil {
		t.Fatalf("response = %#v", response)
	}
	if len(runner.commands) != 1 || !containsArg(runner.commands[0].Args, "services") {
		t.Fatalf("commands = %#v", runner.commands)
	}
}

func TestExecuteAbsentRemovesAndRetriesUntilGone(t *testing.T) {
	clock := &recordingClock{now: time.Unix(0, 1)}
	runner := &scriptedCLI{results: []docker.CLIResult{
		servicesNamed("test_stack_busybox"),
		{Stdout: []byte("Removing service test_stack_busybox\n")},
		nothingFound("test_stack"),
	}}
	response := executeFS(Request{
		Name:                  "test_stack",
		State:                 "absent",
		AbsentRetries:         intPtr(30),
		AbsentRetriesInterval: intPtr(2),
		Detach:                boolPtr(false),
		DockerCLI:             "/bin/docker",
	}, runner, newMemoryFS(), clock)
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if response.Stderr != "Nothing found in stack: test_stack\n" {
		t.Fatalf("stderr = %q", response.Stderr)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 2*time.Second {
		t.Fatalf("sleeps = %#v", clock.sleeps)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	rm := runner.commands[1]
	if rm.Name != "/bin/docker" || !hasSequence(rm.Args, "stack", "rm", "test_stack", "--detach=false") {
		t.Fatalf("rm = %#v", rm)
	}
}

func TestExecuteAbsentFailsWhenRemoveKeepsFailing(t *testing.T) {
	clock := &recordingClock{}
	runner := &scriptedCLI{results: []docker.CLIResult{
		servicesNamed("test_stack_busybox"),
		{ExitCode: 1, Stderr: []byte("busy")},
		{ExitCode: 1, Stderr: []byte("busy")},
	}}
	response := executeFS(Request{
		Name:          "test_stack",
		State:         "absent",
		AbsentRetries: intPtr(1),
	}, runner, newMemoryFS(), clock)
	if !response.Failed || response.Msg != "'docker stack down' command failed" {
		t.Fatalf("response = %#v", response)
	}
	if response.RC == nil || *response.RC != 1 || len(clock.sleeps) != 1 {
		t.Fatalf("response = %#v sleeps=%#v", response, clock.sleeps)
	}
}

func TestExecuteConnectionConflict(t *testing.T) {
	host := "unix:///tmp/docker.sock"
	contextName := "production"
	response := execute(Request{
		CommonArgs: docker.CommonArgs{DockerHost: &host, CLIContext: &contextName},
		Name:       "test_stack",
		State:      "absent",
	}, &scriptedCLI{})
	if !response.Failed || !strings.Contains(response.Msg, "docker_host and cli_context are mutually exclusive") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteUnexpectedCLIStartFailure(t *testing.T) {
	runner := &scriptedCLI{results: []docker.CLIResult{{ExitCode: -1, Stderr: []byte("exec")}}}
	response := execute(Request{Name: "test_stack", State: "absent"}, runner)
	if !response.Failed || !strings.Contains(response.Msg, "An unexpected Docker error occurred") {
		t.Fatalf("response = %#v", response)
	}
}
