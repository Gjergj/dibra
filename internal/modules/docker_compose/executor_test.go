package docker_compose

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type directoryInfo struct{ name string }

func (info directoryInfo) Name() string       { return info.name }
func (info directoryInfo) Size() int64        { return 0 }
func (info directoryInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (info directoryInfo) ModTime() time.Time { return time.Time{} }
func (info directoryInfo) IsDir() bool        { return true }
func (info directoryInfo) Sys() any           { return nil }

type fileInfo struct{ name string }

func (info fileInfo) Name() string       { return info.name }
func (info fileInfo) Size() int64        { return 1 }
func (info fileInfo) Mode() fs.FileMode  { return 0644 }
func (info fileInfo) ModTime() time.Time { return time.Time{} }
func (info fileInfo) IsDir() bool        { return false }
func (info fileInfo) Sys() any           { return nil }

type composeFileSystem struct {
	docker.FileSystem
	dirs  map[string]bool
	files map[string][]byte
}

func newComposeFileSystem(project string) *composeFileSystem {
	return &composeFileSystem{
		dirs:  map[string]bool{project: true},
		files: map[string][]byte{filepath.Join(project, "docker-compose.yml"): []byte("services: {}\n")},
	}
}

func (filesystem *composeFileSystem) Stat(path string) (fs.FileInfo, error) {
	if filesystem.dirs[path] {
		return directoryInfo{name: filepath.Base(path)}, nil
	}
	if _, found := filesystem.files[path]; found {
		return fileInfo{name: filepath.Base(path)}, nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *composeFileSystem) Abs(path string) (string, error) { return path, nil }
func (filesystem *composeFileSystem) TempDir() string                 { return "/tmp" }
func (filesystem *composeFileSystem) MkdirAll(path string, _ fs.FileMode) error {
	filesystem.dirs[path] = true
	return nil
}
func (filesystem *composeFileSystem) WriteFile(path string, data []byte, _ fs.FileMode) error {
	filesystem.files[path] = append([]byte(nil), data...)
	return nil
}
func (filesystem *composeFileSystem) RemoveAll(path string) error {
	delete(filesystem.dirs, path)
	for file := range filesystem.files {
		if strings.HasPrefix(file, path) {
			delete(filesystem.files, file)
		}
	}
	return nil
}

type scriptedCLI struct {
	commands []docker.CLICommand
	version  docker.CLIResult
	results  map[string][]docker.CLIResult
}

func (runner *scriptedCLI) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	action := composeAction(command.Args)
	if action == "version" {
		if runner.version.Output == nil {
			return docker.CLIResult{Output: []byte(`{"version":"v5.4.0"}`)}, nil
		}
		return runner.version, nil
	}
	queue := runner.results[action]
	if len(queue) == 0 {
		return docker.CLIResult{Stdout: []byte("[]")}, nil
	}
	result := queue[0]
	runner.results[action] = queue[1:]
	if result.ExitCode != 0 {
		return result, fmt.Errorf("exit status %d", result.ExitCode)
	}
	return result, nil
}

func composeAction(args []string) string {
	for index, arg := range args {
		if arg != "compose" {
			continue
		}
		for _, candidate := range args[index+1:] {
			switch candidate {
			case "version", "up", "down", "stop", "restart", "ps", "images":
				return candidate
			}
		}
	}
	return ""
}

func creatingEvent() []byte {
	return []byte("{\"id\":\"Container demo-web-1\",\"status\":\"Working\",\"text\":\"Creating\"}\n")
}

func TestExecutePresentUsesPinnedComposeUpFlags(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{Stderr: creatingEvent()}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project", ProjectName: "demo"},
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_HOST": "unix:///tmp/docker.sock"},
		FileSystem:  newComposeFileSystem("/project"),
		CLIRunner:   runner,
	})
	if response.Failed || !response.Changed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if len(response.Actions) != 1 || response.Actions[0] != (docker.ComposeAction{What: "container", ID: "demo-web-1", Status: "Creating"}) {
		t.Fatalf("actions = %#v", response.Actions)
	}
	command := lastAction(t, runner, "up")
	if command.Name != "docker" || command.Dir != "/project" {
		t.Errorf("command = %#v", command)
	}
	want := []string{
		"--host", "unix:///tmp/docker.sock", "compose", "--ansi", "never", "--progress", "json",
		"--project-directory", "/project", "--project-name", "demo",
		"up", "--detach", "--no-color", "--quiet-pull", "--",
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Errorf("args = %#v, want %#v", command.Args, want)
	}
}

func TestExecuteCheckModeAddsDryRunAndDoesNotSkip(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{Stderr: creatingEvent()}},
	}}
	response := ExecuteWithDependenciesAndState(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  newComposeFileSystem("/project"),
		CLIRunner:   runner,
	}, execution.State{CheckMode: true})
	if response.Failed || !response.Changed {
		t.Fatalf("check mode = %#v", response)
	}
	if !containsArgs(lastAction(t, runner, "up").Args, "--dry-run") {
		t.Fatalf("missing --dry-run: %#v", lastAction(t, runner, "up").Args)
	}
}

func TestExecutePresentPoliciesAndScale(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"up": {{}}}}
	timeout := 12
	waitTimeout := 30
	dependencies := false
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectSrc: "/project",
			Files:      []string{"compose.yaml", "override.yaml"},
			EnvFiles:   []string{".env"},
			Profiles:   []string{"web"},
		},
		Pull:             "always",
		Build:            "always",
		Recreate:         "always",
		RenewAnonVolumes: true,
		RemoveOrphans:    true,
		Dependencies:     &dependencies,
		Timeout:          &timeout,
		Wait:             true,
		WaitTimeout:      &waitTimeout,
		AssumeYes:        true,
		Scale:            ScaleMap{"web": 3, "api": 1},
		Services:         []string{"web"},
		DockerCLI:        "/usr/bin/docker",
	}, docker.Dependencies{
		FileSystem: &composeFileSystem{
			dirs: map[string]bool{"/project": true},
			files: map[string][]byte{
				"/project/compose.yaml":  []byte("x"),
				"/project/override.yaml": []byte("y"),
			},
		},
		CLIRunner: runner,
	})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	command := lastAction(t, runner, "up")
	if command.Name != "/usr/bin/docker" {
		t.Fatalf("cli = %s", command.Name)
	}
	for _, want := range []string{
		"--file", "compose.yaml", "--file", "override.yaml", "--env-file", ".env", "--profile", "web",
		"--pull", "always", "--build", "--force-recreate", "--renew-anon-volumes", "--remove-orphans",
		"--no-deps", "--timeout", "12", "--wait", "--wait-timeout", "30", "--yes", "--scale", "api=1",
		"--scale", "web=3", "--", "web",
	} {
		if !containsArgs(command.Args, want) {
			t.Errorf("missing %q in %#v", want, command.Args)
		}
	}
}

func TestExecuteStoppedCreatesThenStops(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up":   {{Stderr: creatingEvent()}},
		"ps":   {{Stdout: []byte(`{"Name":"demo-web-1","State":"running"}`)}},
		"stop": {{Stderr: []byte("{\"id\":\"Container demo-web-1\",\"status\":\"Working\",\"text\":\"Stopping\"}\n")}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		State:             "stopped",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if response.Failed || !response.Changed {
		t.Fatalf("stopped = %#v", response)
	}
	if !containsArgs(lastAction(t, runner, "up").Args, "--no-start") {
		t.Fatalf("up args = %#v", lastAction(t, runner, "up").Args)
	}
	if composeAction(lastAction(t, runner, "stop").Args) != "stop" {
		t.Fatal("expected stop")
	}
}

func TestExecuteStoppedSkipsStopWhenAlreadyStopped(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{}},
		"ps": {{Stdout: []byte(`{"Name":"demo-web-1","State":"exited"}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		State:             "stopped",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if response.Failed {
		t.Fatalf("stopped = %#v", response)
	}
	for _, command := range runner.commands {
		if composeAction(command.Args) == "stop" {
			t.Fatalf("stop was invoked: %#v", runner.commands)
		}
	}
}

func TestExecuteRestartAndDown(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"restart": {{Stderr: []byte("{\"id\":\"Container demo-web-1\",\"status\":\"Working\",\"text\":\"Restarting\"}\n")}},
		"down":    {{Stderr: []byte("{\"id\":\"Container demo-web-1\",\"status\":\"Working\",\"text\":\"Removing\"}\n")}},
	}}
	restart := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		State:             "restarted",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if restart.Failed || !restart.Changed {
		t.Fatalf("restart = %#v", restart)
	}
	down := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		State:             "absent",
		RemoveOrphans:     true,
		RemoveVolumes:     true,
		RemoveImages:      "local",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if down.Failed || !down.Changed {
		t.Fatalf("down = %#v", down)
	}
	if !containsArgs(lastAction(t, runner, "down").Args, "--volumes") || !containsArgs(lastAction(t, runner, "down").Args, "--rmi") {
		t.Fatalf("down args = %#v", lastAction(t, runner, "down").Args)
	}
}

func TestExecuteIgnoresPullEventsForChanged(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{Stderr: []byte("{\"id\":\"Image alpine:latest\",\"status\":\"Working\",\"text\":\"Pulling\"}\n")}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Pull:              "always",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if response.Changed {
		t.Fatal("pull-only up should be unchanged")
	}
	if len(response.Actions) != 1 || response.Actions[0].Status != "Pulling" {
		t.Fatalf("actions = %#v", response.Actions)
	}
}

func TestExecuteIgnoreBuildEvents(t *testing.T) {
	events := []byte("{\"id\":\"Image app\",\"status\":\"Working\",\"text\":\"Building\"}\n")
	ignore := true
	observe := false
	ignored := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Build:             "always",
		IgnoreBuildEvents: &ignore,
	}, docker.Dependencies{
		FileSystem: newComposeFileSystem("/project"),
		CLIRunner:  &scriptedCLI{results: map[string][]docker.CLIResult{"up": {{Stderr: events}}}},
	})
	if ignored.Changed {
		t.Fatal("ignored build should be unchanged")
	}
	observed := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Build:             "always",
		IgnoreBuildEvents: &observe,
	}, docker.Dependencies{
		FileSystem: newComposeFileSystem("/project"),
		CLIRunner:  &scriptedCLI{results: map[string][]docker.CLIResult{"up": {{Stderr: events}}}},
	})
	if !observed.Changed {
		t.Fatal("observed build should be changed")
	}
}

func TestExecuteDefinitionWritesComposeFile(t *testing.T) {
	filesystem := &composeFileSystem{dirs: map[string]bool{}, files: map[string][]byte{}}
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"up": {{Stderr: creatingEvent()}}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectName: "inline",
			Definition:  map[string]any{"services": map[string]any{"web": map[string]any{"image": "alpine"}}},
		},
	}, docker.Dependencies{Environment: docker.StaticEnvironment{}, FileSystem: filesystem, CLIRunner: runner, Clock: docker.SystemClock{}})
	if response.Failed {
		t.Fatalf("definition = %#v", response)
	}
	args := lastAction(t, runner, "up").Args
	if !containsArgs(args, "inline") {
		t.Fatalf("args = %#v", args)
	}
	foundDir := false
	for index, arg := range args {
		if arg == "--project-directory" && index+1 < len(args) && strings.Contains(args[index+1], "dibra-compose-inline-") {
			foundDir = true
		}
	}
	if !foundDir {
		t.Fatalf("missing definition temp directory in %#v", args)
	}
}

func TestExecuteFailedPullUsesUpstreamMessage(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{
			ExitCode: 1,
			Stderr:   []byte("{\"id\":\"does-not-exist\",\"status\":\"Error\",\"message\":\"pull access denied\"}\n"),
		}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Pull:              "never",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if !response.Failed || !strings.Contains(response.Msg, "Error when processing does-not-exist: pull access denied") {
		t.Fatalf("failure = %#v", response)
	}
}

func TestExecuteValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "missing src", request: Request{}, want: "one of the following is required"},
		{name: "definition without name", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{Definition: map[string]any{"services": map[string]any{}}}}, want: "project_name is required"},
		{name: "mutual exclusive", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/p", Definition: map[string]any{"a": 1}}}, want: "mutually exclusive"},
		{name: "bad state", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, State: "running"}, want: "state must be"},
		{name: "missing compose file", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty"}}, want: "does not contain"},
		{name: "missing named file", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project", Files: []string{"missing.yaml"}}}, want: "Cannot find Compose file"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			filesystem := newComposeFileSystem("/project")
			filesystem.dirs["/empty"] = true
			response := ExecuteWithDependencies(test.request, docker.Dependencies{
				FileSystem: filesystem,
				CLIRunner:  &scriptedCLI{},
			})
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestExecuteListsContainersAndImages(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up":     {{}},
		"ps":     {{Stdout: []byte("{\"Name\":\"demo-web-1\",\"Image\":\"alpine:latest\",\"State\":\"running\",\"Labels\":\"com.docker.compose.service=web\"}\n")}},
		"images": {{Stdout: []byte("{\"ID\":\"sha256:1\",\"Repository\":\"alpine\",\"Tag\":\"latest\",\"ContainerName\":\"demo-web-1\"}\n")}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if response.Failed || len(response.Containers) != 1 || len(response.Images) != 1 {
		t.Fatalf("response = %#v", response)
	}
	names, _ := response.Containers[0]["Names"].([]any)
	if len(names) != 1 || names[0] != "demo-web-1" {
		t.Fatalf("containers = %#v", response.Containers[0])
	}
	labels, _ := response.Containers[0]["Labels"].(map[string]any)
	if labels["com.docker.compose.service"] != "web" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestExecuteFailsWhenComposeImagesFailsAfterSuccessfulUp(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{Stderr: creatingEvent()}},
		"ps": {{Stdout: []byte(`{"Name":"demo-web-1","Image":"app:latest","State":"running"}`)}},
		"images": {{
			ExitCode: 1,
			Stderr:   []byte(`{"error":true,"message":"Error response from daemon: No such image: sha256:deadbeef"}`),
		}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Build:             "always",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if !response.Failed || response.Changed || response.RC != 1 {
		t.Fatalf("response = %#v", response)
	}
	if !strings.Contains(response.Cmd, " images --format json") || !strings.Contains(response.Stderr, "No such image") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteCheckFilesExistingFalseSkipsDefaultLookup(t *testing.T) {
	skip := false
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"up": {{Stderr: creatingEvent()}}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty", CheckFilesExisting: &skip},
	}, docker.Dependencies{
		FileSystem: &composeFileSystem{dirs: map[string]bool{"/empty": true}, files: map[string][]byte{}},
		CLIRunner:  runner,
	})
	if response.Failed {
		t.Fatalf("check_files_existing=false = %#v", response)
	}
	if composeAction(lastAction(t, runner, "up").Args) != "up" {
		t.Fatal("expected up")
	}
}

func TestExecuteFailedWaitListsContainers(t *testing.T) {
	waitTimeout := 5
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"up": {{
			ExitCode: 1,
			Stderr:   []byte("{\"id\":\"Container demo-web-1\",\"status\":\"Error\",\"text\":\"exited (0)\"}\n"),
		}},
		"ps":     {{Stdout: []byte(`{"Name":"demo-web-1","Image":"alpine:latest","State":"exited","ExitCode":0}`)}},
		"images": {{Stdout: []byte(`{"Repository":"alpine","Tag":"latest","ContainerName":"demo-web-1"}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Wait:              true,
		WaitTimeout:       &waitTimeout,
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if !response.Failed || !strings.Contains(response.Msg, "exited (0)") {
		t.Fatalf("wait failure = %#v", response)
	}
	if !containsArgs(lastAction(t, runner, "up").Args, "--wait-timeout") {
		t.Fatalf("missing wait-timeout: %#v", lastAction(t, runner, "up").Args)
	}
	if len(response.Containers) != 1 || len(response.Images) != 1 {
		t.Fatalf("listed resources = %#v", response)
	}
}

func TestBoolPullAndBuildCompatibility(t *testing.T) {
	var pull PullPolicy
	if err := pull.UnmarshalJSON([]byte("true")); err != nil || pull != "always" {
		t.Fatalf("pull true = %q (%v)", pull, err)
	}
	if err := pull.UnmarshalJSON([]byte("false")); err != nil || pull != "policy" {
		t.Fatalf("pull false = %q (%v)", pull, err)
	}
	var build BuildPolicy
	if err := build.UnmarshalJSON([]byte(`"policy"`)); err != nil || build != "policy" {
		t.Fatalf("build = %q (%v)", build, err)
	}
}

func lastAction(t *testing.T, runner *scriptedCLI, action string) docker.CLICommand {
	t.Helper()
	for index := len(runner.commands) - 1; index >= 0; index-- {
		if composeAction(runner.commands[index].Args) == action {
			return runner.commands[index]
		}
	}
	t.Fatalf("no %s command in %#v", action, runner.commands)
	return docker.CLICommand{}
}

func containsArgs(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
