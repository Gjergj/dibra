package docker_swarm_info

import "github.com/gjergjiramku/goansible/internal/modules/docker"

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	Nodes    bool `json:"nodes"`    // Include node list
	Verbose  bool `json:"verbose"`  // Include detailed node info
}

// Response represents the module return value
type Response struct {
	Changed   bool                     `json:"changed"`
	Failed    bool                     `json:"failed"`
	Msg       string                   `json:"msg,omitempty"`
	InSwarm   bool                     `json:"in_swarm"`
	IsManager bool                     `json:"is_manager"`
	SwarmInfo map[string]interface{}   `json:"swarm_info,omitempty"`
	Nodes     []map[string]interface{} `json:"nodes,omitempty"`
}
