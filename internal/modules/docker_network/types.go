package docker_network

import "github.com/gjergjiramku/goansible/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name       string            `json:"name"`
	State      string            `json:"state"` // present, absent
	Driver     string            `json:"driver"`
	Options    map[string]string `json:"options"`
	IPAMConfig []IPAMConfig      `json:"ipam_config"`
	Labels     map[string]string `json:"labels"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	Scope      string            `json:"scope"`
	Force      bool              `json:"force"`
}

type IPAMConfig struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
	IPRange string `json:"ip_range"`
}

type Response struct {
	Changed   bool   `json:"changed"`
	Failed    bool   `json:"failed"`
	Msg       string `json:"msg,omitempty"`
	NetworkID string `json:"network_id,omitempty"`
}
