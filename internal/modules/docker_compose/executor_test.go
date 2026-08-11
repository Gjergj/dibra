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
	command docker.CLICommand
	result  docker.CLIResult
}

func (runner *recordingCLIRunner) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.command = command
	return runner.result, nil
}

func TestExecuteWithDependenciesUsesInjectedFilesystemEnvironmentAndCLI(t *testing.T) {
	runner := &recordingCLIRunner{result: docker.CLIResult{Output: []byte("Container web Created\n")}}
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
	if runner.command.Name != "docker" || runner.command.Dir != "/project" {
		t.Errorf("command = %#v", runner.command)
	}
	wantArgs := []string{"--host", "unix:///tmp/docker.sock", "compose", "--project-name", "demo", "--ansi", "never", "up", "-d"}
	if !reflect.DeepEqual(runner.command.Args, wantArgs) {
		t.Errorf("args = %#v, want %#v", runner.command.Args, wantArgs)
	}
}
