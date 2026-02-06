package docker_login

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Username   string `json:"username"`
	Password   string `json:"password"`
	Registry   string `json:"registry"` // URL
	Email      string `json:"email"`
	ConfigPath string `json:"config_path"` // Path to config.json (default ~/.docker/config.json)
	State      string `json:"state"`       // present, absent (logout)
	Relogin    bool   `json:"relogin"`     // Force re-login even if credentials match
	Validate   bool   `json:"validate"`    // Validate credentials via registry ping (default: true)
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	Token    string `json:"token,omitempty"`
	Registry string `json:"registry,omitempty"` // The registry URL used
}
