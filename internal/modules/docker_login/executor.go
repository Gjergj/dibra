package docker_login

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

var configFileMutex sync.Mutex

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()

	stateName := req.State
	if stateName == "" {
		stateName = "present"
	}
	if stateName != "present" && stateName != "absent" {
		return failedResponse(fmt.Sprintf("state must be present or absent, got %q", stateName))
	}

	registryURL := req.registryURL()
	configPath, err := expandUserPath(dependencies.FileSystem, req.ConfigPath)
	if err != nil {
		return failedResponse(fmt.Sprintf("could not find home dir: %v", err))
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to create docker client: %v", err))
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	configFileMutex.Lock()
	defer configFileMutex.Unlock()

	cfg, err := readConfig(dependencies.FileSystem, configPath)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to read config: %v", err))
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to encode config: %v", err))
	}
	helper, helperConfigured, err := docker.CredentialHelperFromConfig(configJSON, registryURL)
	if err != nil {
		return failedResponse(err.Error())
	}

	if stateName == "absent" {
		return logout(ctx, dependencies, cfg, configPath, registryURL, state.CheckMode, helper, helperConfigured)
	}
	return login(ctx, req, dependencies, apiClient, cfg, configPath, registryURL, state.CheckMode, helper, helperConfigured)
}

func login(ctx context.Context, req Request, dependencies docker.Dependencies, apiClient client.APIClient, cfg map[string]any, configPath, registryURL string, checkMode bool, helper docker.CredentialHelper, helperConfigured bool) Response {
	if req.Username == "" || req.Password == "" {
		return failedResponse("username and password are required when state=present")
	}

	auth, err := apiClient.RegistryLogin(ctx, client.RegistryLoginOptions{
		Username:      req.Username,
		Password:      req.Password,
		ServerAddress: registryURL,
	})
	if err != nil {
		return failedResponse(fmt.Sprintf("Logging into %s for user %s failed - %v", registryURL, req.Username, err))
	}

	result, err := loginResult(auth, req.Username, registryURL)
	if err != nil {
		return failedResponse(fmt.Sprintf("encode login result: %v", err))
	}

	if helperConfigured {
		current, found, err := helper.Get(ctx, dependencies)
		if err != nil {
			return failedResponse(err.Error())
		}
		if found && current.Username == req.Username && current.Password == req.Password && !req.reauthorize() {
			return Response{LoginResult: result, Msg: "already logged in"}
		}
		if !checkMode {
			if err := helper.Store(ctx, dependencies, req.Username, req.Password); err != nil {
				return failedResponse(err.Error())
			}
		}
		return Response{Changed: true, LoginResult: result, Msg: "login succeeded"}
	}

	auths := authsMap(cfg)
	if matchingCredentials(auths, registryURL, req.Username, req.Password) && !req.reauthorize() {
		return Response{LoginResult: result, Msg: "already logged in"}
	}

	if !checkMode {
		auths[registryURL] = map[string]any{"auth": encodedAuth(req.Username, req.Password)}
		cfg["auths"] = auths
		if err := writeConfig(dependencies.FileSystem, configPath, cfg); err != nil {
			return failedResponse(fmt.Sprintf("failed to write config: %v", err))
		}
	}
	return Response{Changed: true, LoginResult: result, Msg: "login succeeded"}
}

func logout(ctx context.Context, dependencies docker.Dependencies, cfg map[string]any, configPath, registryURL string, checkMode bool, helper docker.CredentialHelper, helperConfigured bool) Response {
	if helperConfigured {
		_, found, err := helper.Get(ctx, dependencies)
		if err != nil {
			return failedResponse(err.Error())
		}
		if !found {
			return Response{Msg: "not logged in"}
		}
		if !checkMode {
			if err := helper.Erase(ctx, dependencies); err != nil {
				return failedResponse(err.Error())
			}
		}
		return Response{Changed: true, Msg: "logged out"}
	}

	auths := authsMap(cfg)
	if !hasCredentials(auths, registryURL) {
		return Response{Msg: "not logged in"}
	}
	if !checkMode {
		delete(auths, registryURL)
		cfg["auths"] = auths
		if err := writeConfig(dependencies.FileSystem, configPath, cfg); err != nil {
			return failedResponse(fmt.Sprintf("failed to write config: %v", err))
		}
	}
	return Response{Changed: true, Msg: "logged out"}
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}

func expandUserPath(fileSystem docker.FileSystem, path string) (string, error) {
	if path == "" {
		path = defaultConfigPath
	}
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := fileSystem.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func readConfig(fileSystem docker.FileSystem, path string) (map[string]any, error) {
	data, err := fileSystem.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{"auths": map[string]any{}}, nil
		}
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	if _, found := cfg["auths"]; !found {
		cfg["auths"] = map[string]any{}
	}
	return cfg, nil
}

func writeConfig(fileSystem docker.FileSystem, path string, cfg map[string]any) error {
	if err := fileSystem.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	output, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return err
	}
	output = append(output, '\n')
	return fileSystem.WriteFile(path, output, 0o600)
}

func authsMap(cfg map[string]any) map[string]any {
	raw, ok := cfg["auths"]
	if !ok || raw == nil {
		auths := map[string]any{}
		cfg["auths"] = auths
		return auths
	}
	auths, ok := raw.(map[string]any)
	if !ok {
		auths = map[string]any{}
		cfg["auths"] = auths
		return auths
	}
	return auths
}

func hasCredentials(auths map[string]any, registryURL string) bool {
	entry, found := auths[registryURL]
	if !found || entry == nil {
		return false
	}
	object, ok := entry.(map[string]any)
	if !ok {
		return true
	}
	return len(object) > 0
}

func encodedAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func decodedCredentials(entry any) (username, password string, ok bool) {
	object, isObject := entry.(map[string]any)
	if !isObject {
		return "", "", false
	}
	if auth, _ := object["auth"].(string); auth != "" {
		decoded, err := base64.StdEncoding.DecodeString(auth)
		if err != nil {
			return "", "", false
		}
		user, secret, found := strings.Cut(string(decoded), ":")
		if !found {
			return "", "", false
		}
		return user, secret, true
	}
	username, _ = object["username"].(string)
	password, _ = object["password"].(string)
	if username == "" && password == "" {
		return "", "", false
	}
	return username, password, true
}

func matchingCredentials(auths map[string]any, registryURL, username, password string) bool {
	entry, found := auths[registryURL]
	if !found {
		return false
	}
	existingUser, existingPassword, ok := decodedCredentials(entry)
	return ok && existingUser == username && existingPassword == password
}

func loginResult(auth client.RegistryLoginResult, username, registryURL string) (map[string]any, error) {
	result, err := docker.InspectionMap(auth.Auth)
	if err != nil {
		return nil, err
	}
	result["username"] = username
	result["serveraddress"] = registryURL
	delete(result, "password")
	delete(result, "Password")
	return result, nil
}
