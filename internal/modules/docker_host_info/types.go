package docker_host_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	// Options for what information to include
	Containers bool `json:"containers"` // Include container count
	Images     bool `json:"images"`     // Include image count
	Volumes    bool `json:"volumes"`    // Include volume count
	DiskUsage  bool `json:"disk_usage"` // Include disk usage (can be slow)
}

// Response represents the module return value
type Response struct {
	Changed   bool                   `json:"changed"`
	Failed    bool                   `json:"failed"`
	Msg       string                 `json:"msg,omitempty"`
	HostInfo  map[string]interface{} `json:"host_info,omitempty"`
	DiskUsage map[string]interface{} `json:"disk_usage,omitempty"`
}
