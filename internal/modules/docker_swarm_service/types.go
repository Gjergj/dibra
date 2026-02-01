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
	UpdateDelay       string            `json:"update_delay"`
	UpdateParallelism uint64            `json:"update_parallelism"`
	ForceUpdate       bool              `json:"force_update"`
	ResolveImage      string            `json:"resolve_image"` // always, changed, never
}

type PortPublish struct {
	PublishedPort uint32 `json:"published_port"`
	TargetPort    uint32 `json:"target_port"`
	Protocol      string `json:"protocol"` // tcp, udp
	Mode          string `json:"mode"`     // ingress, host
}

type Response struct {
	Changed   bool   `json:"changed"`
	Failed    bool   `json:"failed"`
	Msg       string `json:"msg,omitempty"`
	ServiceID string `json:"service_id,omitempty"`
}
