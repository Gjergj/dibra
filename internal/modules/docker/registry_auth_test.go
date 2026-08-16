package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/registry"
)

type credentialRunner struct {
	commands []CLICommand
	inputs   [][]byte
	results  []CLIResult
	errors   []error
}

func (runner *credentialRunner) Run(_ context.Context, command CLICommand) (CLIResult, error) {
	runner.commands = append(runner.commands, command)
	input, _ := io.ReadAll(command.Stdin)
	runner.inputs = append(runner.inputs, input)
	result := CLIResult{}
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

func TestEncodeRegistryAuthForImage(t *testing.T) {
	encoded, err := EncodeRegistryAuthForImage("registry.example.test:5000/team/app", "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Username != "user" || auth.Password != "pass" || auth.ServerAddress != "registry.example.test:5000" {
		t.Fatalf("decoded auth = %#v", auth)
	}
}

func TestRegistryAuthFromConfig(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("config-user:config-pass"))
	config := []byte(`{"auths":{"https://index.docker.io/v1/":{"auth":"` + encoded + `"}}}`)
	auth, found, err := RegistryAuthFromConfig(config, "alpine:latest")
	if err != nil {
		t.Fatal(err)
	}
	if !found || auth.Username != "config-user" || auth.Password != "config-pass" {
		t.Fatalf("RegistryAuthFromConfig() = %#v, %t", auth, found)
	}
}

func TestRegistryAuthFromConfigIdentityToken(t *testing.T) {
	config := []byte(`{"auths":{"registry.example.test":{"identitytoken":"token"}}}`)
	auth, found, err := RegistryAuthFromConfig(config, "registry.example.test/team/app")
	if err != nil || !found || auth.IdentityToken != "token" {
		t.Fatalf("RegistryAuthFromConfig() = %#v, %t, %v", auth, found, err)
	}
}

func TestResolveRegistryAuthForImageUsesInjectedDockerConfigAndAnonymousFallback(t *testing.T) {
	directory := t.TempDir()
	credentials := base64.StdEncoding.EncodeToString([]byte("config-user:config-pass"))
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(
		`{"auths":{"registry.example.test:5000":{"auth":"`+credentials+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencies := Dependencies{
		Environment: StaticEnvironment{"DOCKER_CONFIG": directory},
		FileSystem:  OSFileSystem{},
	}
	encoded, err := ResolveRegistryAuthForImage("registry.example.test:5000/team/app", dependencies, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Username != "config-user" || auth.Password != "config-pass" {
		t.Fatalf("resolved auth = %#v", auth)
	}

	anonymous, err := ResolveRegistryAuthForImage("other.example.test/team/app", dependencies, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = base64.URLEncoding.DecodeString(anonymous)
	if err != nil || string(raw) != "{}" {
		t.Fatalf("anonymous auth = %q, %v", raw, err)
	}
}

func TestResolveRegistryAuthUsesPerRegistryCredentialHelper(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(
		`{"credsStore":"global","credHelpers":{"registry.example.test:5000":"special"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &credentialRunner{results: []CLIResult{{
		Stdout: []byte(`{"Username":"helper-user","Secret":"helper-pass"}`),
	}}}
	dependencies := Dependencies{
		Environment: StaticEnvironment{"DOCKER_CONFIG": directory, "PATH": "/helpers"},
		FileSystem:  OSFileSystem{},
		CLIRunner:   runner,
	}
	encoded, err := ResolveRegistryAuthForImage("registry.example.test:5000/team/app", dependencies, false)
	if err != nil {
		t.Fatal(err)
	}
	auth := decodeRegistryAuth(t, encoded)
	if auth.Username != "helper-user" || auth.Password != "helper-pass" || auth.ServerAddress != "registry.example.test:5000" {
		t.Fatalf("auth = %#v", auth)
	}
	if len(runner.commands) != 1 || runner.commands[0].Name != "docker-credential-special" ||
		len(runner.commands[0].Args) != 1 || runner.commands[0].Args[0] != "get" ||
		string(runner.inputs[0]) != "registry.example.test:5000" {
		t.Fatalf("helper invocation = %#v input=%q", runner.commands, runner.inputs)
	}
	if len(runner.commands[0].Env) != 2 || !strings.Contains(strings.Join(runner.commands[0].Env, " "), "PATH=/helpers") {
		t.Fatalf("helper environment = %#v", runner.commands[0].Env)
	}
}

func TestResolveRegistryAuthMapsHelperTokenAndDockerHubAddress(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(`{"credsStore":"secretservice"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &credentialRunner{results: []CLIResult{{
		Stdout: []byte(`{"Username":"<token>","Secret":"identity-token"}`),
	}}}
	encoded, err := ResolveRegistryAuthForImage("alpine:latest", Dependencies{
		Environment: StaticEnvironment{"DOCKER_CONFIG": directory},
		FileSystem:  OSFileSystem{},
		CLIRunner:   runner,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	auth := decodeRegistryAuth(t, encoded)
	if auth.IdentityToken != "identity-token" || auth.Username != "" || auth.ServerAddress != dockerHubRegistryURL {
		t.Fatalf("auth = %#v", auth)
	}
	if string(runner.inputs[0]) != dockerHubRegistryURL {
		t.Fatalf("helper input = %q", runner.inputs[0])
	}
}

func TestResolveRegistryAuthFallsBackToInlineWhenHelperEntryIsMissing(t *testing.T) {
	directory := t.TempDir()
	credentials := base64.StdEncoding.EncodeToString([]byte("inline-user:inline-pass"))
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(
		`{"credsStore":"secretservice","auths":{"registry.example.test":{"auth":"`+credentials+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &credentialRunner{
		results: []CLIResult{{ExitCode: 1, Stderr: []byte("credentials not found in native keychain")}},
		errors:  []error{errors.New("exit status 1")},
	}
	encoded, err := ResolveRegistryAuthForImage("registry.example.test/team/app", Dependencies{
		Environment: StaticEnvironment{"DOCKER_CONFIG": directory},
		FileSystem:  OSFileSystem{},
		CLIRunner:   runner,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	auth := decodeRegistryAuth(t, encoded)
	if auth.Username != "inline-user" || auth.Password != "inline-pass" {
		t.Fatalf("auth = %#v", auth)
	}
}

func TestResolveRegistryAuthSurfacesCredentialHelperFailureWithoutOutput(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(`{"credsStore":"broken"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &credentialRunner{
		results: []CLIResult{{ExitCode: 2, Stderr: []byte("secret-value must not escape")}},
		errors:  []error{errors.New("exit status 2")},
	}
	_, err := ResolveRegistryAuthForImage("registry.example.test/team/app", Dependencies{
		Environment: StaticEnvironment{"DOCKER_CONFIG": directory},
		FileSystem:  OSFileSystem{},
		CLIRunner:   runner,
	}, false)
	if err == nil || !strings.Contains(err.Error(), "Credentials store error") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error = %v", err)
	}
}

func TestCredentialHelperStoreAndEraseProtocol(t *testing.T) {
	runner := &credentialRunner{results: []CLIResult{{}, {}}}
	dependencies := Dependencies{Environment: StaticEnvironment{"PATH": "/helpers"}, CLIRunner: runner}
	helper := CredentialHelper{Name: "test", ServerAddress: "registry.example.test"}
	if err := helper.Store(context.Background(), dependencies, "alice", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := helper.Erase(context.Background(), dependencies); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 2 || runner.commands[0].Args[0] != "store" || runner.commands[1].Args[0] != "erase" {
		t.Fatalf("commands = %#v", runner.commands)
	}
	var stored credentialHelperInput
	if err := json.Unmarshal(runner.inputs[0], &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ServerURL != "registry.example.test" || stored.Username != "alice" || stored.Secret != "secret" {
		t.Fatalf("store input = %#v", stored)
	}
	if string(runner.inputs[1]) != "registry.example.test" {
		t.Fatalf("erase input = %q", runner.inputs[1])
	}
}

func TestAllRegistryAuthConfigsCombinesInlineStoreAndPerRegistryHelper(t *testing.T) {
	inline := base64.StdEncoding.EncodeToString([]byte("inline-user:inline-pass"))
	config := []byte(`{
		"auths":{"inline.registry":{"auth":"` + inline + `"}},
		"credsStore":"global",
		"credHelpers":{"override.registry":"special"}
	}`)
	runner := &credentialRunner{results: []CLIResult{
		{Stdout: []byte(`{"global.registry":"global-user","override.registry":"old-user"}`)},
		{Stdout: []byte(`{"Username":"global-user","Secret":"global-pass"}`)},
		{Stdout: []byte(`{"Username":"old-user","Secret":"old-pass"}`)},
		{Stdout: []byte(`{"Username":"special-user","Secret":"special-pass"}`)},
	}}
	configs, err := AllRegistryAuthConfigs(context.Background(), config, Dependencies{
		Environment: StaticEnvironment{"PATH": "/helpers"},
		CLIRunner:   runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configs["inline.registry"].Username != "inline-user" ||
		configs["global.registry"].Password != "global-pass" ||
		configs["override.registry"].Username != "special-user" {
		t.Fatalf("configs = %#v", configs)
	}
	if len(runner.commands) != 4 ||
		runner.commands[0].Name != "docker-credential-global" || runner.commands[0].Args[0] != "list" ||
		runner.commands[3].Name != "docker-credential-special" || runner.commands[3].Args[0] != "get" {
		t.Fatalf("commands = %#v", runner.commands)
	}
}

func decodeRegistryAuth(t *testing.T, encoded string) registry.AuthConfig {
	t.Helper()
	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}
	return auth
}
