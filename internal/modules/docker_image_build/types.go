package docker_image_build

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name       string            `json:"name"`       // Image name (required)
	Tag        string            `json:"tag"`        // Image tag (default: latest)
	Path       string            `json:"path"`       // Build context path (required)
	Dockerfile string            `json:"dockerfile"` // Alternate Dockerfile name
	CacheFrom  []string          `json:"cache_from"` // Cache source images
	Pull       bool              `json:"pull"`       // Pull newer FROM images
	Network    string            `json:"network"`    // Network for RUN instructions
	NoCache    bool              `json:"nocache"`    // Disable cache
	EtcHosts   map[string]string `json:"etc_hosts"`  // Extra /etc/hosts entries
	Args       map[string]string `json:"args"`       // Build arguments
	Target     string            `json:"target"`     // Target build stage
	Platform   []string          `json:"platform"`   // Target platforms
	ShmSize    string            `json:"shm_size"`   // /dev/shm size
	Labels     map[string]string `json:"labels"`     // Image labels
	Rebuild    string            `json:"rebuild"`    // never or always
	Push       bool              `json:"push"`       // Push after build
}

type Response struct {
	Changed  bool              `json:"changed"`
	Failed   bool              `json:"failed"`
	Msg      string            `json:"msg,omitempty"`
	Image    map[string]string `json:"image,omitempty"`
	ImageID  string            `json:"image_id,omitempty"`  // The resulting image ID
	Digest   string            `json:"digest,omitempty"`    // The image digest (if pushed)
	Stdout   string            `json:"stdout,omitempty"`
	Stderr   string            `json:"stderr,omitempty"`
	Logs     []string          `json:"logs,omitempty"`      // Build log lines
}
