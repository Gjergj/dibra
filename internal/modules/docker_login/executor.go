package docker_login

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/registry"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// Minimal config.json structure
type DockerConfig struct {
	Auths map[string]DockerAuth `json:"auths"`
}

type DockerAuth struct {
	Auth  string `json:"auth"`
	Email string `json:"email,omitempty"`
}

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

	// Read existing config
	cfg := DockerConfig{Auths: make(map[string]DockerAuth)}
	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &cfg)
	}

	if state == "absent" {
		if _, ok := cfg.Auths[reg]; !ok {
			return Response{Changed: false, Msg: "not logged in"}
		}
		delete(cfg.Auths, reg)
		if err := writeConfig(configPath, cfg); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write config: %v", err)}
		}
		return Response{Changed: true, Msg: "logged out"}
	}

	if state == "present" {
		// Calculate auth string
		authStr := base64.StdEncoding.EncodeToString([]byte(req.Username + ":" + req.Password))

		// Check if already logged in with same credentials
		if existing, ok := cfg.Auths[reg]; ok && !req.Relogin {
			if existing.Auth == authStr {
				// Verify strictly?
				return Response{Changed: false, Msg: "already logged in"}
			}
		}

		// Authenticate
		authConfig := registry.AuthConfig{
			Username:      req.Username,
			Password:      req.Password,
			ServerAddress: reg,
			Email:         req.Email,
		}

		resp, err := cli.RegistryLogin(ctx, authConfig)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("login failed: %v", err)}
		}

		// Update config
		cfg.Auths[reg] = DockerAuth{
			Auth:  authStr,
			Email: req.Email,
		}

		if err := writeConfig(configPath, cfg); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to write config: %v", err)}
		}

		return Response{Changed: true, Msg: "login succeeded", Token: resp.IdentityToken}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}

func writeConfig(path string, cfg DockerConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
