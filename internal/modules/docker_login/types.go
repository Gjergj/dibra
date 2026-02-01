package docker_login

import "github.com/gjergjiramku/goansible/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Username   string `json:"username"`
	Password   string `json:"password"`
	Registry   string `json:"registry"` // URL
	Email      string `json:"email"`
	ConfigPath string `json:"config_path"` // Path to config.json (default ~/.docker/config.json)
	State      string `json:"state"`       // present, absent (logout)
	Relogin    bool   `json:"relogin"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Token   string `json:"token,omitempty"`
}
