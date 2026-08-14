package docker_swarm_service_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name string `json:"name"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Exists  bool           `json:"exists"`
	Service map[string]any `json:"service"`
}
