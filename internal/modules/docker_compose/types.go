package docker_compose

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs
	docker.ComposeCommonArgs

	State            string         `json:"state"`              // present (up), absent (down)
	Services         []string       `json:"services"`           // Specific services to target
	Scale            map[string]int `json:"scale"`              // Map of service -> replicas
	Build            bool           `json:"build"`              // Build images before starting
	Pull             bool           `json:"pull"`               // Pull images before starting
	RemoveOrphans    bool           `json:"remove_orphans"`     // Remove containers for services not defined
	Recreate         string         `json:"recreate"`           // always (force-recreate), never (no-recreate), "" (auto)
	RemoveVolumes    bool           `json:"remove_volumes"`     // Remove named volumes on down
	RemoveImages     string         `json:"remove_images"`      // all, local - remove images on down
	StopTimeout      int            `json:"stop_timeout"`       // Timeout for stopping containers
	Wait             bool           `json:"wait"`               // Wait for services to be running|healthy
	WaitTimeout      int            `json:"wait_timeout"`       // Wait timeout in seconds
	NoDeps           bool           `json:"no_deps"`            // Don't start linked services
	ForceRecreate    bool           `json:"force_recreate"`     // Deprecated: use recreate=always
	NoRecreate       bool           `json:"no_recreate"`        // Deprecated: use recreate=never
}

type Response struct {
	Changed  bool     `json:"changed"`
	Failed   bool     `json:"failed"`
	Msg      string   `json:"msg,omitempty"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Actions  []string `json:"actions,omitempty"` // List of actions performed (created, started, etc.)
}
