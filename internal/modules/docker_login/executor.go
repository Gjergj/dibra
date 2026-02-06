package docker_login

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/docker/api/types/registry"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

// DockerConfig represents the full docker config.json structure
type DockerConfig struct {
	Auths       map[string]DockerAuth `json:"auths,omitempty"`
	CredsStore  string                `json:"credsStore,omitempty"`
	CredHelpers map[string]string     `json:"credHelpers,omitempty"`
	// Preserve other fields
	HttpHeaders       map[string]string          `json:"HttpHeaders,omitempty"`
	Psformat          string                     `json:"psFormat,omitempty"`
	ImagesFormat      string                     `json:"imagesFormat,omitempty"`
	NetworksFormat    string                     `json:"networksFormat,omitempty"`
	VolumesFormat     string                     `json:"volumesFormat,omitempty"`
	StatsFormat       string                     `json:"statsFormat,omitempty"`
	Detachkeys        string                     `json:"detachKeys,omitempty"`
	CredentialsStore  string                     `json:"credentialsStore,omitempty"`
	Stackorchestrator string                     `json:"stackOrchestrator,omitempty"`
	CurrentContext    string                     `json:"currentContext,omitempty"`
	Plugins           json.RawMessage            `json:"plugins,omitempty"`
	Aliases           json.RawMessage            `json:"aliases,omitempty"`
	ExtraFields       map[string]json.RawMessage `json:"-"`
}

type DockerAuth struct {
	Auth          string `json:"auth,omitempty"`
	Email         string `json:"email,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	ServerAddress string `json:"serveraddress,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
	RegistryToken string `json:"registrytoken,omitempty"`
}

// configFileMutex prevents concurrent writes to config.json
var configFileMutex sync.Mutex

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	state := req.State
	if state == "" {
		state = "present"
	}

	reg := req.Registry
	if reg == "" {
		reg = "https://index.docker.io/v1/"
	}

	configPath := req.ConfigPath
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("could not find home dir: %v", err)}
		}
		configPath = filepath.Join(home, ".docker", "config.json")
	}

	// Acquire lock for file operations
	configFileMutex.Lock()
	defer configFileMutex.Unlock()

	// Read existing config (preserve structure)
	cfg, rawData, err := readConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read config: %v", err)}
	}
	if cfg.Auths == nil {
		cfg.Auths = make(map[string]DockerAuth)
	}

	// Check for credential helpers
	if cfg.CredsStore != "" || (cfg.CredHelpers != nil && cfg.CredHelpers[reg] != "") {
		helper := cfg.CredsStore
		if cfg.CredHelpers != nil && cfg.CredHelpers[reg] != "" {
			helper = cfg.CredHelpers[reg]
		}
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("credential helper '%s' is configured for this registry; docker_login module cannot manage credentials stored in external helpers - use docker login CLI or manage the helper directly", helper),
		}
	}

	if state == "absent" {
		if _, ok := cfg.Auths[reg]; !ok {
			return Response{Changed: false, Msg: "not logged in", Registry: reg}
		}
		delete(cfg.Auths, reg)
		if err := writeConfig(configPath, cfg, rawData); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write config: %v", err)}
		}
		return Response{Changed: true, Msg: "logged out", Registry: reg}
	}

	if state == "present" {
		// Calculate auth string
		authStr := base64.StdEncoding.EncodeToString([]byte(req.Username + ":" + req.Password))

		// Check if already logged in with same credentials
		if existing, ok := cfg.Auths[reg]; ok && !req.Relogin {
			if existing.Auth == authStr {
				return Response{Changed: false, Msg: "already logged in", Registry: reg}
			}
		}

		// Validate credentials via registry login (default: true)
		validate := true
		if req.Validate == false && req.State != "" {
			// Only skip validation if explicitly set to false
			// Note: Go unmarshals missing bool as false, so we check if State was provided
			// to distinguish between explicit false and default
			validate = req.Validate
		}

		if validate {
			authConfig := registry.AuthConfig{
				Username:      req.Username,
				Password:      req.Password,
				ServerAddress: reg,
				Email:         req.Email,
			}

			resp, err := cli.RegistryLogin(ctx, authConfig)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("login failed: %v", err), Registry: reg}
			}

			// Update config
			cfg.Auths[reg] = DockerAuth{
				Auth:  authStr,
				Email: req.Email,
			}

			if err := writeConfig(configPath, cfg, rawData); err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to write config: %v", err)}
			}

			return Response{Changed: true, Msg: "login succeeded", Token: resp.IdentityToken, Registry: reg}
		}

		// Skip validation, just store credentials
		cfg.Auths[reg] = DockerAuth{
			Auth:  authStr,
			Email: req.Email,
		}

		if err := writeConfig(configPath, cfg, rawData); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write config: %v", err)}
		}

		return Response{Changed: true, Msg: "credentials stored (validation skipped)", Registry: reg}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}

// readConfig reads and parses the docker config.json, also returning raw data
// for preserving unknown fields
func readConfig(path string) (DockerConfig, []byte, error) {
	cfg := DockerConfig{Auths: make(map[string]DockerAuth)}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, data, err
	}
	return cfg, data, nil
}

// writeConfig writes the docker config.json, preserving existing structure
func writeConfig(path string, cfg DockerConfig, rawData []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// If we have raw data, try to preserve unknown fields
	var output []byte
	var err error
	if rawData != nil {
		// Parse raw data, update only what we changed
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(rawData, &rawMap); err == nil {
			// Update auths
			authsData, _ := json.Marshal(cfg.Auths)
			rawMap["auths"] = authsData

			output, err = json.MarshalIndent(rawMap, "", "\t")
			if err != nil {
				return err
			}
		} else {
			// Fall back to normal marshal
			output, err = json.MarshalIndent(cfg, "", "\t")
			if err != nil {
				return err
			}
		}
	} else {
		output, err = json.MarshalIndent(cfg, "", "\t")
		if err != nil {
			return err
		}
	}

	// Write with proper permissions (600 for config files)
	return os.WriteFile(path, output, 0600)
}
