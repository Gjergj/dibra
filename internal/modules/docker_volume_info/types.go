package docker_volume_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	Name string `json:"name"` // Volume name to inspect
}

// Response represents the module return value
type Response struct {
	Changed bool                   `json:"changed"`
	Failed  bool                   `json:"failed"`
	Msg     string                 `json:"msg,omitempty"`
	Exists  bool                   `json:"exists"`
	Volume  map[string]interface{} `json:"volume,omitempty"`
}
