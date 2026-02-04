package docker_container

import (
	"encoding/json"

	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// PullPolicy represents the image pull behavior with backward compatibility
type PullPolicy string

const (
	PullMissing PullPolicy = "missing"
	PullAlways  PullPolicy = "always"
	PullNever   PullPolicy = "never"
)

// UnmarshalJSON handles both bool and string for backward compatibility
func (p *PullPolicy) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = PullPolicy(s)
		return nil
	}

	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*p = PullAlways
		} else {
			*p = PullMissing
		}
		return nil
	}

	*p = PullMissing
	return nil
}

// RecreatePolicy represents the container recreate behavior
type RecreatePolicy string

const (
	RecreateAuto   RecreatePolicy = "auto"
	RecreateAlways RecreatePolicy = "always"
	RecreateNever  RecreatePolicy = "never"
)

// UnmarshalJSON handles both bool and string for backward compatibility
func (r *RecreatePolicy) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = RecreatePolicy(s)
		return nil
	}

	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*r = RecreateAlways
		} else {
			*r = RecreateAuto
		}
		return nil
	}

	*r = RecreateAuto
	return nil
}

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	Name       string      `json:"name"`
	Image      string      `json:"image"`
	State      string      `json:"state"`      // present, absent, started, stopped
	Command    interface{} `json:"command"`    // string or []string
	Entrypoint interface{} `json:"entrypoint"` // string or []string
	Env        map[string]string `json:"env"`

	// Port bindings
	ExposedPorts []string `json:"exposed_ports"` // Ports to expose without host binding
	Ports        []string `json:"ports"`         // Port bindings: "host:container" or "ip:host:container"

	// Volume bindings
	Volumes []string `json:"volumes"` // Bind mounts: "host:container:mode"

	// Networking
	NetworkMode    string    `json:"network_mode"`    // bridge, host, none, container:<id>
	Networks       []Network `json:"networks"`        // Additional networks to connect
	NetworksAppend bool      `json:"networks_append"` // If true, don't disconnect from networks not in list

	// Container settings
	RestartPolicy string            `json:"restart_policy"` // no, always, on-failure[:max], unless-stopped
	AutoRemove    bool              `json:"auto_remove"`
	Privileged    bool              `json:"privileged"`
	User          string            `json:"user"`
	WorkingDir    string            `json:"working_dir"`
	Hostname      string            `json:"hostname"`
	Domainname    string            `json:"domainname"`
	Labels        map[string]string `json:"labels"`
	Links         []string          `json:"links"` // Legacy linking

	// Logging
	LogDriver  string            `json:"log_driver"`
	LogOptions map[string]string `json:"log_options"`

	// Capabilities (Tier 1)
	CapAdd  []string `json:"cap_add"`  // Capabilities to add
	CapDrop []string `json:"cap_drop"` // Capabilities to drop

	// Devices (Tier 1)
	Devices []string `json:"devices"` // Device mappings: "/dev/sda:/dev/xvda:rwm"

	// Health check (Tier 1)
	Healthcheck *Healthcheck `json:"healthcheck"`

	// Other Tier 1 options
	Init    bool     `json:"init"`     // Run init inside container
	Tmpfs   []string `json:"tmpfs"`    // Tmpfs mounts: "/run:rw,noexec,nosuid,size=65536k"
	ShmSize string   `json:"shm_size"` // /dev/shm size: "64m"

	// Resource limits (Tier 2)
	Ulimits    []Ulimit          `json:"ulimits"`      // Ulimit options
	Sysctls    map[string]string `json:"sysctls"`      // Sysctl options
	SecurityOpt []string         `json:"security_opt"` // Security options
	CPUs       float64           `json:"cpus"`         // CPU limit
	Memory     string            `json:"memory"`       // Memory limit: "512m"
	MemorySwap string            `json:"memory_swap"`  // Swap limit: "1g" or "-1" for unlimited
	PidsLimit  int64             `json:"pids_limit"`   // PID limit

	// Idempotency control
	Comparisons map[string]string `json:"comparisons"` // Field comparison modes: strict, ignore, allow_more_present
	Recreate    RecreatePolicy    `json:"recreate"`    // auto, always, never (default: auto)
	ForceKill   bool              `json:"force_kill"`  // Force kill on stop/remove
	KeepVolumes bool              `json:"keep_volumes"` // Keep volumes on remove

	// Image pull behavior
	Pull PullPolicy `json:"pull"` // missing (default), always, never

	// Registry authentication
	RegistryUsername string `json:"registry_username"` // Username for registry auth
	RegistryPassword string `json:"registry_password"` // Password for registry auth
}

type Network struct {
	Name        string   `json:"name"`
	IPv4Address string   `json:"ipv4_address"`
	IPv6Address string   `json:"ipv6_address"`
	Links       []string `json:"links"`
	Aliases     []string `json:"aliases"`
}

// Healthcheck configuration
type Healthcheck struct {
	Test        []string `json:"test"`         // Command to run: ["CMD", "curl", "-f", "http://localhost/"]
	Interval    string   `json:"interval"`     // Time between checks: "30s"
	Timeout     string   `json:"timeout"`      // Check timeout: "10s"
	StartPeriod string   `json:"start_period"` // Start period: "5s"
	Retries     int      `json:"retries"`      // Number of retries before unhealthy
}

// Ulimit configuration
type Ulimit struct {
	Name string `json:"name"` // nofile, nproc, etc.
	Soft int64  `json:"soft"`
	Hard int64  `json:"hard"`
}

// Response represents the module return value
type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	Container map[string]interface{} `json:"container,omitempty"`
	Stdout    string                 `json:"stdout,omitempty"`
	Stderr    string                 `json:"stderr,omitempty"`
	Diff      map[string]interface{} `json:"diff,omitempty"` // Changed fields with before/after
	Actions   []string               `json:"actions,omitempty"` // Actions taken: created, started, stopped, removed, updated
}
