package docker_compose

import (
	"context"
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type directoryInfo struct{}

func (directoryInfo) Name() string       { return "project" }
func (directoryInfo) Size() int64        { return 0 }
func (directoryInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (directoryInfo) ModTime() time.Time { return time.Time{} }
func (directoryInfo) IsDir() bool        { return true }
func (directoryInfo) Sys() any           { return nil }

type composeFileSystem struct{ docker.FileSystem }

func (composeFileSystem) Stat(string) (fs.FileInfo, error) { return directoryInfo{}, nil }

type recordingCLIRunner struct {
	commands []docker.CLICommand
	results  []docker.CLIResult
}

func (runner *recordingCLIRunner) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	result := runner.results[0]
	runner.results = runner.results[1:]
	return result, nil
}

func TestExecuteWithDependenciesUsesInjectedFilesystemEnvironmentAndCLI(t *testing.T) {
	runner := &recordingCLIRunner{results: []docker.CLIResult{
		{Output: []byte(`{"version":"v5.4.0"}`)},
		{Output: []byte("{\"id\":\"Container web\",\"status\":\"Working\",\"text\":\"Creating\"}\n")},
	}}
	response := ExecuteWithDependencies(Request{
		ComposeCommonArgs: docker.ComposeCommonArgs{ProjectSrc: "/project", ProjectName: "demo"},
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_HOST": "unix:///tmp/docker.sock"},
		FileSystem:  composeFileSystem{},
		CLIRunner:   runner,
	})

	if response.Failed || !response.Changed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	command := runner.commands[1]
	if command.Name != "docker" || command.Dir != "/project" {
		t.Errorf("command = %#v", command)
	}
	wantArgs := []string{"--host", "unix:///tmp/docker.sock", "compose", "--project-name", "demo", "--ansi", "never", "--progress", "json", "up", "-d"}
	if !reflect.DeepEqual(command.Args, wantArgs) {
		t.Errorf("args = %#v, want %#v", command.Args, wantArgs)
	}
}
