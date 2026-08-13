package docker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
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
	Auths map[string]dockerConfigAuthEntry `json:"auths"`
}

// RegistryAuthFromConfig resolves inline auths from a Docker config.json.
// External credential helpers remain the CLI's responsibility.
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
				return registry.AuthConfig{}, false, fmt.Errorf("decode authentication for registry %s: %w", configuredRegistry, decodeErr)
			}
			username, password, found := strings.Cut(string(decoded), ":")
			if !found {
				return registry.AuthConfig{}, false, fmt.Errorf("authentication for registry %s does not contain username and password", configuredRegistry)
			}
			result.Username = username
			result.Password = password
		}
		return result, true, nil
	}
	return registry.AuthConfig{}, false, nil
}

// ResolveRegistryAuthForImage reads Docker's config.json through injected
// dependencies and returns the Engine RegistryAuth value for an image.
// Credential helpers are intentionally left to Docker CLI-backed modules.
func ResolveRegistryAuthForImage(image string, dependencies Dependencies, requireHeader bool) (string, error) {
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
			auth, found, err := RegistryAuthFromConfig(configJSON, image)
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
