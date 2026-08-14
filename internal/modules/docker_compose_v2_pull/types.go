package docker_compose_v2_pull

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs
	docker.ComposeCommonArgs

	Policy             string   `json:"policy"`
	IgnoreBuildable    bool     `json:"ignore_buildable"`
	IgnorePullFailures bool     `json:"ignore_pull_failures"`
	IncludeDeps        bool     `json:"include_deps"`
	Services           []string `json:"services"`
	DockerCLI          string   `json:"docker_cli"`
}

type Response struct {
	Changed  bool                   `json:"changed"`
	Failed   bool                   `json:"failed"`
	Msg      string                 `json:"msg,omitempty"`
	Stdout   string                 `json:"stdout,omitempty"`
	Stderr   string                 `json:"stderr,omitempty"`
	Actions  []docker.ComposeAction `json:"actions"`
	Cmd      string                 `json:"cmd,omitempty"`
	RC       int                    `json:"rc,omitempty"`
	Warnings []string               `json:"warnings,omitempty"`
}
