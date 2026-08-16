package docker_container

import (
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// PullPolicy is the community.docker image pull policy. Booleans remain
// accepted for compatibility with older collection releases.
type PullPolicy string

const (
	PullMissing PullPolicy = "missing"
	PullAlways  PullPolicy = "always"
	PullNever   PullPolicy = "never"
)

func (p *PullPolicy) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*p = PullPolicy(value)
		return nil
	}
	var valueBool bool
	if err := json.Unmarshal(data, &valueBool); err == nil {
		if valueBool {
			*p = PullAlways
		} else {
			*p = PullMissing
		}
		return nil
	}
	return fmt.Errorf("pull must be one of never, missing, always, true, or false")
}

// RecreatePolicy retains Dibra's historical string spellings while exposing
// upstream's boolean recreate option as the canonical form.
type RecreatePolicy string

const (
	RecreateAuto   RecreatePolicy = "auto"
	RecreateAlways RecreatePolicy = "always"
	RecreateNever  RecreatePolicy = "never"
)

func (r *RecreatePolicy) UnmarshalJSON(data []byte) error {
	var valueBool bool
	if err := json.Unmarshal(data, &valueBool); err == nil {
		if valueBool {
			*r = RecreateAlways
		} else {
			*r = RecreateAuto
		}
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*r = RecreatePolicy(value)
		return nil
	}
	return fmt.Errorf("recreate must be a boolean or one of auto, always, never")
}

// Request is the community.docker.docker_container 5.2.2 argument contract.
// Pointer fields preserve the distinction between omitted values and explicit
// false/zero values, which is essential to comparison and compatibility-mode
// behavior.
type Request struct {
	docker.CommonArgs

	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`

	Cleanup                  bool              `json:"cleanup"`
	Comparisons              map[string]string `json:"comparisons"`
	ContainerDefaultBehavior string            `json:"container_default_behavior"`
	CommandHandling          string            `json:"command_handling"`
	DefaultHostIP            *string           `json:"default_host_ip"`
	ForceKill                bool              `json:"force_kill"`
	HealthyWaitTimeout       *float64          `json:"healthy_wait_timeout"`
	ImageComparison          string            `json:"image_comparison"`
	ImageLabelMismatch       string            `json:"image_label_mismatch"`
	ImageNameMismatch        string            `json:"image_name_mismatch"`
	KeepVolumes              *bool             `json:"keep_volumes"`
	KillSignal               string            `json:"kill_signal"`
	NetworksCLICompatible    *bool             `json:"networks_cli_compatible"`
	OutputLogs               bool              `json:"output_logs"`
	Paused                   *bool             `json:"paused"`
	Pull                     PullPolicy        `json:"pull"`
	PullCheckModeBehavior    string            `json:"pull_check_mode_behavior"`
	Recreate                 RecreatePolicy    `json:"recreate"`
	RemovalWaitTimeout       *float64          `json:"removal_wait_timeout"`
	Restart                  bool              `json:"restart"`

	Command    any               `json:"command"`
	Entrypoint []string          `json:"entrypoint"`
	Env        map[string]string `json:"env"`
	EnvFile    string            `json:"env_file"`

	AutoRemove  *bool `json:"auto_remove"`
	Detach      *bool `json:"detach"`
	Interactive *bool `json:"interactive"`
	Init        *bool `json:"init"`
	Privileged  *bool `json:"privileged"`
	ReadOnly    *bool `json:"read_only"`
	TTY         *bool `json:"tty"`

	BlkioWeight       *int64   `json:"blkio_weight"`
	CgroupParent      string   `json:"cgroup_parent"`
	CgroupnsMode      string   `json:"cgroupns_mode"`
	CPUPeriod         *int64   `json:"cpu_period"`
	CPUQuota          *int64   `json:"cpu_quota"`
	CPUShares         *int64   `json:"cpu_shares"`
	CPUs              *float64 `json:"cpus"`
	CPUSetCPUs        string   `json:"cpuset_cpus"`
	CPUSetMems        string   `json:"cpuset_mems"`
	KernelMemory      *string  `json:"kernel_memory"`
	Memory            *string  `json:"memory"`
	MemoryReservation *string  `json:"memory_reservation"`
	MemorySwap        *string  `json:"memory_swap"`
	MemorySwappiness  *int64   `json:"memory_swappiness"`
	OOMKiller         *bool    `json:"oom_killer"`
	OOMScoreAdj       *int     `json:"oom_score_adj"`
	PidsLimit         *int64   `json:"pids_limit"`

	Capabilities      []string          `json:"capabilities"`
	CapDrop           []string          `json:"cap_drop"`
	DeviceCgroupRules []string          `json:"device_cgroup_rules"`
	DeviceReadBPS     []DeviceRate      `json:"device_read_bps"`
	DeviceWriteBPS    []DeviceRate      `json:"device_write_bps"`
	DeviceReadIOPS    []DeviceIOPS      `json:"device_read_iops"`
	DeviceWriteIOPS   []DeviceIOPS      `json:"device_write_iops"`
	DeviceRequests    []DeviceRequest   `json:"device_requests"`
	Devices           []string          `json:"devices"`
	DNSServers        []string          `json:"dns_servers"`
	DNSOptions        []string          `json:"dns_opts"`
	DNSSearchDomains  []string          `json:"dns_search_domains"`
	EtcHosts          map[string]string `json:"etc_hosts"`
	Groups            []string          `json:"groups"`
	SecurityOptions   []string          `json:"security_opts"`
	StorageOptions    map[string]string `json:"storage_opts"`
	Sysctls           map[string]string `json:"sysctls"`
	Ulimits           UlimitList        `json:"ulimits"`

	Domainname     string            `json:"domainname"`
	Healthcheck    *Healthcheck      `json:"healthcheck"`
	Hostname       string            `json:"hostname"`
	IPCMode        string            `json:"ipc_mode"`
	Labels         map[string]string `json:"labels"`
	Links          []string          `json:"links"`
	LogDriver      string            `json:"log_driver"`
	LogOptions     map[string]string `json:"log_options"`
	MacAddress     string            `json:"mac_address"`
	PIDMode        string            `json:"pid_mode"`
	Platform       string            `json:"platform"`
	RestartPolicy  string            `json:"restart_policy"`
	RestartRetries *int              `json:"restart_retries"`
	Runtime        string            `json:"runtime"`
	ShmSize        *string           `json:"shm_size"`
	StopSignal     string            `json:"stop_signal"`
	StopTimeout    *int              `json:"stop_timeout"`
	User           string            `json:"user"`
	UsernsMode     string            `json:"userns_mode"`
	UTSMode        string            `json:"uts"`
	WorkingDir     string            `json:"working_dir"`

	ExposedPorts    []string `json:"exposed_ports"`
	PublishAllPorts *bool    `json:"publish_all_ports"`
	PublishedPorts  []string `json:"published_ports"`

	Mounts       []Mount  `json:"mounts"`
	Tmpfs        []string `json:"tmpfs"`
	VolumeDriver string   `json:"volume_driver"`
	Volumes      []string `json:"volumes"`
	VolumesFrom  []string `json:"volumes_from"`

	NetworkMode string    `json:"network_mode"`
	Networks    []Network `json:"networks"`

	RegistryUsername string `json:"registry_username"`
	RegistryPassword string `json:"registry_password"`

	resolvedPlatform  *ocispec.Platform
	providedArguments map[string]bool
}

// SetProvidedArguments records the canonical argument keys that survived
// registry alias normalization. The controller preserves this set when it
// renders and forwards a request, allowing the executor to distinguish an
// omitted scalar from an explicitly supplied empty value.
func (request *Request) SetProvidedArguments(names []string) {
	request.providedArguments = make(map[string]bool, len(names))
	for _, name := range names {
		request.providedArguments[name] = true
	}
}

// ProvidedArguments exposes the immutable presence map to the registry's
// controller-side JSON projection.
func (request Request) ProvidedArguments() map[string]bool {
	return request.providedArguments
}

func (request Request) argumentProvided(name string, directFallback bool) bool {
	if request.providedArguments == nil {
		return directFallback
	}
	return request.providedArguments[name]
}

type Network struct {
	Name        string            `json:"name"`
	IPv4Address string            `json:"ipv4_address"`
	IPv6Address string            `json:"ipv6_address"`
	Links       []string          `json:"links"`
	Aliases     []string          `json:"aliases"`
	MacAddress  string            `json:"mac_address"`
	DriverOpts  map[string]string `json:"driver_opts"`
	GWPriority  *int              `json:"gw_priority"`
}

type Healthcheck struct {
	Test              any    `json:"test"`
	TestCLICompatible bool   `json:"test_cli_compatible"`
	Interval          string `json:"interval"`
	Timeout           string `json:"timeout"`
	StartPeriod       string `json:"start_period"`
	StartInterval     string `json:"start_interval"`
	Retries           *int   `json:"retries"`
}

type DeviceRate struct {
	Path string `json:"path"`
	Rate string `json:"rate"`
}

type DeviceIOPS struct {
	Path string `json:"path"`
	Rate int64  `json:"rate"`
}

type DeviceRequest struct {
	Capabilities [][]string        `json:"capabilities"`
	Count        *int              `json:"count"`
	DeviceIDs    []string          `json:"device_ids"`
	Driver       string            `json:"driver"`
	Options      map[string]string `json:"options"`
}

type Mount struct {
	Target                 string               `json:"target"`
	Source                 string               `json:"source"`
	Type                   string               `json:"type"`
	ReadOnly               *bool                `json:"read_only"`
	Consistency            string               `json:"consistency"`
	Propagation            string               `json:"propagation"`
	NoCopy                 *bool                `json:"no_copy"`
	Labels                 map[string]string    `json:"labels"`
	VolumeDriver           string               `json:"volume_driver"`
	VolumeOptions          map[string]string    `json:"volume_options"`
	TmpfsSize              string               `json:"tmpfs_size"`
	TmpfsMode              string               `json:"tmpfs_mode"`
	NonRecursive           *bool                `json:"non_recursive"`
	CreateMountpoint       *bool                `json:"create_mountpoint"`
	ReadOnlyNonRecursive   *bool                `json:"read_only_non_recursive"`
	ReadOnlyForceRecursive *bool                `json:"read_only_force_recursive"`
	Subpath                string               `json:"subpath"`
	TmpfsOptions           []map[string]*string `json:"tmpfs_options"`
}

// UlimitList accepts the canonical community.docker string form and Dibra's
// former mapping form so existing playbooks continue to decode explicitly.
type UlimitList []string

func (values *UlimitList) UnmarshalJSON(data []byte) error {
	var canonical []string
	if err := json.Unmarshal(data, &canonical); err == nil {
		*values = canonical
		return nil
	}
	var legacy []struct {
		Name string `json:"name"`
		Soft int64  `json:"soft"`
		Hard int64  `json:"hard"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("ulimits must contain name:soft[:hard] strings: %w", err)
	}
	result := make(UlimitList, 0, len(legacy))
	for _, value := range legacy {
		if value.Name == "" {
			return fmt.Errorf("ulimit name is required")
		}
		result = append(result, fmt.Sprintf("%s:%d:%d", value.Name, value.Soft, value.Hard))
	}
	*values = result
	return nil
}

type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	Container map[string]interface{} `json:"container,omitempty"`
	Status    *int64                 `json:"status,omitempty"`
	Stdout    string                 `json:"stdout,omitempty"`
	Stderr    string                 `json:"stderr,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
	Diff      map[string]interface{} `json:"diff,omitempty"`
	Actions   []map[string]any       `json:"actions,omitempty"`
}
