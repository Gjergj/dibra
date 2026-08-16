package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"
)

const dockerHubRegistryURL = "https://index.docker.io/v1/"

// EncodeRegistryAuthConfig serializes an Engine RegistryAuth header value.
func EncodeRegistryAuthConfig(auth registry.AuthConfig) (string, error) {
	if auth == (registry.AuthConfig{}) {
		return "", nil
	}
	encoded, err := authconfig.Encode(auth)
	if err != nil {
		return "", fmt.Errorf("encode registry authentication: %w", err)
	}
	return encoded, nil
}

// EncodeRegistryAuth encodes direct username/password credentials for Docker
// Hub. Call EncodeRegistryAuthForImage when the image reference is available.
func EncodeRegistryAuth(username, password string) string {
	encoded, _ := EncodeRegistryAuthConfig(registry.AuthConfig{Username: username, Password: password})
	return encoded
}

// EncodeRegistryAuthForImage includes the resolved registry endpoint in the
// Engine authentication payload.
func EncodeRegistryAuthForImage(image, username, password string) (string, error) {
	if username == "" && password == "" {
		return "", nil
	}
	registryName, err := RegistryName(image)
	if err != nil {
		return "", err
	}
	serverAddress := registryName
	if registryName == "docker.io" {
		serverAddress = dockerHubRegistryURL
	}
	return EncodeRegistryAuthConfig(registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: serverAddress,
	})
}

// RegistryName returns the normalized registry for an image reference.
func RegistryName(image string) (string, error) {
	reference := ParseImageReference(image)
	if err := reference.Validate(); err != nil {
		return "", err
	}
	return reference.Normalize().Registry, nil
}

// ExtractRegistry is the compatibility form of RegistryName.
func ExtractRegistry(image string) string {
	name, err := RegistryName(image)
	if err != nil {
		return "docker.io"
	}
	return name
}

type dockerConfigAuthEntry struct {
	Auth          string `json:"auth"`
	Email         string `json:"email"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	IdentityToken string `json:"identitytoken"`
	RegistryToken string `json:"registrytoken"`
}

type dockerRegistryConfig struct {
	Auths       map[string]dockerConfigAuthEntry `json:"auths"`
	CredsStore  string                           `json:"credsStore"`
	CredHelpers map[string]string                `json:"credHelpers"`
}

// RegistryAuthFromConfig resolves inline auths from a Docker config.json.
func RegistryAuthFromConfig(configJSON []byte, image string) (registry.AuthConfig, bool, error) {
	var config dockerRegistryConfig
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return registry.AuthConfig{}, false, fmt.Errorf("decode Docker registry config: %w", err)
	}
	registryName, err := RegistryName(image)
	if err != nil {
		return registry.AuthConfig{}, false, err
	}

	for configuredRegistry, entry := range config.Auths {
		if normalizeRegistryConfigKey(configuredRegistry) != registryName {
			continue
		}
		result, err := authConfigFromEntry(configuredRegistry, entry)
		if err != nil {
			return registry.AuthConfig{}, false, err
		}
		return result, true, nil
	}
	return registry.AuthConfig{}, false, nil
}

func authConfigFromEntry(configuredRegistry string, entry dockerConfigAuthEntry) (registry.AuthConfig, error) {
	result := registry.AuthConfig{
		Username:      entry.Username,
		Password:      entry.Password,
		IdentityToken: entry.IdentityToken,
		RegistryToken: entry.RegistryToken,
		ServerAddress: configuredRegistry,
	}
	if result.IdentityToken == "" && result.Username == "" && result.Password == "" && entry.Auth != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(entry.Auth)
		if decodeErr != nil {
			return registry.AuthConfig{}, fmt.Errorf("decode authentication for registry %s: %w", configuredRegistry, decodeErr)
		}
		username, password, found := strings.Cut(string(decoded), ":")
		if !found {
			return registry.AuthConfig{}, fmt.Errorf("authentication for registry %s does not contain username and password", configuredRegistry)
		}
		result.Username = username
		result.Password = password
	}
	return result, nil
}

// CredentialHelper describes the external Docker credential helper selected
// for one registry.
type CredentialHelper struct {
	Name          string
	ServerAddress string
}

type credentialHelperOutput struct {
	Username string `json:"Username"`
	Secret   string `json:"Secret"`
}

type credentialHelperInput struct {
	ServerURL string `json:"ServerURL"`
	Username  string `json:"Username"`
	Secret    string `json:"Secret"`
}

// CredentialHelperFromConfig applies Docker's per-registry helper precedence
// over the global credential store.
func CredentialHelperFromConfig(configJSON []byte, registryName string) (CredentialHelper, bool, error) {
	var config dockerRegistryConfig
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return CredentialHelper{}, false, fmt.Errorf("decode Docker registry config: %w", err)
	}
	normalized := normalizeRegistryConfigKey(registryName)
	for configuredRegistry, helperName := range config.CredHelpers {
		if normalizeRegistryConfigKey(configuredRegistry) == normalized && helperName != "" {
			return CredentialHelper{Name: helperName, ServerAddress: credentialHelperServerAddress(normalized)}, true, nil
		}
	}
	if config.CredsStore != "" {
		return CredentialHelper{Name: config.CredsStore, ServerAddress: credentialHelperServerAddress(normalized)}, true, nil
	}
	return CredentialHelper{}, false, nil
}

// Get resolves credentials through docker-credential-<name>. Missing helper
// entries are reported as found=false so callers can fall back to inline auths.
func (helper CredentialHelper) Get(ctx context.Context, dependencies Dependencies) (registry.AuthConfig, bool, error) {
	result, err := helper.run(ctx, dependencies, "get", []byte(helper.ServerAddress))
	if err != nil {
		if helperCredentialsNotFound(result) {
			return registry.AuthConfig{}, false, nil
		}
		return registry.AuthConfig{}, false, helperCommandError(helper, "get", result)
	}
	var output credentialHelperOutput
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		return registry.AuthConfig{}, false, fmt.Errorf("Credentials store error: decode docker-credential-%s get response: %w", helper.Name, err)
	}
	if output.Username == "" && output.Secret == "" {
		return registry.AuthConfig{}, false, nil
	}
	auth := registry.AuthConfig{ServerAddress: helper.ServerAddress}
	if output.Username == "<token>" {
		auth.IdentityToken = output.Secret
	} else {
		auth.Username = output.Username
		auth.Password = output.Secret
	}
	return auth, true, nil
}

// Store persists credentials through docker-credential-<name>.
func (helper CredentialHelper) Store(ctx context.Context, dependencies Dependencies, username, secret string) error {
	input, err := json.Marshal(credentialHelperInput{
		ServerURL: helper.ServerAddress,
		Username:  username,
		Secret:    secret,
	})
	if err != nil {
		return fmt.Errorf("encode credential helper request: %w", err)
	}
	result, runErr := helper.run(ctx, dependencies, "store", input)
	if runErr != nil {
		return helperCommandError(helper, "store", result)
	}
	return nil
}

// Erase removes credentials through docker-credential-<name>.
func (helper CredentialHelper) Erase(ctx context.Context, dependencies Dependencies) error {
	result, err := helper.run(ctx, dependencies, "erase", []byte(helper.ServerAddress))
	if err != nil {
		if helperCredentialsNotFound(result) {
			return nil
		}
		return helperCommandError(helper, "erase", result)
	}
	return nil
}

// List returns every server known to a credential store.
func (helper CredentialHelper) List(ctx context.Context, dependencies Dependencies) ([]string, error) {
	result, err := helper.run(ctx, dependencies, "list", nil)
	if err != nil {
		return nil, helperCommandError(helper, "list", result)
	}
	entries := map[string]string{}
	if err := json.Unmarshal(result.Stdout, &entries); err != nil {
		return nil, fmt.Errorf("Credentials store error: decode docker-credential-%s list response: %w", helper.Name, err)
	}
	servers := make([]string, 0, len(entries))
	for server := range entries {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	return servers, nil
}

func (helper CredentialHelper) run(ctx context.Context, dependencies Dependencies, operation string, input []byte) (CLIResult, error) {
	dependencies = dependencies.Resolve()
	return dependencies.CLIRunner.Run(ctx, CLICommand{
		Name:  "docker-credential-" + helper.Name,
		Args:  []string{operation},
		Env:   dependencies.Environment.Environ(),
		Stdin: bytes.NewReader(input),
	})
}

func helperCredentialsNotFound(result CLIResult) bool {
	message := strings.ToLower(string(result.Stderr))
	return strings.Contains(message, "credentials not found") ||
		strings.Contains(message, "no credentials") ||
		strings.Contains(message, "not found in native keychain")
}

func helperCommandError(helper CredentialHelper, operation string, result CLIResult) error {
	return fmt.Errorf("Credentials store error: docker-credential-%s %s failed with exit code %d", helper.Name, operation, result.ExitCode)
}

func credentialHelperServerAddress(registryName string) string {
	if normalizeRegistryConfigKey(registryName) == "docker.io" {
		return dockerHubRegistryURL
	}
	return strings.TrimSuffix(registryName, "/")
}

// RegistryAuthFromConfigWithDependencies resolves helper-backed credentials
// first and falls back to inline auths when the helper has no matching entry.
func RegistryAuthFromConfigWithDependencies(ctx context.Context, configJSON []byte, image string, dependencies Dependencies) (registry.AuthConfig, bool, error) {
	registryName, err := RegistryName(image)
	if err != nil {
		return registry.AuthConfig{}, false, err
	}
	helper, configured, err := CredentialHelperFromConfig(configJSON, registryName)
	if err != nil {
		return registry.AuthConfig{}, false, err
	}
	if configured {
		auth, found, err := helper.Get(ctx, dependencies)
		if err != nil {
			return registry.AuthConfig{}, false, err
		}
		if found {
			return auth, true, nil
		}
	}
	return RegistryAuthFromConfig(configJSON, image)
}

// AllRegistryAuthConfigs resolves inline entries plus all configured helper
// credentials for Engine build authentication.
func AllRegistryAuthConfigs(ctx context.Context, configJSON []byte, dependencies Dependencies) (map[string]registry.AuthConfig, error) {
	var config dockerRegistryConfig
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return nil, fmt.Errorf("decode Docker registry config: %w", err)
	}
	result := make(map[string]registry.AuthConfig, len(config.Auths))
	for server, entry := range config.Auths {
		auth, err := authConfigFromEntry(server, entry)
		if err != nil {
			return nil, err
		}
		result[server] = auth
	}
	if config.CredsStore != "" {
		store := CredentialHelper{Name: config.CredsStore}
		servers, err := store.List(ctx, dependencies)
		if err != nil {
			return nil, err
		}
		for _, server := range servers {
			store.ServerAddress = server
			auth, found, err := store.Get(ctx, dependencies)
			if err != nil {
				return nil, err
			}
			if found {
				result[server] = auth
			}
		}
	}
	for configuredRegistry, helperName := range config.CredHelpers {
		if helperName == "" {
			continue
		}
		helper := CredentialHelper{Name: helperName, ServerAddress: credentialHelperServerAddress(configuredRegistry)}
		auth, found, err := helper.Get(ctx, dependencies)
		if err != nil {
			return nil, err
		}
		if found {
			result[helper.ServerAddress] = auth
		}
	}
	return result, nil
}

// ResolveRegistryAuthForImage reads Docker's config.json through injected
// dependencies and returns the Engine RegistryAuth value for an image.
func ResolveRegistryAuthForImage(image string, dependencies Dependencies, requireHeader bool) (string, error) {
	return ResolveRegistryAuthForImageContext(context.Background(), image, dependencies, requireHeader)
}

// ResolveRegistryAuthForImageContext is the context-aware form used by
// executors that already own a request context.
func ResolveRegistryAuthForImageContext(ctx context.Context, image string, dependencies Dependencies, requireHeader bool) (string, error) {
	dependencies = dependencies.Resolve()
	var directory string
	if configured, found := dependencies.Environment.LookupEnv("DOCKER_CONFIG"); found && configured != "" {
		directory = configured
	} else {
		home, err := dependencies.FileSystem.UserHomeDir()
		if err == nil {
			directory = filepath.Join(home, ".docker")
		}
	}
	if directory != "" {
		configJSON, err := dependencies.FileSystem.ReadFile(filepath.Join(directory, "config.json"))
		if err == nil {
			auth, found, err := RegistryAuthFromConfigWithDependencies(ctx, configJSON, image, dependencies)
			if err != nil {
				return "", err
			}
			if found {
				return EncodeRegistryAuthConfig(auth)
			}
		}
	}
	if requireHeader {
		return base64.URLEncoding.EncodeToString([]byte("{}")), nil
	}
	return "", nil
}

func normalizeRegistryConfigKey(value string) string {
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	value = strings.SplitN(value, "/", 2)[0]
	if value == "index.docker.io" || value == "registry.hub.docker.com" {
		return "docker.io"
	}
	return value
}
