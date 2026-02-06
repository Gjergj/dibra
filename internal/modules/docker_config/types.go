package docker_config

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// DataHashLabel is the label key used to store the hash of config data for idempotency
const DataHashLabel = "goansible.data_hash"

type Request struct {
	docker.CommonArgs

	Name      string            `json:"name"`
	Data      string            `json:"data"`        // The config content
	DataIsB64 bool              `json:"data_is_b64"` // If true, decode data from base64
	Labels    map[string]string `json:"labels"`
	Force     bool              `json:"force"` // Force recreate even if hash matches
	State     string            `json:"state"` // present, absent
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	ConfigID string `json:"config_id,omitempty"`
	DataHash string `json:"data_hash,omitempty"` // SHA256 hash of the data (for verification)
}
