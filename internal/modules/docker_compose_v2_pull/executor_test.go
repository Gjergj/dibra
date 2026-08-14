package docker_compose_v2_pull

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
			case "version", "pull":
				return candidate
			}
		}
	}
	return ""
}

func pullingEvent() []byte {
	return []byte("{\"id\":\"Image alpine:latest\",\"status\":\"Working\",\"text\":\"Pulling\"}\n")
}

func layerPullEvent() []byte {
	return []byte("{\"id\":\"sha256:layer\",\"parent_id\":\"Image alpine:latest\",\"status\":\"Working\",\"text\":\"Downloading\"}\n")
}

func TestExecuteAlwaysOmitsDefaultPolicyAndUsesPinnedFlags(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"pull": {{Stderr: pullingEvent()}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project", ProjectName: "demo"},
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_HOST": "unix:///tmp/docker.sock"},
		FileSystem:  newComposeFileSystem("/project"),
		CLIRunner:   runner,
	})
	if response.Failed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if response.Changed {
		t.Fatal("policy=always real pull should ignore Pulling for changed")
	}
	if len(response.Actions) != 1 || response.Actions[0] != (docker.ComposeAction{What: "image", ID: "alpine:latest", Status: "Pulling"}) {
		t.Fatalf("actions = %#v", response.Actions)
	}
	command := lastAction(t, runner, "pull")
	if command.Name != "docker" || command.Dir != "/project" {
		t.Errorf("command = %#v", command)
	}
	want := []string{
		"--host", "unix:///tmp/docker.sock", "compose", "--ansi", "never", "--progress", "json",
		"--project-directory", "/project", "--project-name", "demo",
		"pull", "--",
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Errorf("args = %#v, want %#v", command.Args, want)
	}
}

func TestExecuteAlwaysCheckModeCountsPullingAsChanged(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"pull": {{Stderr: pullingEvent()}},
	}}
	response := ExecuteWithDependenciesAndState(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
	}, docker.Dependencies{
		FileSystem: newComposeFileSystem("/project"),
		CLIRunner:  runner,
	}, execution.State{CheckMode: true})
	if response.Failed || !response.Changed {
		t.Fatalf("check mode always = %#v", response)
	}
	if !containsArgs(lastAction(t, runner, "pull").Args, "--dry-run") {
		t.Fatalf("missing --dry-run: %#v", lastAction(t, runner, "pull").Args)
	}
	if containsArgs(lastAction(t, runner, "pull").Args, "--policy") {
		t.Fatalf("default always should omit --policy: %#v", lastAction(t, runner, "pull").Args)
	}
}

func TestExecuteMissingPolicyCountsPullingAsChanged(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"pull": {{Stderr: pullingEvent()}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Policy:            "missing",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if response.Failed || !response.Changed {
		t.Fatalf("policy=missing = %#v", response)
	}
	command := lastAction(t, runner, "pull")
	if !containsArgs(command.Args, "--policy") || !containsArgs(command.Args, "missing") {
		t.Fatalf("missing policy flags: %#v", command.Args)
	}
}

func TestExecuteAlwaysLayerProgressIsChanged(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"pull": {{Stderr: append(pullingEvent(), layerPullEvent()...)}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Policy:            "always",
	}, docker.Dependencies{FileSystem: newComposeFileSystem("/project"), CLIRunner: runner})
	if response.Failed || !response.Changed {
		t.Fatalf("layer progress = %#v", response)
	}
}

func TestExecutePullFlagsAndServices(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"pull": {{Stderr: pullingEvent()}}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectSrc: "/project",
			Files:      []string{"compose.yaml", "override.yaml"},
			EnvFiles:   []string{".env"},
			Profiles:   []string{"web"},
		},
		Policy:             "missing",
		IgnoreBuildable:    true,
		IgnorePullFailures: true,
		IncludeDeps:        true,
		Services:           []string{"web", "db"},
		DockerCLI:          "/usr/bin/docker",
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
	command := lastAction(t, runner, "pull")
	if command.Name != "/usr/bin/docker" {
		t.Fatalf("cli = %s", command.Name)
	}
	for _, want := range []string{
		"--file", "compose.yaml", "--file", "override.yaml", "--env-file", ".env", "--profile", "web",
		"--policy", "missing", "--ignore-buildable", "--ignore-pull-failures", "--include-deps", "--", "web", "db",
	} {
		if !containsArgs(command.Args, want) {
			t.Errorf("missing %q in %#v", want, command.Args)
		}
	}
}

func TestExecuteDefinitionWritesComposeFile(t *testing.T) {
	filesystem := &composeFileSystem{dirs: map[string]bool{}, files: map[string][]byte{}}
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"pull": {{Stderr: pullingEvent()}}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectName: "inline",
			Definition:  map[string]any{"services": map[string]any{"web": map[string]any{"image": "alpine"}}},
		},
	}, docker.Dependencies{Environment: docker.StaticEnvironment{}, FileSystem: filesystem, CLIRunner: runner, Clock: docker.SystemClock{}})
	if response.Failed {
		t.Fatalf("definition = %#v", response)
	}
	args := lastAction(t, runner, "pull").Args
	foundDir := false
	for index, arg := range args {
		if arg == "--project-directory" && index+1 < len(args) && strings.Contains(args[index+1], "dibra-compose-inline-") {
			foundDir = true
		}
	}
	if !foundDir {
		t.Fatalf("missing definition temp directory in %#v", args)
	}
	if !containsArgs(args, "inline") {
		t.Fatalf("missing project name in %#v", args)
	}
}

func TestExecuteFailedPullUsesUpstreamMessage(t *testing.T) {
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{
		"pull": {{
			ExitCode: 1,
			Stderr:   []byte("{\"id\":\"does-not-exist\",\"status\":\"Error\",\"message\":\"pull access denied\"}\n"),
		}},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
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
		{name: "mutual exclusive src", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/p", Definition: map[string]any{"a": 1}}}, want: "mutually exclusive"},
		{name: "mutual exclusive files", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{Definition: map[string]any{"a": 1}, Files: []string{"compose.yaml"}, ProjectName: "x"}}, want: "mutually exclusive"},
		{name: "bad policy", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Policy: "never"}, want: "policy must be"},
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

func TestExecuteCheckFilesExistingFalseSkipsDefaultLookup(t *testing.T) {
	skip := false
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"pull": {{Stderr: pullingEvent()}}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty", CheckFilesExisting: &skip},
	}, docker.Dependencies{
		FileSystem: &composeFileSystem{dirs: map[string]bool{"/empty": true}, files: map[string][]byte{}},
		CLIRunner:  runner,
	})
	if response.Failed {
		t.Fatalf("check_files_existing=false = %#v", response)
	}
	if composeAction(lastAction(t, runner, "pull").Args) != "pull" {
		t.Fatal("expected pull")
	}
}

func TestExecuteExplicitDockerHostSuppressesEnvironment(t *testing.T) {
	host := "unix:///explicit.sock"
	runner := &scriptedCLI{results: map[string][]docker.CLIResult{"pull": {{Stderr: pullingEvent()}}}}
	response := ExecuteWithDependencies(Request{
		CommonArgs:        docker.CommonArgs{DockerHost: &host},
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_HOST": "unix:///env.sock"},
		FileSystem:  newComposeFileSystem("/project"),
		CLIRunner:   runner,
	})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	args := lastAction(t, runner, "pull").Args
	if !containsArgs(args, "unix:///explicit.sock") {
		t.Fatalf("missing explicit host: %#v", args)
	}
	if containsArgs(args, "unix:///env.sock") {
		t.Fatalf("environment host leaked: %#v", args)
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
