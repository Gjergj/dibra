package docker_login

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

type memoryFS struct {
	docker.FileSystem
	home  string
	files map[string][]byte
	modes map[string]fs.FileMode
}

func (fileSystem *memoryFS) UserHomeDir() (string, error) { return fileSystem.home, nil }
func (fileSystem *memoryFS) ReadFile(path string) ([]byte, error) {
	data, found := fileSystem.files[path]
	if !found {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}
func (fileSystem *memoryFS) WriteFile(path string, data []byte, mode fs.FileMode) error {
	if fileSystem.files == nil {
		fileSystem.files = map[string][]byte{}
	}
	if fileSystem.modes == nil {
		fileSystem.modes = map[string]fs.FileMode{}
	}
	fileSystem.files[path] = append([]byte(nil), data...)
	fileSystem.modes[path] = mode
	return nil
}
func (*memoryFS) MkdirAll(string, fs.FileMode) error { return nil }

type loginClient struct {
	client.APIClient
	result client.RegistryLoginResult
	err    error
	calls  int
}

func (fake *loginClient) RegistryLogin(context.Context, client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
	fake.calls++
	return fake.result, fake.err
}
func (*loginClient) Close() error { return nil }

type loginHelperRunner struct {
	commands []docker.CLICommand
	inputs   [][]byte
	results  []docker.CLIResult
	errors   []error
}

func (runner *loginHelperRunner) Run(_ context.Context, command docker.CLICommand) (docker.CLIResult, error) {
	runner.commands = append(runner.commands, command)
	input, _ := io.ReadAll(command.Stdin)
	runner.inputs = append(runner.inputs, input)
	result := docker.CLIResult{}
	if len(runner.results) > 0 {
		result = runner.results[0]
		runner.results = runner.results[1:]
	}
	var err error
	if len(runner.errors) > 0 {
		err = runner.errors[0]
		runner.errors = runner.errors[1:]
	}
	return result, err
}

func loginDependencies(fake *loginClient, fileSystem *memoryFS) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  fileSystem,
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func helperLoginDependencies(fake *loginClient, fileSystem *memoryFS, runner docker.CLIRunner) docker.Dependencies {
	dependencies := loginDependencies(fake, fileSystem)
	dependencies.CLIRunner = runner
	return dependencies
}

func TestLoginWritesConfigAndIsIdempotent(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{}}
	fake := &loginClient{result: client.RegistryLoginResult{Auth: registry.AuthResponse{Status: "Login Succeeded"}}}
	request := Request{Username: "alice", Password: "secret", ConfigPath: "/tmp/config.json"}
	created := ExecuteWithDependencies(request, loginDependencies(fake, fileSystem))
	if created.Failed || !created.Changed || fake.calls != 1 {
		t.Fatalf("created = %#v calls=%d", created, fake.calls)
	}
	if created.LoginResult["username"] != "alice" || fileSystem.modes["/tmp/config.json"] != 0o600 {
		t.Fatalf("login result = %#v mode=%#v", created.LoginResult, fileSystem.modes)
	}

	again := ExecuteWithDependencies(request, loginDependencies(fake, fileSystem))
	if again.Failed || again.Changed {
		t.Fatalf("idempotent = %#v", again)
	}

	reauth := request
	reauth.Reauthorize = true
	forced := ExecuteWithDependencies(reauth, loginDependencies(fake, fileSystem))
	if forced.Failed || !forced.Changed {
		t.Fatalf("reauthorize = %#v", forced)
	}
}

func TestLoginCheckModeAuthenticatesWithoutWriting(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{}}
	fake := &loginClient{result: client.RegistryLoginResult{Auth: registry.AuthResponse{Status: "Login Succeeded"}}}
	response := ExecuteWithDependenciesAndState(Request{Username: "alice", Password: "secret", ConfigPath: "/tmp/config.json"}, loginDependencies(fake, fileSystem), execution.State{CheckMode: true})
	if response.Failed || !response.Changed || fake.calls != 1 || len(fileSystem.files) != 0 {
		t.Fatalf("response = %#v files=%#v", response, fileSystem.files)
	}
}

func TestLoginFailsOnWrongPassword(t *testing.T) {
	fake := &loginClient{err: errors.New("unauthorized")}
	response := ExecuteWithDependencies(Request{Username: "alice", Password: "bad", ConfigPath: "/tmp/config.json"}, loginDependencies(fake, &memoryFS{home: "/home/user", files: map[string][]byte{}}))
	if !response.Failed || !strings.Contains(response.Msg, "Logging into") {
		t.Fatalf("response = %#v", response)
	}
}

func TestLoginStoresAndReusesCredentialHelperCredentials(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{
		"/tmp/config.json": []byte(`{"credsStore":"test"}`),
	}}
	fake := &loginClient{result: client.RegistryLoginResult{Auth: registry.AuthResponse{Status: "Login Succeeded"}}}
	runner := &loginHelperRunner{
		results: []docker.CLIResult{
			{ExitCode: 1, Stderr: []byte("credentials not found")},
			{},
		},
		errors: []error{errors.New("exit status 1"), nil},
	}
	request := Request{Username: "alice", Password: "secret", ConfigPath: "/tmp/config.json"}
	response := ExecuteWithDependencies(request, helperLoginDependencies(fake, fileSystem, runner))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if len(runner.commands) != 2 || runner.commands[0].Args[0] != "get" || runner.commands[1].Args[0] != "store" {
		t.Fatalf("commands = %#v", runner.commands)
	}
	var stored map[string]string
	if err := json.Unmarshal(runner.inputs[1], &stored); err != nil {
		t.Fatal(err)
	}
	if stored["ServerURL"] != dockerHubRegistryURLForTest || stored["Username"] != "alice" || stored["Secret"] != "secret" {
		t.Fatalf("store input = %#v", stored)
	}
	if string(fileSystem.files["/tmp/config.json"]) != `{"credsStore":"test"}` {
		t.Fatalf("helper login rewrote config = %s", fileSystem.files["/tmp/config.json"])
	}

	reuseRunner := &loginHelperRunner{results: []docker.CLIResult{{
		Stdout: []byte(`{"Username":"alice","Secret":"secret"}`),
	}}}
	again := ExecuteWithDependencies(request, helperLoginDependencies(fake, fileSystem, reuseRunner))
	if again.Failed || again.Changed || len(reuseRunner.commands) != 1 || reuseRunner.commands[0].Args[0] != "get" {
		t.Fatalf("again = %#v commands=%#v", again, reuseRunner.commands)
	}
}

func TestLogoutErasesCredentialHelperCredentials(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{
		"/tmp/config.json": []byte(`{"credHelpers":{"registry.example.test":"special"}}`),
	}}
	runner := &loginHelperRunner{results: []docker.CLIResult{
		{Stdout: []byte(`{"Username":"alice","Secret":"secret"}`)},
		{},
	}}
	request := Request{State: "absent", RegistryURL: "registry.example.test", ConfigPath: "/tmp/config.json"}
	response := ExecuteWithDependencies(request, helperLoginDependencies(&loginClient{}, fileSystem, runner))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if len(runner.commands) != 2 || runner.commands[0].Name != "docker-credential-special" ||
		runner.commands[0].Args[0] != "get" || runner.commands[1].Args[0] != "erase" ||
		string(runner.inputs[1]) != "registry.example.test" {
		t.Fatalf("commands = %#v inputs=%q", runner.commands, runner.inputs)
	}
}

const dockerHubRegistryURLForTest = "https://index.docker.io/v1/"

func TestLogoutIsIdempotent(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{
		"/tmp/config.json": []byte(`{"auths":{"https://index.docker.io/v1/":{"auth":"YWxpY2U6c2VjcmV0"}}}`),
	}}
	first := ExecuteWithDependencies(Request{State: "absent", ConfigPath: "/tmp/config.json"}, loginDependencies(&loginClient{}, fileSystem))
	if first.Failed || !first.Changed {
		t.Fatalf("first logout = %#v", first)
	}
	var cfg map[string]any
	if err := json.Unmarshal(fileSystem.files["/tmp/config.json"], &cfg); err != nil {
		t.Fatal(err)
	}
	auths, _ := cfg["auths"].(map[string]any)
	if len(auths) != 0 {
		t.Fatalf("auths after logout = %#v", auths)
	}
	second := ExecuteWithDependencies(Request{State: "absent", ConfigPath: "/tmp/config.json"}, loginDependencies(&loginClient{}, fileSystem))
	if second.Failed || second.Changed {
		t.Fatalf("second logout = %#v", second)
	}
}

func TestLoginExpandsHomeConfigPath(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{}}
	fake := &loginClient{result: client.RegistryLoginResult{Auth: registry.AuthResponse{Status: "Login Succeeded"}}}
	response := ExecuteWithDependencies(Request{Username: "alice", Password: "secret"}, loginDependencies(fake, fileSystem))
	if response.Failed || fileSystem.files["/home/user/.docker/config.json"] == nil {
		t.Fatalf("response = %#v files=%#v", response, fileSystem.files)
	}
}
