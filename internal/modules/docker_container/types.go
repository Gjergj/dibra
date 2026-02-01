package docker_container

import "github.com/gjergjiramku/goansible/internal/modules/docker"

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`   // present, absent, started, stopped
	Command       interface{}       `json:"command"` // string or []string
	Entrypoint    interface{}       `json:"entrypoint"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	ExposedPorts  []string          `json:"exposed_ports"`
	Ports         []string          `json:"ports"`   // Bindings
	Volumes       []string          `json:"volumes"` // Bindings
	NetworkMode   string            `json:"network_mode"`
	Networks      []Network         `json:"networks"`
	RestartPolicy string            `json:"restart_policy"`
	AutoRemove    bool              `json:"auto_remove"`
	Privileged    bool              `json:"privileged"`
	User          string            `json:"user"`
	WorkingDir    string            `json:"working_dir"`
	Hostname      string            `json:"hostname"`
	Domainname    string            `json:"domainname"`
	Labels        map[string]string `json:"labels"`
	Links         []string          `json:"links"`
	LogDriver     string            `json:"log_driver"`
	LogOptions    map[string]string `json:"log_options"`

	// Idempotency
	Comparisons map[string]string `json:"comparisons"`
	Recreate    bool              `json:"recreate"`
	ForceKill   bool              `json:"force_kill"`
	KeepVolumes bool              `json:"keep_volumes"`
	Pull        bool              `json:"pull"` // Pull image if missing or always? Ansible defaults to missing.
}

type Network struct {
	Name        string   `json:"name"`
	IPv4Address string   `json:"ipv4_address"`
	IPv6Address string   `json:"ipv6_address"`
	Links       []string `json:"links"`
	Aliases     []string `json:"aliases"`
}

// Response represents the module return value
type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	Container map[string]interface{} `json:"container,omitempty"` // Full inspection data
	Stdout    string                 `json:"stdout,omitempty"`
	Stderr    string                 `json:"stderr,omitempty"`
	Diff      interface{}            `json:"diff,omitempty"`
}
