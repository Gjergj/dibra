package docker_stack_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

// Request is the pinned community.docker.docker_stack_info 5.2.2 argument contract.
// The module has no module-specific options beyond shared CLI connection arguments.
type Request struct {
	docker.CommonArgs

	DockerCLI string `json:"docker_cli"`
}

// Response is the pinned docker_stack_info return contract.
type Response struct {
	Changed bool             `json:"changed"`
	Failed  bool             `json:"failed"`
	Msg     string           `json:"msg,omitempty"`
	RC      *int             `json:"rc,omitempty"`
	Stdout  string           `json:"stdout"`
	Stderr  string           `json:"stderr"`
	Results []map[string]any `json:"results"`
}
