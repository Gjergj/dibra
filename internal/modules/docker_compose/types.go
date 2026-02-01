package docker_compose

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	ProjectSrc    string            `json:"project_src"`    // Path to directory containing docker-compose.yml
	ProjectName   string            `json:"project_name"`   // Override project name
	Files         []string          `json:"files"`          // List of filenames relative to project_src
	State         string            `json:"state"`          // present (up), absent (down)
	Services      []string          `json:"services"`       // Specific services to target
	Scale         map[string]int    `json:"scale"`          // Map of service -> replicas
	Build         bool              `json:"build"`          // Build images before starting
	Pull          bool              `json:"pull"`           // Pull images before starting
	RemoveOrphans bool              `json:"remove_orphans"` // Remove containers for services not defined
	Env           map[string]string `json:"env"`            // Environment variables for the process
	Profiles      []string          `json:"profiles"`       // List of profiles to enable
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
}
