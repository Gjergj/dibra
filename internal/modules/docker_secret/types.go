package docker_secret

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name            string            `json:"name"`
	Data            string            `json:"data"`        // The secret content (base64 encoded usually? No, string/bytes)
	DataIsB64       bool              `json:"data_is_b64"` // If true, decode data from base64
	Labels          map[string]string `json:"labels"`
	Force           bool              `json:"force"`            // Force update (rotates secret? usually secrets are immutable)
	State           string            `json:"state"`            // present, absent
	RollingVersions bool              `json:"rolling_versions"` // Not standard? Ansible might have it.
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
}
