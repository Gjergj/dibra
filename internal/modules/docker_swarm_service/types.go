package docker_swarm_service

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name              string            `json:"name"`
	Image             string            `json:"image"`
	State             string            `json:"state"` // present, absent
	Replicas          *uint64           `json:"replicas"`
	Args              []string          `json:"args"`
	Command           interface{}       `json:"command"` // string or []string
	Env               map[string]string `json:"env"`
	Publish           []PortPublish     `json:"publish"`
	Networks          []string          `json:"networks"`
	Labels            map[string]string `json:"labels"`
	LimitCPU          float64           `json:"limit_cpu"`
	LimitMemory       int64             `json:"limit_memory"` // Bytes
	Constraint        []string          `json:"constraint"`
	RestartPolicy     string            `json:"restart_policy"` // any, none, on-failure
	UpdateDelay         string            `json:"update_delay"`
	UpdateParallelism   uint64            `json:"update_parallelism"`
	UpdateFailureAction string            `json:"update_failure_action"` // pause, continue, rollback
	UpdateOrder         string            `json:"update_order"`          // stop-first, start-first
	UpdateMonitor       string            `json:"update_monitor"`
	MaxFailureRatio     float32           `json:"max_failure_ratio"`
	ForceUpdate         bool              `json:"force_update"`
	ResolveImage        string            `json:"resolve_image"` // always, changed, never

	// Rollback configuration
	RollbackDelay         string  `json:"rollback_delay"`
	RollbackParallelism   uint64  `json:"rollback_parallelism"`
	RollbackFailureAction string  `json:"rollback_failure_action"` // pause, continue
	RollbackOrder         string  `json:"rollback_order"`          // stop-first, start-first
	RollbackMonitor       string  `json:"rollback_monitor"`
	RollbackMaxFailureRatio float32 `json:"rollback_max_failure_ratio"`

	Configs []ConfigReference `json:"configs,omitempty"`
	Secrets []SecretReference `json:"secrets,omitempty"`

	// Phase 6.3: Additional Options
	Healthcheck *ServiceHealthcheck `json:"healthcheck,omitempty"`
	DNS         []string            `json:"dns,omitempty"`
	DNSSearch   []string            `json:"dns_search,omitempty"`
	DNSOptions  []string            `json:"dns_options,omitempty"`
	Hosts       []string            `json:"hosts,omitempty"` // Extra hosts (host:ip format)
	Mounts      []ServiceMount      `json:"mounts,omitempty"`
}

type PortPublish struct {
	PublishedPort uint32 `json:"published_port"`
	TargetPort    uint32 `json:"target_port"`
	Protocol      string `json:"protocol"` // tcp, udp
	Mode          string `json:"mode"`     // ingress, host
}

// ConfigReference for mounting Docker configs into service containers
type ConfigReference struct {
	ConfigName string `json:"config_name"`
	ConfigID   string `json:"config_id,omitempty"`
	FileName   string `json:"filename,omitempty"`
	UID        string `json:"uid,omitempty"`
	GID        string `json:"gid,omitempty"`
	Mode       uint32 `json:"mode,omitempty"`
}

// SecretReference for mounting Docker secrets into service containers
type SecretReference struct {
	SecretName string `json:"secret_name"`
	SecretID   string `json:"secret_id,omitempty"`
	FileName   string `json:"filename,omitempty"`
	UID        string `json:"uid,omitempty"`
	GID        string `json:"gid,omitempty"`
	Mode       uint32 `json:"mode,omitempty"`
}

// ServiceHealthcheck configuration for service containers
type ServiceHealthcheck struct {
	Test        []string `json:"test,omitempty"`
	Interval    string   `json:"interval,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	StartPeriod string   `json:"start_period,omitempty"`
	Retries     int      `json:"retries,omitempty"`
}

// ServiceMount configuration for service container mounts
type ServiceMount struct {
	Type        string `json:"type,omitempty"` // bind, volume, tmpfs
	Source      string `json:"source,omitempty"`
	Target      string `json:"target,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	Consistency string `json:"consistency,omitempty"`
	// Bind-specific options
	BindPropagation string `json:"bind_propagation,omitempty"`
	// Volume-specific options
	VolumeDriver  string            `json:"volume_driver,omitempty"`
	VolumeLabels  map[string]string `json:"volume_labels,omitempty"`
	VolumeOptions map[string]string `json:"volume_options,omitempty"`
	VolumeNoCopy  bool              `json:"volume_no_copy,omitempty"`
	// Tmpfs-specific options
	TmpfsSize int64  `json:"tmpfs_size,omitempty"`
	TmpfsMode uint32 `json:"tmpfs_mode,omitempty"`
}

type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	ServiceID string                 `json:"service_id,omitempty"`
	Diff      map[string]interface{} `json:"diff,omitempty"`
}
