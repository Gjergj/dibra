package docker_login

import "github.com/gjergjiramku/dibra/internal/modules/docker"

const defaultRegistryURL = "https://index.docker.io/v1/"
const defaultConfigPath = "~/.docker/config.json"

type Request struct {
	docker.CommonArgs

	Username    string `json:"username"`
	Password    string `json:"password"`
	RegistryURL string `json:"registry_url"`
	ConfigPath  string `json:"config_path"`
	State       string `json:"state"`
	Reauthorize bool   `json:"reauthorize"`
}

func (request Request) registryURL() string {
	if request.RegistryURL != "" {
		return request.RegistryURL
	}
	return defaultRegistryURL
}

func (request Request) reauthorize() bool {
	return request.Reauthorize
}

type Response struct {
	Changed     bool           `json:"changed"`
	Failed      bool           `json:"failed"`
	Msg         string         `json:"msg,omitempty"`
	LoginResult map[string]any `json:"login_result,omitempty"`
}
