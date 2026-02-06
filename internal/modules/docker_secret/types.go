package docker_secret

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// DataHashLabel is the label key used to store the hash of secret data for idempotency
const DataHashLabel = "goansible.data_hash"

type Request struct {
	docker.CommonArgs

	Name            string            `json:"name"`
	Data            string            `json:"data"`        // The secret content
	DataIsB64       bool              `json:"data_is_b64"` // If true, decode data from base64
	Labels          map[string]string `json:"labels"`
	Force           bool              `json:"force"`            // Force recreate even if hash matches
	State           string            `json:"state"`            // present, absent
	RollingVersions bool              `json:"rolling_versions"` // Use versioned names (secret-v1, secret-v2, etc.)
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
	DataHash string `json:"data_hash,omitempty"` // SHA256 hash of the data (for verification)
}
