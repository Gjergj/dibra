package docker_image_export

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Names []string `json:"names"` // List of image names or IDs (required)
	Tag   string   `json:"tag"`   // Default tag if not provided in name (default: latest)
	Path  string   `json:"path"`  // Resulting tar archive path (required)
	Force bool     `json:"force"` // Export even if archive exists
}

type Response struct {
	Changed bool                     `json:"changed"`
	Failed  bool                     `json:"failed"`
	Msg     string                   `json:"msg,omitempty"`
	Images  []map[string]interface{} `json:"images,omitempty"`
}
