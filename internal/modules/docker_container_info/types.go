package docker_container_info

import "github.com/gjergjiramku/goansible/internal/modules/docker"

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	Name string `json:"name"` // Container name or ID to inspect
}

// Response represents the module return value
type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	Exists    bool                   `json:"exists"`
	Container map[string]interface{} `json:"container,omitempty"`
}
