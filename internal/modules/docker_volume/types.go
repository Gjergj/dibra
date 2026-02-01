package docker_volume

import "github.com/gjergjiramku/goansible/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name          string            `json:"name"`
	State         string            `json:"state"` // present, absent
	Driver        string            `json:"driver"`
	DriverOptions map[string]string `json:"driver_options"`
	Labels        map[string]string `json:"labels"`
	Recreate      string            `json:"recreate"` // always, never (default: never)
	Force         bool              `json:"force"`    // Force removal
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	VolumeID string `json:"volume_id,omitempty"`
}
