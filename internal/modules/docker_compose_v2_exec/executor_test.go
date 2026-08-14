package docker_compose_v2_exec

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type testFileInfo struct {
	name string
	dir  bool
}

func (info testFileInfo) Name() string       { return info.name }
func (info testFileInfo) Size() int64        { return 0 }
func (info testFileInfo) Mode() fs.FileMode  { return 0o644 }
func (info testFileInfo) ModTime() time.Time { return time.Time{} }
func (info testFileInfo) IsDir() bool        { return info.dir }
func (info testFileInfo) Sys() any           { return nil }

type execFileSystem struct {
	docker.FileSystem
	dirs    map[string]bool
	files   map[string][]byte
	removed []string
}

func newExecFileSystem(project string) *execFileSystem {
	return &execFileSystem{
		dirs:  map[string]bool{project: true},
		files: map[string][]byte{filepath.Join(project, "compose.yaml"): []byte("services: {}\n")},
	}
}

func (filesystem *execFileSystem) Stat(path string) (fs.FileInfo, error) {
	if filesystem.dirs[path] {
		return testFileInfo{name: filepath.Base(path), dir: true}, nil
	}
	if _, ok := filesystem.files[path]; ok {
		return testFileInfo{name: filepath.Base(path)}, nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *execFileSystem) Abs(path string) (string, error) { return path, nil }
func (filesystem *execFileSystem) TempDir() string                 { return "/tmp" }
func (filesystem *execFileSystem) MkdirAll(path string, _ fs.FileMode) error {
	filesystem.dirs[path] = true
	return nil
}
func (filesystem *execFileSystem) WriteFile(path string, data []byte, _ fs.FileMode) error {
	filesystem.files[path] = append([]byte(nil), data...)
	return nil
}
func (filesystem *execFileSystem) RemoveAll(path string) error {
	filesystem.removed = append(filesystem.removed, path)
	delete(filesystem.dirs, path)
	for file := range filesystem.files {
		if strings.HasPrefix(file, path) {
			delete(filesystem.files, file)
		}
	}
	return nil
}

type execCLI struct {
	commands []docker.CLICommand
	stdins   []string
	results  []docker.CLIResult
}

func (runner *execCLI) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	if command.Stdin != nil {
		data, _ := io.ReadAll(command.Stdin)
		runner.stdins = append(runner.stdins, string(data))
	} else {
		runner.stdins = append(runner.stdins, "")
	}
	if composeExecAction(command.Args) == "version" {
		return docker.CLIResult{Output: []byte(`{"version":"v5.4.0"}`)}, nil
	}
	if len(runner.results) == 0 {
		return docker.CLIResult{}, nil
	}
	result := runner.results[0]
	runner.results = runner.results[1:]
	if result.ExitCode != 0 {
		return result, fmt.Errorf("exit status %d", result.ExitCode)
	}
	return result, nil
}

func composeExecAction(args []string) string {
	for index, arg := range args {
		if arg != "compose" {
			continue
		}
		for _, candidate := range args[index+1:] {
			switch candidate {
			case "version", "exec":
				return candidate
			}
		}
	}
	return ""
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func intPointer(value int) *int          { return &value }

func TestExecuteBuildsCompleteExecCommand(t *testing.T) {
	host := "unix:///explicit.sock"
	runner := &execCLI{results: []docker.CLIResult{{
		Stdout: []byte("output\n\n"),
		Stderr: []byte("warning\r\n"),
	}}}
	response := ExecuteWithDependencies(Request{
		CommonArgs: docker.CommonArgs{DockerHost: &host},
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectSrc:  "/project",
			ProjectName: "demo",
			Files:       []string{"compose.yaml"},
			EnvFiles:    []string{".env"},
			Profiles:    []string{"workers"},
		},
		Service:         "worker",
		Index:           intPointer(2),
		Argv:            []string{"sh", "-c", "printf ok"},
		Chdir:           stringPointer("/work"),
		User:            stringPointer("1234"),
		Stdin:           stringPointer("input"),
		StdinAddNewline: boolPointer(true),
		StripEmptyEnds:  boolPointer(true),
		Privileged:      true,
		TTY:             boolPointer(false),
		Env:             map[string]any{"ZETA": "last", "ALPHA": "first"},
		DockerCLI:       "/usr/bin/docker",
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_HOST": "unix:///ambient.sock"},
		FileSystem:  newExecFileSystem("/project"),
		CLIRunner:   runner,
	})
	if response.Failed || !response.Changed || response.RC == nil || *response.RC != 0 {
		t.Fatalf("response = %#v", response)
	}
	if response.Stdout == nil || *response.Stdout != "output" || response.Stderr == nil || *response.Stderr != "warning" {
		t.Fatalf("output = %#v", response)
	}
	command := lastExecCommand(t, runner)
	want := []string{
		"--host", "unix:///explicit.sock", "compose", "--ansi", "never", "--progress", "plain",
		"--project-directory", "/project", "--project-name", "demo",
		"--file", "compose.yaml", "--env-file", ".env", "--profile", "workers",
		"exec", "--index", "2", "--workdir", "/work", "--user", "1234",
		"--privileged", "--no-tty", "--env", "ALPHA=first", "--env", "ZETA=last",
		"--", "worker", "sh", "-c", "printf ok",
	}
	if command.Name != "/usr/bin/docker" || command.Dir != "/project" || !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("command = %#v\nwant args = %#v", command, want)
	}
	if got := runner.stdins[len(runner.stdins)-1]; got != "input\n" {
		t.Fatalf("stdin = %q", got)
	}
}

func TestExecuteCommandSplittingAndDefaults(t *testing.T) {
	command := `/bin/sh -c "printf '%s' 'hello world'"`
	runner := &execCLI{results: []docker.CLIResult{{Stdout: []byte("hello world\r\n\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Command:           &command,
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: runner})
	if response.Failed || response.Stdout == nil || *response.Stdout != "hello world" {
		t.Fatalf("response = %#v", response)
	}
	args := lastExecCommand(t, runner).Args
	wantTail := []string{"--", "web", "/bin/sh", "-c", "printf '%s' 'hello world'"}
	if !reflect.DeepEqual(args[len(args)-len(wantTail):], wantTail) || containsArg(args, "--no-tty") {
		t.Fatalf("args = %#v", args)
	}
}

func TestExecuteExplicitStdinAndStripControls(t *testing.T) {
	command := "cat"
	runner := &execCLI{results: []docker.CLIResult{{Stdout: []byte("out\n\n"), Stderr: []byte("err\n\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Command:           &command,
		Stdin:             stringPointer("value"),
		StdinAddNewline:   boolPointer(false),
		StripEmptyEnds:    boolPointer(false),
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: runner})
	if response.Stdout == nil || *response.Stdout != "out\n\n" || response.Stderr == nil || *response.Stderr != "err\n\n" {
		t.Fatalf("response = %#v", response)
	}
	if got := runner.stdins[len(runner.stdins)-1]; got != "value" {
		t.Fatalf("stdin = %q", got)
	}
}

func TestExecuteSynchronousNonzeroIsSuccessfulModuleResult(t *testing.T) {
	runner := &execCLI{results: []docker.CLIResult{{ExitCode: 1, Stderr: []byte("unknown uid\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Argv:              []string{"whoami"},
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: runner})
	if response.Failed || !response.Changed || response.RC == nil || *response.RC != 1 {
		t.Fatalf("response = %#v", response)
	}
	if response.Stdout == nil || *response.Stdout != "" || response.Stderr == nil || *response.Stderr != "unknown uid" {
		t.Fatalf("output = %#v", response)
	}
}

func TestExecuteDetachedReturnsNoSynchronousFields(t *testing.T) {
	runner := &execCLI{results: []docker.CLIResult{{}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Argv:              []string{"sleep", "1"},
		Detach:            true,
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: runner})
	if response.Failed || response.Changed || response.RC != nil || response.Stdout != nil || response.Stderr != nil {
		t.Fatalf("response = %#v", response)
	}
	if !containsArg(lastExecCommand(t, runner).Args, "--detach") {
		t.Fatal("missing --detach")
	}
}

func TestExecuteDetachedNonzeroFails(t *testing.T) {
	runner := &execCLI{results: []docker.CLIResult{{ExitCode: 1, Stderr: []byte("service is not running\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Argv:              []string{"true"},
		Detach:            true,
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: runner})
	if !response.Failed || !strings.Contains(response.Msg, "not running") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteDefinitionCreatesAndRemovesTemporaryProject(t *testing.T) {
	filesystem := &execFileSystem{dirs: map[string]bool{}, files: map[string][]byte{}}
	runner := &execCLI{}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectName: "inline",
			Definition:  map[string]any{"services": map[string]any{"web": map[string]any{"image": "alpine"}}},
		},
		Service: "web",
		Argv:    []string{"true"},
	}, docker.Dependencies{FileSystem: filesystem, CLIRunner: runner, Clock: fixedExecClock{}})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	if !containsSequence(lastExecCommand(t, runner).Args, "--project-directory", "/tmp/dibra-compose-inline-123") {
		t.Fatalf("args = %#v", lastExecCommand(t, runner).Args)
	}
	if !reflect.DeepEqual(filesystem.removed, []string{"/tmp/dibra-compose-inline-123"}) {
		t.Fatalf("removed = %#v", filesystem.removed)
	}
}

type fixedExecClock struct{ docker.Clock }

func (fixedExecClock) Now() time.Time { return time.Unix(0, 123) }

func TestExecuteValidation(t *testing.T) {
	command := "echo ok"
	empty := ""
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "missing project", request: Request{Service: "web", Command: &command}, want: "one of the following is required: definition, project_src"},
		{name: "missing service", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Command: &command}, want: "service is required"},
		{name: "missing command", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web"}, want: "required: argv, command"},
		{name: "command and argv", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web", Command: &command, Argv: []string{"echo"}}, want: "mutually exclusive"},
		{name: "definition and src", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project", ProjectName: "x", Definition: map[string]any{"services": map[string]any{}}}, Service: "web", Command: &command}, want: "mutually exclusive"},
		{name: "definition and files", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectName: "x", Definition: map[string]any{"services": map[string]any{}}, Files: []string{"x.yml"}}, Service: "web", Command: &command}, want: "mutually exclusive"},
		{name: "definition without name", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{Definition: map[string]any{"services": map[string]any{}}}, Service: "web", Command: &command}, want: "project_name is required"},
		{name: "detach empty stdin", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web", Command: &command, Detach: true, Stdin: &empty}, want: "stdin cannot be provided"},
		{name: "non-string env", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web", Command: &command, Env: map[string]any{"COUNT": 2}}, want: "Non-string value"},
		{name: "missing compose file", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty"}, Service: "web", Command: &command}, want: "does not contain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filesystem := newExecFileSystem("/project")
			filesystem.dirs["/empty"] = true
			response := ExecuteWithDependencies(test.request, docker.Dependencies{FileSystem: filesystem, CLIRunner: &execCLI{}})
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestExecuteParseAndProcessStartFailures(t *testing.T) {
	badCommand := `echo "unterminated`
	parseFailure := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Command:           &badCommand,
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: &execCLI{}})
	if !parseFailure.Failed || !strings.Contains(parseFailure.Msg, "failed to parse command") {
		t.Fatalf("parse failure = %#v", parseFailure)
	}

	runner := &execCLI{results: []docker.CLIResult{{ExitCode: -1}}}
	processFailure := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Argv:              []string{"true"},
	}, docker.Dependencies{FileSystem: newExecFileSystem("/project"), CLIRunner: runner})
	if !processFailure.Failed || !strings.Contains(processFailure.Msg, "failed to execute command") {
		t.Fatalf("process failure = %#v", processFailure)
	}
}

func TestExecuteCheckFilesExistingFalse(t *testing.T) {
	skip := false
	filesystem := &execFileSystem{dirs: map[string]bool{"/empty": true}, files: map[string][]byte{}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty", CheckFilesExisting: &skip},
		Service:           "web",
		Argv:              []string{"true"},
	}, docker.Dependencies{FileSystem: filesystem, CLIRunner: &execCLI{}})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
}

func lastExecCommand(t *testing.T, runner *execCLI) docker.CLICommand {
	t.Helper()
	for index := len(runner.commands) - 1; index >= 0; index-- {
		if composeExecAction(runner.commands[index].Args) == "exec" {
			return runner.commands[index]
		}
	}
	t.Fatalf("no exec command in %#v", runner.commands)
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

func containsSequence(args []string, first, second string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == first && args[index+1] == second {
			return true
		}
	}
	return false
}
