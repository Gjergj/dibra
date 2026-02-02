package docker_compose_v2_run

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs
	docker.ComposeCommonArgs

	Service         string   `json:"service"`
	Argv            []string `json:"argv"`
	Command         string   `json:"command"`
	Build           bool     `json:"build"`
	CapAdd          []string `json:"cap_add"`
	CapDrop         []string `json:"cap_drop"`
	EntryPoint      string   `json:"entrypoint"`
	Interactive     *bool    `json:"interactive"`
	Labels          []string `json:"labels"`
	Name            string   `json:"name"`
	NoDeps          bool     `json:"no_deps"`
	Publish         []string `json:"publish"`
	QuietPull       bool     `json:"quiet_pull"`
	RemoveOrphans   bool     `json:"remove_orphans"`
	Cleanup         bool     `json:"cleanup"` // --rm
	ServicePorts    bool     `json:"service_ports"`
	UseAliases      bool     `json:"use_aliases"`
	Volumes         []string `json:"volumes"`
	Chdir           string   `json:"chdir"` // --workdir
	Detach          bool     `json:"detach"`
	User            string   `json:"user"`
	Stdin           string   `json:"stdin"`
	StdinAddNewline bool     `json:"stdin_add_newline"`
	StripEmptyEnds  bool     `json:"strip_empty_ends"`
	TTY             *bool    `json:"tty"`
}

type Response struct {
	Changed     bool   `json:"changed"`
	Failed      bool   `json:"failed"`
	Msg         string `json:"msg,omitempty"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	RC          int    `json:"rc"`
	ContainerID string `json:"container_id,omitempty"`
}
