package docker_login

import (
	"context"
	"encoding/json"
	"errors"
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

func loginDependencies(fake *loginClient, fileSystem *memoryFS) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  fileSystem,
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
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

func TestLoginFailsWhenCredentialHelperConfigured(t *testing.T) {
	fileSystem := &memoryFS{home: "/home/user", files: map[string][]byte{
		"/tmp/config.json": []byte(`{"credsStore":"osxkeychain"}`),
	}}
	response := ExecuteWithDependencies(Request{Username: "alice", Password: "secret", ConfigPath: "/tmp/config.json"}, loginDependencies(&loginClient{}, fileSystem))
	if !response.Failed || !strings.Contains(response.Msg, "credential helper") {
		t.Fatalf("response = %#v", response)
	}
}

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
