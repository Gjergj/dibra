package docker_swarm_service_info

import (
	"github.com/docker/docker/api/types/swarm"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name string `json:"name"` // Service name or ID
}

type Response struct {
	Changed   bool        `json:"changed"`
	Failed    bool        `json:"failed"`
	Msg       string      `json:"msg,omitempty"`
	Exists    bool        `json:"exists"`
	Service   interface{} `json:"service,omitempty"`
	ServiceID string      `json:"service_id,omitempty"`
	Tasks     []TaskInfo  `json:"tasks,omitempty"`
}

// TaskInfo contains summarized task information
type TaskInfo struct {
	ID           string           `json:"id"`
	NodeID       string           `json:"node_id"`
	Status       swarm.TaskStatus `json:"status"`
	DesiredState swarm.TaskState  `json:"desired_state"`
	Slot         int              `json:"slot"`
}
