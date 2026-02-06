package docker_volume

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name          string            `json:"name"`
	State         string            `json:"state"` // present, absent
	Driver        string            `json:"driver"`
	DriverOptions map[string]string `json:"driver_options"`
	Labels        map[string]string `json:"labels"`
	Recreate      string            `json:"recreate"` // always, never (default: never)
	Force         bool              `json:"force"`    // Force removal even if in use
}

type Response struct {
	Changed    bool              `json:"changed"`
	Failed     bool              `json:"failed"`
	Msg        string            `json:"msg,omitempty"`
	VolumeID   string            `json:"volume_id,omitempty"`
	Name       string            `json:"name,omitempty"`
	Driver     string            `json:"driver,omitempty"`
	Mountpoint string            `json:"mountpoint,omitempty"`
	CreatedAt  string            `json:"created_at,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}
