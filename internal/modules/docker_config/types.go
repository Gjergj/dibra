package docker_config

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name      string            `json:"name"`
	Data      string            `json:"data"`
	DataIsB64 bool              `json:"data_is_b64"`
	Labels    map[string]string `json:"labels"`
	Force     bool              `json:"force"`
	State     string            `json:"state"`
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	ConfigID string `json:"config_id,omitempty"`
}
