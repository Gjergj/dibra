package docker_image

import (
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type ContainerLimits struct {
	Memory     string `json:"memory"`
	MemorySwap string `json:"memswap"`
	CPUShares  int64  `json:"cpushares"`
	CPUSetCPUs string `json:"cpusetcpus"`
}

type BuildOptions struct {
	CacheFrom       []string         `json:"cache_from"`
	Dockerfile      string           `json:"dockerfile"`
	HTTPTimeout     int              `json:"http_timeout"`
	Path            string           `json:"path"`
	Pull            *bool            `json:"pull"`
	Remove          *bool            `json:"rm"`
	Network         string           `json:"network"`
	NoCache         bool             `json:"nocache"`
	EtcHosts        map[string]any   `json:"etc_hosts"`
	Args            map[string]any   `json:"args"`
	ContainerLimits *ContainerLimits `json:"container_limits"`
	UseConfigProxy  *bool            `json:"use_config_proxy"`
	Target          string           `json:"target"`
	Platform        string           `json:"platform"`
	ShmSize         string           `json:"shm_size"`
	Labels          map[string]any   `json:"labels"`
}

// PullOptions is the pinned upstream pull dictionary. Policy is a Dibra
// compatibility extension for the former pull: missing|always|never syntax.
type PullOptions struct {
	Platform string `json:"platform"`
	Policy   string `json:"policy,omitempty"`
}

func (options *PullOptions) UnmarshalJSON(data []byte) error {
	type plain PullOptions
	var object plain
	if err := json.Unmarshal(data, &object); err == nil {
		*options = PullOptions(object)
		return nil
	}
	var policy string
	if err := json.Unmarshal(data, &policy); err == nil {
		switch policy {
		case "missing", "always", "never":
			options.Policy = policy
			return nil
		default:
			return fmt.Errorf("pull policy must be missing, always, or never")
		}
	}
	var legacy bool
	if err := json.Unmarshal(data, &legacy); err == nil {
		if legacy {
			options.Policy = "always"
		} else {
			options.Policy = "missing"
		}
		return nil
	}
	return fmt.Errorf("pull must be a dictionary")
}

type Request struct {
	docker.CommonArgs

	Name        string        `json:"name"`
	Source      string        `json:"source"`
	Build       *BuildOptions `json:"build"`
	ArchivePath string        `json:"archive_path"`
	LoadPath    string        `json:"load_path"`
	ForceSource bool          `json:"force_source"`
	ForceAbsent bool          `json:"force_absent"`
	ForceTag    bool          `json:"force_tag"`
	Pull        *PullOptions  `json:"pull"`
	Push        bool          `json:"push"`
	Repository  string        `json:"repository"`
	State       string        `json:"state"`
	Tag         string        `json:"tag"`

	// Direct credentials are retained as a documented Dibra compatibility
	// extension; upstream docker_image reads credentials from config.json.
	RegistryUsername string `json:"registry_username"`
	RegistryPassword string `json:"registry_password"`

	providedArguments map[string]bool
}

func (request *Request) SetProvidedArguments(arguments []string) {
	request.providedArguments = make(map[string]bool, len(arguments))
	for _, argument := range arguments {
		request.providedArguments[argument] = true
	}
}

func (request Request) ProvidedArguments() map[string]bool {
	return request.providedArguments
}

func (request Request) argumentProvided(name string) bool {
	return request.providedArguments == nil || request.providedArguments[name]
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Actions []string       `json:"actions"`
	Image   map[string]any `json:"image"`
	Stdout  string         `json:"stdout,omitempty"`
}
