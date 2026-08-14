package docker_context_info

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

const (
	defaultUnixSocket  = "unix:///var/run/docker.sock"
	defaultDescription = "Current DOCKER_HOST based configuration"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if req.OnlyCurrent && req.Name != "" {
		return failedResponse("only_current and name are mutually exclusive")
	}

	configDir, err := dockerConfigDir(dependencies)
	if err != nil {
		return failedResponse(err.Error())
	}

	currentName, currentSource := currentContextName(req, dependencies, configDir)
	contextsByName, err := loadContexts(dependencies, configDir)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error when handling Docker contexts: %v", err))
	}

	var selected []dockerContext
	switch {
	case req.Name != "":
		context, found := contextsByName[req.Name]
		if !found {
			return failedResponse(fmt.Sprintf("There is no context of name %q", req.Name))
		}
		selected = []dockerContext{context}
	case req.OnlyCurrent:
		context, found := contextsByName[currentName]
		if !found {
			return failedResponse(fmt.Sprintf("There is no context of name %q, which is configured as the default context (%s)", currentName, currentSource))
		}
		selected = []dockerContext{context}
	default:
		selected = make([]dockerContext, 0, len(contextsByName))
		for _, context := range contextsByName {
			selected = append(selected, context)
		}
	}

	infos := make([]ContextInfo, 0, len(selected))
	for _, context := range selected {
		infos = append(infos, context.toInfo(currentName, dependencies.FileSystem))
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return Response{Contexts: infos, CurrentContextName: currentName}
}

type dockerContext struct {
	Name        string
	Description string
	MetaPath    string
	TLSPath     string
	Host        string
	SkipTLS     bool
	InMemory    bool
}

func (context dockerContext) toInfo(currentName string, fileSystem docker.FileSystem) ContextInfo {
	info := ContextInfo{
		Current:     context.Name == currentName,
		Name:        context.Name,
		Description: context.Description,
		Config:      map[string]any{},
	}
	if !context.InMemory {
		meta := context.MetaPath
		tls := context.TLSPath
		info.MetaPath = &meta
		info.TLSPath = &tls
	}
	if context.Host != "" {
		host := normalizeDockerHost(context.Host)
		info.Config["docker_host"] = host
		caPath := filepath.Join(context.TLSPath, "docker", "ca.pem")
		certPath := filepath.Join(context.TLSPath, "docker", "cert.pem")
		keyPath := filepath.Join(context.TLSPath, "docker", "key.pem")
		hasCA := fileExists(fileSystem, caPath)
		hasCert := fileExists(fileSystem, certPath)
		hasKey := fileExists(fileSystem, keyPath)
		if hasCA || hasCert || hasKey {
			if hasCA {
				info.Config["ca_path"] = caPath
			}
			if hasCert {
				info.Config["client_cert"] = certPath
			}
			if hasKey {
				info.Config["client_key"] = keyPath
			}
			info.Config["validate_certs"] = !context.SkipTLS
			info.Config["tls"] = true
		} else {
			info.Config["tls"] = context.SkipTLS
		}
	}
	return info
}

func currentContextName(req Request, dependencies docker.Dependencies, configDir string) (string, string) {
	if req.CLIContext != "" {
		return req.CLIContext, "cli_context module option"
	}
	if value, found := dependencies.Environment.LookupEnv("DOCKER_HOST"); found && value != "" {
		return "default", "DOCKER_HOST environment variable set"
	}
	if value, found := dependencies.Environment.LookupEnv("DOCKER_CONTEXT"); found && value != "" {
		return value, "DOCKER_CONTEXT environment variable set"
	}
	configPath := filepath.Join(configDir, "config.json")
	data, err := dependencies.FileSystem.ReadFile(configPath)
	if err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			if name, _ := cfg["currentContext"].(string); name != "" {
				return name, "configuration file " + configPath
			}
		}
	}
	return "default", "fallback value"
}

func dockerConfigDir(dependencies docker.Dependencies) (string, error) {
	if value, found := dependencies.Environment.LookupEnv("DOCKER_CONFIG"); found && value != "" {
		return value, nil
	}
	home, err := dependencies.FileSystem.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home dir: %v", err)
	}
	return filepath.Join(home, ".docker"), nil
}

func loadContexts(dependencies docker.Dependencies, configDir string) (map[string]dockerContext, error) {
	contexts := map[string]dockerContext{
		"default": defaultContext(dependencies),
	}
	metaRoot := filepath.Join(configDir, "contexts", "meta")
	err := dependencies.FileSystem.WalkDir(metaRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "meta.json" {
			return nil
		}
		data, err := dependencies.FileSystem.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to load metafile %s: %w", path, err)
		}
		var payload struct {
			Name     string `json:"Name"`
			Metadata struct {
				Description string `json:"Description"`
			} `json:"Metadata"`
			Endpoints map[string]struct {
				Host          string `json:"Host"`
				SkipTLSVerify bool   `json:"SkipTLSVerify"`
			} `json:"Endpoints"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return fmt.Errorf("Failed to load metafile %s: %w", path, err)
		}
		if payload.Name == "" {
			return fmt.Errorf("Failed to load metafile %s: missing Name", path)
		}
		if payload.Name == "default" {
			return fmt.Errorf(`"default" is a reserved context name`)
		}
		endpoint := payload.Endpoints["docker"]
		id := contextID(payload.Name)
		contexts[payload.Name] = dockerContext{
			Name:        payload.Name,
			Description: payload.Metadata.Description,
			MetaPath:    filepath.Dir(path),
			TLSPath:     filepath.Join(configDir, "contexts", "tls", id),
			Host:        endpoint.Host,
			SkipTLS:     endpoint.SkipTLSVerify,
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return contexts, nil
}

func defaultContext(dependencies docker.Dependencies) dockerContext {
	host := defaultUnixSocket
	if value, found := dependencies.Environment.LookupEnv("DOCKER_HOST"); found && value != "" {
		host = value
	}
	return dockerContext{
		Name:        "default",
		Description: defaultDescription,
		Host:        host,
		InMemory:    true,
	}
}

func normalizeDockerHost(host string) string {
	scheme, rest, found := strings.Cut(host, "://")
	if !found {
		return host
	}
	switch scheme {
	case "http", "https":
		scheme = "tcp"
	case "http+unix":
		scheme = "unix"
	}
	return scheme + "://" + rest
}

func contextID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

func fileExists(fileSystem docker.FileSystem, path string) bool {
	_, err := fileSystem.Stat(path)
	return err == nil
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message, Contexts: []ContextInfo{}}
}
