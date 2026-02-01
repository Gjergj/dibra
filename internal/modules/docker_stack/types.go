package docker_stack

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name             string `json:"name"`               // Stack name
	ComposeFile      string `json:"compose_file"`       // Path to compose file (on remote)
	State            string `json:"state"`              // present, absent
	WithRegistryAuth bool   `json:"with_registry_auth"` // Pass registry auth from local client
	Prune            bool   `json:"prune"`              // Prune services no longer defined
	ResolveImage     string `json:"resolve_image"`      // always, changed, never
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
}
