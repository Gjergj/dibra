package docker_image_info

import "github.com/gjergjiramku/goansible/internal/modules/docker"

// Request represents the module arguments
type Request struct {
	docker.CommonArgs

	Name string `json:"name"` // Image name, tag, or ID to inspect (e.g., "alpine:latest", "sha256:abc123")
}

// Response represents the module return value
type Response struct {
	Changed bool                     `json:"changed"`
	Failed  bool                     `json:"failed"`
	Msg     string                   `json:"msg,omitempty"`
	Exists  bool                     `json:"exists"`
	Images  []map[string]interface{} `json:"images,omitempty"` // List of matching images
}
