package docker_container_copy_into

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Container     string `json:"container"`
	Path          string `json:"path"`           // Local path on agent machine
	Content       string `json:"content"`        // File content (alternative to path)
	ContentIsB64  bool   `json:"content_is_b64"` // If true, content is base64 encoded
	ContainerPath string `json:"container_path"` // Destination path inside container
	Follow        bool   `json:"follow"`         // Follow symlinks in container
	LocalFollow   bool   `json:"local_follow"`   // Follow symlinks locally
	OwnerID       *int   `json:"owner_id"`       // Owner UID for file
	GroupID       *int   `json:"group_id"`       // Group GID for file
	Mode          string `json:"mode"`           // File mode (octal string like "0644")
	Force         *bool  `json:"force"`          // Force overwrite without idempotency checks
}

type Response struct {
	Changed       bool   `json:"changed"`
	Failed        bool   `json:"failed"`
	Msg           string `json:"msg,omitempty"`
	ContainerPath string `json:"container_path,omitempty"`
}
