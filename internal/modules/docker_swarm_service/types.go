package docker_swarm_service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`

	Args             []string          `json:"args"`
	Command          any               `json:"command"`
	Configs          []FileReference   `json:"configs"`
	ContainerLabels  LabelMap          `json:"container_labels"`
	DNS              []string          `json:"dns"`
	DNSOptions       []string          `json:"dns_options"`
	DNSSearch        []string          `json:"dns_search"`
	EndpointMode     string            `json:"endpoint_mode"`
	Env              any               `json:"env"`
	EnvFiles         []string          `json:"env_files"`
	ForceUpdate      bool              `json:"force_update"`
	Groups           []any             `json:"groups"`
	Healthcheck      *Healthcheck      `json:"healthcheck"`
	Hostname         *string           `json:"hostname"`
	Hosts            HostMap           `json:"hosts"`
	Init             *bool             `json:"init"`
	Labels           LabelMap          `json:"labels"`
	Limits           *ResourceSpec     `json:"limits"`
	Logging          *LoggingSpec      `json:"logging"`
	Mode             string            `json:"mode"`
	Mounts           []MountSpec       `json:"mounts"`
	Networks         NetworkList       `json:"networks"`
	Placement        *PlacementSpec    `json:"placement"`
	Publish          []PublishSpec     `json:"publish"`
	ReadOnly         *bool             `json:"read_only"`
	Replicas         *int64            `json:"replicas"`
	Reservations     *ResourceSpec     `json:"reservations"`
	ResolveImage     *bool             `json:"resolve_image"`
	RestartConfig    *RestartSpec      `json:"restart_config"`
	RollbackConfig   *UpdateSpec       `json:"rollback_config"`
	Secrets          []FileReference   `json:"secrets"`
	StopGracePeriod  *DurationValue    `json:"stop_grace_period"`
	StopSignal       *string           `json:"stop_signal"`
	Sysctls          LabelMap          `json:"sysctls"`
	TTY              *bool             `json:"tty"`
	UpdateConfig     *UpdateSpec       `json:"update_config"`
	User             *string           `json:"user"`
	WorkingDir       *string           `json:"working_dir"`
	CapAdd           []string          `json:"cap_add"`
	CapDrop          []string          `json:"cap_drop"`

	// Compatibility aliases retained from earlier Dibra playbooks.
	LimitCPU            *float64 `json:"limit_cpu"`
	LimitMemory         *int64   `json:"limit_memory"`
	Constraint          []string `json:"constraint"`
	RestartPolicy       string   `json:"restart_policy"`
	UpdateDelay         string   `json:"update_delay"`
	UpdateParallelism   *uint64  `json:"update_parallelism"`
	UpdateFailureAction string   `json:"update_failure_action"`
	UpdateOrder         string   `json:"update_order"`
	UpdateMonitor       string   `json:"update_monitor"`
	MaxFailureRatio     *float32 `json:"max_failure_ratio"`
	RollbackDelay           string   `json:"rollback_delay"`
	RollbackParallelism     *uint64  `json:"rollback_parallelism"`
	RollbackFailureAction   string   `json:"rollback_failure_action"`
	RollbackOrder           string   `json:"rollback_order"`
	RollbackMonitor         string   `json:"rollback_monitor"`
	RollbackMaxFailureRatio *float32 `json:"rollback_max_failure_ratio"`
}

type ResourceSpec struct {
	CPUs   *float64    `json:"cpus"`
	Memory *SizeValue  `json:"memory"`
}

type LoggingSpec struct {
	Driver  *string           `json:"driver"`
	Options map[string]string `json:"options"`
}

type PlacementSpec struct {
	Constraints        []string         `json:"constraints"`
	Preferences        []map[string]any `json:"preferences"`
	ReplicasMaxPerNode *uint64          `json:"replicas_max_per_node"`
}

type RestartSpec struct {
	Condition   *string        `json:"condition"`
	Delay       *DurationValue `json:"delay"`
	MaxAttempts *uint64        `json:"max_attempts"`
	Window      *DurationValue `json:"window"`
}

type UpdateSpec struct {
	Parallelism     *uint64        `json:"parallelism"`
	Delay           *DurationValue `json:"delay"`
	FailureAction   *string        `json:"failure_action"`
	Monitor         *DurationValue `json:"monitor"`
	MaxFailureRatio *float32       `json:"max_failure_ratio"`
	Order           *string        `json:"order"`
}

type Healthcheck struct {
	Test        any            `json:"test"`
	Interval    *DurationValue `json:"interval"`
	Timeout     *DurationValue `json:"timeout"`
	StartPeriod *DurationValue `json:"start_period"`
	Retries     *int           `json:"retries"`
}

type PublishSpec struct {
	PublishedPort *uint32 `json:"published_port"`
	TargetPort    uint32  `json:"target_port"`
	Protocol      string  `json:"protocol"`
	Mode          *string `json:"mode"`
}

type FileReference struct {
	ConfigID   string `json:"config_id"`
	ConfigName string `json:"config_name"`
	SecretID   string `json:"secret_id"`
	SecretName string `json:"secret_name"`
	Filename   string `json:"filename"`
	UID        any    `json:"uid"`
	GID        any    `json:"gid"`
	Mode       any    `json:"mode"`
}

type MountSpec struct {
	Source      *string           `json:"source"`
	Target      string            `json:"target"`
	Type        string            `json:"type"`
	Readonly    *bool             `json:"readonly"`
	ReadOnly    *bool             `json:"read_only"`
	Labels      map[string]string `json:"labels"`
	NoCopy      *bool             `json:"no_copy"`
	VolumeNoCopy *bool            `json:"volume_no_copy"`
	Propagation *string           `json:"propagation"`
	BindPropagation string        `json:"bind_propagation"`
	DriverConfig *MountDriver     `json:"driver_config"`
	VolumeDriver string           `json:"volume_driver"`
	VolumeLabels map[string]string `json:"volume_labels"`
	VolumeOptions map[string]string `json:"volume_options"`
	TmpfsSize   *SizeValue        `json:"tmpfs_size"`
	TmpfsMode   any               `json:"tmpfs_mode"`
}

type MountDriver struct {
	Name    string            `json:"name"`
	Options map[string]string `json:"options"`
}

type NetworkSpec struct {
	Name    string            `json:"name"`
	Aliases []string          `json:"aliases"`
	Options map[string]string `json:"options"`
}

type NetworkList []NetworkSpec

func (list *NetworkList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*list = nil
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("Only a list of strings or dictionaries are allowed to be passed as networks.")
	}
	result := make(NetworkList, 0, len(items))
	for _, item := range items {
		item = bytes.TrimSpace(item)
		if len(item) == 0 || bytes.Equal(item, []byte("null")) {
			continue
		}
		if item[0] == '"' {
			var name string
			if err := json.Unmarshal(item, &name); err != nil {
				return err
			}
			result = append(result, NetworkSpec{Name: name})
			continue
		}
		var spec NetworkSpec
		if err := json.Unmarshal(item, &spec); err != nil {
			return fmt.Errorf("Only a list of strings or dictionaries are allowed to be passed as networks.")
		}
		result = append(result, spec)
	}
	*list = result
	return nil
}

type LabelMap map[string]string

func (labels *LabelMap) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*labels = nil
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("labels must be a dictionary: %w", err)
	}
	result := make(map[string]string, len(raw))
	for key, value := range raw {
		text, err := labelValueToString(key, value)
		if err != nil {
			return err
		}
		result[key] = text
	}
	*labels = result
	return nil
}

func labelValueToString(key string, value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool:
		return "", fmt.Errorf("The value %v for %q of labels is not a string or something than can be safely converted to a string!", typed, key)
	case json.Number:
		text := typed.String()
		if strings.ContainsAny(text, ".eE") {
			return "", fmt.Errorf("The value %s for %q of labels is not a string or something than can be safely converted to a string!", text, key)
		}
		return text, nil
	case float64:
		return "", fmt.Errorf("The value %v for %q of labels is not a string or something than can be safely converted to a string!", typed, key)
	default:
		return fmt.Sprint(typed), nil
	}
}

type HostMap map[string]string

func (hosts *HostMap) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*hosts = nil
		return nil
	}
	if data[0] == '{' {
		var values map[string]string
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		*hosts = values
		return nil
	}
	var items []string
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("hosts must be a dictionary or a list of hostname:ip strings")
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		host, ip, ok := strings.Cut(item, ":")
		if !ok {
			host, ip, ok = strings.Cut(item, " ")
		}
		if !ok {
			return fmt.Errorf("invalid extra host %q, expected hostname:ip", item)
		}
		result[strings.TrimSpace(host)] = strings.TrimSpace(ip)
	}
	*hosts = result
	return nil
}

type SizeValue struct {
	Bytes int64
}

func (value SizeValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Bytes)
}

func (value *SizeValue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '{' {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		field, ok := raw["Bytes"]
		if !ok {
			return fmt.Errorf("size must be a string or integer")
		}
		var parsed int64
		if err := json.Unmarshal(field, &parsed); err != nil {
			return err
		}
		value.Bytes = parsed
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		parsed, err := number.Int64()
		if err != nil {
			return err
		}
		value.Bytes = parsed
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("size must be a string or integer")
	}
	parsed, err := parseHumanBytes(text)
	if err != nil {
		return err
	}
	value.Bytes = parsed
	return nil
}

type DurationValue struct {
	Nanoseconds int64
}

func (value DurationValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Nanoseconds)
}

func (value *DurationValue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '{' {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		field, ok := raw["Nanoseconds"]
		if !ok {
			return fmt.Errorf("duration must be a string or integer")
		}
		var parsed int64
		if err := json.Unmarshal(field, &parsed); err != nil {
			return err
		}
		value.Nanoseconds = parsed
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		parsed, err := number.Int64()
		if err != nil {
			return err
		}
		value.Nanoseconds = parsed
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("duration must be a string or integer")
	}
	parsed, err := nanosecondsFromRaw("duration", text)
	if err != nil {
		return err
	}
	if parsed == nil {
		return nil
	}
	value.Nanoseconds = *parsed
	return nil
}

type Diff struct {
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
}

type Response struct {
	Changed      bool           `json:"changed"`
	Failed       bool           `json:"failed"`
	Msg          string         `json:"msg,omitempty"`
	Rebuilt      bool           `json:"rebuilt"`
	Changes      []string       `json:"changes"`
	SwarmService map[string]any `json:"swarm_service,omitempty"`
	ServiceID    string         `json:"service_id,omitempty"`
	Diff         *Diff          `json:"diff,omitempty"`
}
