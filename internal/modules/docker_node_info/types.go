package docker_node_info

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
)

type Request struct {
	docker.CommonArgs

	Name string `json:"name"` // Node hostname or ID, or empty for self
	Self bool   `json:"self"` // Get info for the current node
}

type Response struct {
	Changed bool        `json:"changed"`
	Failed  bool        `json:"failed"`
	Msg     string      `json:"msg,omitempty"`
	Exists  bool        `json:"exists"`
	Node    interface{} `json:"node,omitempty"`
	NodeID  string      `json:"node_id,omitempty"`
	Tasks   []TaskInfo  `json:"tasks,omitempty"`
}

// TaskInfo contains summarized task information
type TaskInfo struct {
	ID           string           `json:"id"`
	ServiceID    string           `json:"service_id"`
	Status       swarm.TaskStatus `json:"status"`
	DesiredState swarm.TaskState  `json:"desired_state"`
	Slot         int              `json:"slot"`
}
