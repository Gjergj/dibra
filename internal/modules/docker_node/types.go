package docker_node

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Hostname     string            `json:"hostname"`
	Self         bool              `json:"self"`         // Modify the node the agent is running on
	Availability string            `json:"availability"` // active, pause, drain
	Role         string            `json:"role"`         // manager, worker
	Labels       map[string]string `json:"labels"`
	LabelsState  string            `json:"labels_state"` // merge (default), replace
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
}
