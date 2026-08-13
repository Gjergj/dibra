package docker_image_load

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Path string `json:"path"` // Path to .tar archive (required)
}

type Response struct {
	Changed    bool             `json:"changed"`
	Failed     bool             `json:"failed"`
	Msg        string           `json:"msg,omitempty"`
	ImageNames []string         `json:"image_names"`
	Images     []map[string]any `json:"images"`
	Stdout     string           `json:"stdout,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}
