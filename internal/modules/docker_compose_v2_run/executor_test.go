package docker_compose_v2_run

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

type runFileSystem struct {
	docker.FileSystem
	dirs    map[string]bool
	files   map[string][]byte
	removed []string
}

func newRunFileSystem(project string) *runFileSystem {
	return &runFileSystem{
		dirs:  map[string]bool{project: true},
		files: map[string][]byte{filepath.Join(project, "compose.yaml"): []byte("services: {}\n")},
	}
}

func (filesystem *runFileSystem) Stat(path string) (fs.FileInfo, error) {
	if filesystem.dirs[path] {
		return testFileInfo{name: filepath.Base(path), dir: true}, nil
	}
	if _, ok := filesystem.files[path]; ok {
		return testFileInfo{name: filepath.Base(path)}, nil
	}
	return nil, fs.ErrNotExist
}

func (filesystem *runFileSystem) Abs(path string) (string, error) { return path, nil }
func (filesystem *runFileSystem) TempDir() string                 { return "/tmp" }
func (filesystem *runFileSystem) MkdirAll(path string, _ fs.FileMode) error {
	filesystem.dirs[path] = true
	return nil
}
func (filesystem *runFileSystem) WriteFile(path string, data []byte, _ fs.FileMode) error {
	filesystem.files[path] = append([]byte(nil), data...)
	return nil
}
func (filesystem *runFileSystem) RemoveAll(path string) error {
	filesystem.removed = append(filesystem.removed, path)
	delete(filesystem.dirs, path)
	for file := range filesystem.files {
		if strings.HasPrefix(file, path) {
			delete(filesystem.files, file)
		}
	}
	return nil
}

type runCLI struct {
	commands []docker.CLICommand
	stdins   []string
	results  []docker.CLIResult
}

func (runner *runCLI) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	if command.Stdin != nil {
		data, _ := io.ReadAll(command.Stdin)
		runner.stdins = append(runner.stdins, string(data))
	} else {
		runner.stdins = append(runner.stdins, "")
	}
	if composeRunAction(command.Args) == "version" {
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

func composeRunAction(args []string) string {
	for index, arg := range args {
		if arg != "compose" {
			continue
		}
		for _, candidate := range args[index+1:] {
			switch candidate {
			case "version", "run":
				return candidate
			}
		}
	}
	return ""
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }

func TestExecuteBuildsCompleteRunCommand(t *testing.T) {
	host := "unix:///explicit.sock"
	runner := &runCLI{results: []docker.CLIResult{{
		Stdout: []byte("output\n\n"),
		Stderr: []byte("warning\n\n"),
	}}}
	response := ExecuteWithDependencies(Request{
		CommonArgs: docker.CommonArgs{DockerHost: &host},
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectSrc:  "/project",
			ProjectName: "demo",
			Files:       []string{"compose.yaml"},
			EnvFiles:    []string{".env"},
			Profiles:    []string{"tools"},
		},
		Service:         "web",
		Argv:            []string{"sh", "-c", "printf ok"},
		Build:           true,
		CapAdd:          []string{"NET_ADMIN"},
		CapDrop:         []string{"MKNOD"},
		EntryPoint:      stringPointer("/bin/sh"),
		Interactive:     boolPointer(false),
		Labels:          []string{"purpose=test"},
		Name:            stringPointer("one-off"),
		NoDeps:          true,
		Publish:         []string{"127.0.0.1:8080:80"},
		QuietPull:       true,
		RemoveOrphans:   true,
		Cleanup:         true,
		ServicePorts:    true,
		UseAliases:      true,
		Volumes:         []string{"/tmp:/work"},
		Chdir:           stringPointer("/work"),
		User:            stringPointer("1234"),
		TTY:             boolPointer(false),
		Env:             map[string]any{"ZETA": "last", "ALPHA": "first"},
		DockerCLI:       "/usr/bin/docker",
		StripEmptyEnds:  boolPointer(true),
		StdinAddNewline: boolPointer(true),
		Stdin:           stringPointer("input"),
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_HOST": "unix:///ambient.sock"},
		FileSystem:  newRunFileSystem("/project"),
		CLIRunner:   runner,
	})
	if response.Failed || !response.Changed || response.RC == nil || *response.RC != 0 {
		t.Fatalf("response = %#v", response)
	}
	if response.Stdout == nil || *response.Stdout != "output" || response.Stderr == nil || *response.Stderr != "warning" {
		t.Fatalf("output = %#v", response)
	}
	command := lastRunCommand(t, runner)
	if command.Name != "/usr/bin/docker" || command.Dir != "/project" {
		t.Fatalf("command = %#v", command)
	}
	want := []string{
		"--host", "unix:///explicit.sock", "compose", "--ansi", "never", "--progress", "plain",
		"--project-directory", "/project", "--project-name", "demo",
		"--file", "compose.yaml", "--env-file", ".env", "--profile", "tools",
		"run", "--build", "--cap-add", "NET_ADMIN", "--cap-drop", "MKNOD",
		"--entrypoint", "/bin/sh", "--interactive=false", "--label", "purpose=test",
		"--name", "one-off", "--no-deps", "--publish", "127.0.0.1:8080:80",
		"--quiet-pull", "--remove-orphans", "--rm", "--service-ports", "--use-aliases",
		"--volume", "/tmp:/work", "--workdir", "/work", "--user", "1234", "--no-tty",
		"--env", "ALPHA=first", "--env", "ZETA=last",
		"--", "web", "sh", "-c", "printf ok",
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Errorf("args = %#v\nwant = %#v", command.Args, want)
	}
	if got := runner.stdins[len(runner.stdins)-1]; got != "input\n" {
		t.Fatalf("stdin = %q", got)
	}
}

func TestExecuteCommandUsesShellStyleSplitting(t *testing.T) {
	command := `/bin/sh -c "printf '%s' 'hello world'"`
	runner := &runCLI{results: []docker.CLIResult{{Stdout: []byte("hello world\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Command:           &command,
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: runner})
	if response.Failed || response.Stdout == nil || *response.Stdout != "hello world" {
		t.Fatalf("response = %#v", response)
	}
	wantTail := []string{"--", "web", "/bin/sh", "-c", "printf '%s' 'hello world'"}
	args := lastRunCommand(t, runner).Args
	if !reflect.DeepEqual(args[len(args)-len(wantTail):], wantTail) {
		t.Fatalf("args tail = %#v, want %#v", args, wantTail)
	}
}

func TestExecuteDefaultAndExplicitOutputControls(t *testing.T) {
	defaultRunner := &runCLI{results: []docker.CLIResult{{Stdout: []byte("out\r\n\n"), Stderr: []byte("err\r\n")}}}
	defaults := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Stdin:             stringPointer("value"),
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: defaultRunner})
	if defaults.Stdout == nil || *defaults.Stdout != "out" || defaults.Stderr == nil || *defaults.Stderr != "err" {
		t.Fatalf("defaults = %#v", defaults)
	}
	if got := defaultRunner.stdins[len(defaultRunner.stdins)-1]; got != "value\n" {
		t.Fatalf("default stdin = %q", got)
	}
	defaultArgs := lastRunCommand(t, defaultRunner).Args
	if containsRunArg(defaultArgs, "--interactive=false") || containsRunArg(defaultArgs, "--no-tty") {
		t.Fatalf("default true flags emitted negative options: %#v", defaultArgs)
	}

	explicitRunner := &runCLI{results: []docker.CLIResult{{Stdout: []byte("out\n\n"), Stderr: []byte("err\n\n")}}}
	explicit := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Stdin:             stringPointer("value"),
		StdinAddNewline:   boolPointer(false),
		StripEmptyEnds:    boolPointer(false),
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: explicitRunner})
	if explicit.Stdout == nil || *explicit.Stdout != "out\n\n" || explicit.Stderr == nil || *explicit.Stderr != "err\n\n" {
		t.Fatalf("explicit = %#v", explicit)
	}
	if got := explicitRunner.stdins[len(explicitRunner.stdins)-1]; got != "value" {
		t.Fatalf("explicit stdin = %q", got)
	}
}

func TestExecuteSynchronousNonzeroIsSuccessfulModuleResult(t *testing.T) {
	runner := &runCLI{results: []docker.CLIResult{{
		ExitCode: 1,
		Stderr:   []byte("whoami: unknown uid 1234\n"),
	}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Argv:              []string{"whoami"},
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: runner})
	if response.Failed || !response.Changed || response.RC == nil || *response.RC != 1 {
		t.Fatalf("response = %#v", response)
	}
	if response.Stdout == nil || *response.Stdout != "" || response.Stderr == nil || !strings.Contains(*response.Stderr, "unknown uid") {
		t.Fatalf("output = %#v", response)
	}
}

func TestExecuteDetachedReturnsOnlyContainerID(t *testing.T) {
	runner := &runCLI{results: []docker.CLIResult{{Stdout: []byte("abcdef123456\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Detach:            true,
		Cleanup:           true,
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: runner})
	if response.Failed || response.Changed || response.ContainerID != "abcdef123456" {
		t.Fatalf("response = %#v", response)
	}
	if response.RC != nil || response.Stdout != nil || response.Stderr != nil {
		t.Fatalf("detached response leaked synchronous fields: %#v", response)
	}
	if !containsRunArg(lastRunCommand(t, runner).Args, "--detach") {
		t.Fatal("missing --detach")
	}
}

func TestExecuteDetachedNonzeroFails(t *testing.T) {
	runner := &runCLI{results: []docker.CLIResult{{ExitCode: 1, Stderr: []byte("no such service: web\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Detach:            true,
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: runner})
	if !response.Failed || !strings.Contains(response.Msg, "no such service") {
		t.Fatalf("response = %#v", response)
	}
}

func TestExecuteDefinitionCreatesAndRemovesTemporaryProject(t *testing.T) {
	filesystem := &runFileSystem{dirs: map[string]bool{}, files: map[string][]byte{}}
	runner := &runCLI{results: []docker.CLIResult{{Stdout: []byte("ok\n")}}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{
			ProjectName: "inline",
			Definition: map[string]any{
				"services": map[string]any{"web": map[string]any{"image": "alpine"}},
			},
		},
		Service: "web",
	}, docker.Dependencies{
		FileSystem: filesystem,
		CLIRunner:  runner,
		Clock:      fixedRunClock{},
	})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	command := lastRunCommand(t, runner)
	if !containsRunSequence(command.Args, "--project-directory", "/tmp/dibra-compose-inline-123") {
		t.Fatalf("args = %#v", command.Args)
	}
	if len(filesystem.removed) != 1 || filesystem.removed[0] != "/tmp/dibra-compose-inline-123" {
		t.Fatalf("removed = %#v", filesystem.removed)
	}
}

type fixedRunClock struct{ docker.Clock }

func (fixedRunClock) Now() time.Time { return time.Unix(0, 123) }

func TestExecuteValidation(t *testing.T) {
	command := "echo ok"
	empty := ""
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "missing project", request: Request{Service: "web"}, want: "one of the following is required"},
		{name: "missing service", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}}, want: "service is required"},
		{name: "definition and src", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project", ProjectName: "x", Definition: map[string]any{"services": map[string]any{}}}, Service: "web"}, want: "mutually exclusive"},
		{name: "definition and files", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectName: "x", Definition: map[string]any{"services": map[string]any{}}, Files: []string{"x.yml"}}, Service: "web"}, want: "mutually exclusive"},
		{name: "definition without name", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{Definition: map[string]any{"services": map[string]any{}}}, Service: "web"}, want: "project_name is required"},
		{name: "command and argv", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web", Command: &command, Argv: []string{"echo"}}, want: "mutually exclusive"},
		{name: "detach with empty stdin", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web", Detach: true, Stdin: &empty}, want: "stdin cannot be provided"},
		{name: "non-string env", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"}, Service: "web", Env: map[string]any{"COUNT": 2}}, want: "Non-string value"},
		{name: "missing compose file", request: Request{ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty"}, Service: "web"}, want: "does not contain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filesystem := newRunFileSystem("/project")
			filesystem.dirs["/empty"] = true
			response := ExecuteWithDependencies(test.request, docker.Dependencies{
				FileSystem: filesystem,
				CLIRunner:  &runCLI{},
			})
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestExecuteCommandParseAndProcessStartFailures(t *testing.T) {
	badCommand := `echo "unterminated`
	parseFailure := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
		Command:           &badCommand,
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: &runCLI{}})
	if !parseFailure.Failed || !strings.Contains(parseFailure.Msg, "failed to parse command") {
		t.Fatalf("parse failure = %#v", parseFailure)
	}

	runner := &runCLI{results: []docker.CLIResult{{ExitCode: -1}}}
	processFailure := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project"},
		Service:           "web",
	}, docker.Dependencies{FileSystem: newRunFileSystem("/project"), CLIRunner: runner})
	if !processFailure.Failed || !strings.Contains(processFailure.Msg, "failed to execute command") {
		t.Fatalf("process failure = %#v", processFailure)
	}
}

func TestExecuteCheckFilesExistingFalse(t *testing.T) {
	skip := false
	filesystem := &runFileSystem{dirs: map[string]bool{"/empty": true}, files: map[string][]byte{}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/empty", CheckFilesExisting: &skip},
		Service:           "web",
	}, docker.Dependencies{FileSystem: filesystem, CLIRunner: &runCLI{}})
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
}

func lastRunCommand(t *testing.T, runner *runCLI) docker.CLICommand {
	t.Helper()
	for index := len(runner.commands) - 1; index >= 0; index-- {
		if composeRunAction(runner.commands[index].Args) == "run" {
			return runner.commands[index]
		}
	}
	t.Fatalf("no run command in %#v", runner.commands)
	return docker.CLICommand{}
}

func containsRunArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsRunSequence(args []string, first, second string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == first && args[index+1] == second {
			return true
		}
	}
	return false
}
