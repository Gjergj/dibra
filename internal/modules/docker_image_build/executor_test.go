package docker_image_build

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type testFileInfo struct {
	name string
	mode fs.FileMode
}

func (info testFileInfo) Name() string       { return info.name }
func (info testFileInfo) Size() int64        { return 0 }
func (info testFileInfo) Mode() fs.FileMode  { return info.mode }
func (info testFileInfo) ModTime() time.Time { return time.Time{} }
func (info testFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info testFileInfo) Sys() any           { return nil }

type buildFileSystem struct {
	docker.FileSystem
	files map[string]fs.FileMode
}

func (fileSystem buildFileSystem) Stat(path string) (fs.FileInfo, error) {
	mode, found := fileSystem.files[path]
	if !found {
		return nil, fs.ErrNotExist
	}
	return testFileInfo{name: path, mode: mode}, nil
}

type cliReply struct {
	result docker.CLIResult
	err    error
}

type buildRunner struct {
	commands []docker.CLICommand
	replies  []cliReply
}

func (runner *buildRunner) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	if len(runner.replies) == 0 {
		return docker.CLIResult{}, errors.New("unexpected command")
	}
	reply := runner.replies[0]
	runner.replies = runner.replies[1:]
	return reply.result, reply.err
}

func buildDependencies(runner *buildRunner, files map[string]fs.FileMode) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  buildFileSystem{files: files},
		CLIRunner:   runner,
	}
}

func TestBuildUsesPinnedUpstreamCommandContractAndRawInspection(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("Error: No such image")}, err: errors.New("exit 1")},
		{result: docker.CLIResult{Stdout: []byte("stdout"), Stderr: []byte("build progress")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new","Config":{"Labels":{"release":"1"}}}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		Name:       "example:v1",
		Tag:        "ignored",
		Path:       "/context",
		Dockerfile: "Dockerfile.custom",
		CacheFrom:  StringList{"cache:v1"},
		Pull:       true,
		Network:    "host",
		NoCache:    true,
		EtcHosts:   map[string]any{"host.test": "host-gateway"},
		Args:       map[string]any{"COUNT": 3, "RELEASE": "1"},
		Target:     "runtime",
		Platform:   StringList{"linux/amd64"},
		ShmSize:    "128MB",
		Labels:     map[string]any{"release": 1},
	}, buildDependencies(runner, map[string]fs.FileMode{
		"/context":                   fs.ModeDir,
		"/context/Dockerfile.custom": 0,
	}))

	if response.Failed || !response.Changed || response.Image["Id"] != "sha256:new" {
		t.Fatalf("response = %#v", response)
	}
	if response.Stdout != "stdout" || response.Stderr != "build progress" {
		t.Fatalf("stdout/stderr = %q/%q", response.Stdout, response.Stderr)
	}
	command := response.Command
	for _, expected := range [][]string{
		{"--tag", "example:v1"},
		{"--file", "/context/Dockerfile.custom"},
		{"--cache-from", "cache:v1"},
		{"--pull"},
		{"--network", "host"},
		{"--no-cache"},
		{"--add-host", "host.test:host-gateway"},
		{"--build-arg", "COUNT=3"},
		{"--target", "runtime"},
		{"--platform", "linux/amd64"},
		{"--shm-size", "134217728"},
		{"--label", "release=1"},
		{"--", "/context"},
	} {
		if !containsSequence(command, expected) {
			t.Errorf("command %q does not contain %q", command, expected)
		}
	}
}

func TestOutputsAddNamedImageAndValueSecretWithoutExposingValue(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:old"}`)}},
		{result: docker.CLIResult{}},
		{result: docker.CLIResult{Output: []byte(`{"Id":"sha256:new"}`)}},
	}}
	response := ExecuteWithDependencies(Request{
		Name:    "example",
		Path:    "/context",
		Rebuild: "always",
		Secrets: []Secret{{ID: "token", Type: "value", Value: "very-secret"}},
		Outputs: []Output{{Type: "tar", Dest: "/tmp/rootfs.tar"}},
	}, buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}))

	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if containsSequence(response.Command, []string{"--tag"}) {
		t.Fatalf("command unexpectedly contains --tag: %q", response.Command)
	}
	if !containsSequence(response.Command, []string{"--output", "type=tar,dest=/tmp/rootfs.tar"}) ||
		!containsSequence(response.Command, []string{"--output", "type=image,name=example:latest"}) {
		t.Fatalf("outputs missing from command: %q", response.Command)
	}
	if strings.Contains(strings.Join(response.Command, " "), "very-secret") {
		t.Fatalf("secret value leaked in command: %q", response.Command)
	}
	buildInvocation := runner.commands[2]
	if !slices.ContainsFunc(buildInvocation.Env, func(value string) bool {
		return strings.HasPrefix(value, "ANSIBLE_DOCKER_COMPOSE_ENV_SECRET_") && strings.HasSuffix(value, "=very-secret")
	}) {
		t.Fatalf("secret environment missing: %#v", buildInvocation.Env)
	}
}

func TestCheckModeValidatesButDoesNotBuild(t *testing.T) {
	runner := &buildRunner{replies: []cliReply{
		{result: docker.CLIResult{Output: []byte("github.com/docker/buildx v0.13.1")}},
		{result: docker.CLIResult{Output: []byte("No such image")}, err: errors.New("exit 1")},
	}}
	response := ExecuteWithDependenciesAndState(Request{Name: "example", Path: "/context"},
		buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}),
		execution.State{CheckMode: true})

	if response.Failed || !response.Changed || len(runner.commands) != 2 {
		t.Fatalf("response = %#v; commands = %#v", response, runner.commands)
	}
}

func TestBuildxVersionAndNestedValidation(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		version string
		want    string
	}{
		{
			name:    "multiple outputs",
			request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "tar", Dest: "/one"}, {Type: "oci", Dest: "/two"}}},
			version: "v0.12.0",
			want:    "0.13.0 is needed",
		},
		{
			name:    "environment secret",
			request: Request{Name: "example", Path: "/context", Secrets: []Secret{{ID: "secret", Type: "env", Env: "TOKEN"}}},
			version: "v0.5.0",
			want:    "0.6.0 is needed",
		},
		{
			name:    "output destination",
			request: Request{Name: "example", Path: "/context", Outputs: []Output{{Type: "tar"}}},
			version: "v0.13.1",
			want:    "dest is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &buildRunner{replies: []cliReply{{result: docker.CLIResult{Output: []byte(test.version)}}}}
			response := ExecuteWithDependencies(test.request,
				buildDependencies(runner, map[string]fs.FileMode{"/context": fs.ModeDir}))
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestStringListAcceptsScalarAndList(t *testing.T) {
	for input, count := range map[string]int{`"linux/amd64"`: 1, `["linux/amd64","linux/arm64"]`: 2} {
		var values StringList
		if err := values.UnmarshalJSON([]byte(input)); err != nil || len(values) != count {
			t.Fatalf("UnmarshalJSON(%s) = %#v, %v", input, values, err)
		}
	}
}

func containsSequence(values, wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}
	for start := 0; start+len(wanted) <= len(values); start++ {
		if slices.Equal(values[start:start+len(wanted)], wanted) {
			return true
		}
	}
	return false
}
