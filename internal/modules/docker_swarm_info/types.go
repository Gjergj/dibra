package docker_swarm_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Nodes           bool             `json:"nodes"`
	NodesFilters    docker.FilterMap `json:"nodes_filters"`
	Services        bool             `json:"services"`
	ServicesFilters docker.FilterMap `json:"services_filters"`
	Tasks           bool             `json:"tasks"`
	TasksFilters    docker.FilterMap `json:"tasks_filters"`
	UnlockKey       bool             `json:"unlock_key"`
	VerboseOutput   bool             `json:"verbose_output"`
}

type Response struct {
	Changed            bool              `json:"changed"`
	Failed             bool              `json:"failed"`
	Msg                string            `json:"msg,omitempty"`
	CanTalkToDocker    bool              `json:"can_talk_to_docker"`
	DockerSwarmActive  bool              `json:"docker_swarm_active"`
	DockerSwarmManager bool              `json:"docker_swarm_manager"`
	SwarmFacts         map[string]any    `json:"swarm_facts,omitempty"`
	SwarmUnlockKey     any               `json:"swarm_unlock_key,omitempty"`
	Nodes              *[]map[string]any `json:"nodes,omitempty"`
	Services           *[]map[string]any `json:"services,omitempty"`
	Tasks              *[]map[string]any `json:"tasks,omitempty"`
}
