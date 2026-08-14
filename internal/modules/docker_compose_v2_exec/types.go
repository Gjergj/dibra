package docker_compose_v2_exec

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs
	docker.ComposeCommonArgs

	Service         string         `json:"service"`
	Index           *int           `json:"index"`
	Argv            []string       `json:"argv"`
	Command         *string        `json:"command"`
	Chdir           *string        `json:"chdir"`
	Detach          bool           `json:"detach"`
	User            *string        `json:"user"`
	Stdin           *string        `json:"stdin"`
	StdinAddNewline *bool          `json:"stdin_add_newline"`
	StripEmptyEnds  *bool          `json:"strip_empty_ends"`
	Privileged      bool           `json:"privileged"`
	TTY             *bool          `json:"tty"`
	Env             map[string]any `json:"env"`
	DockerCLI       string         `json:"docker_cli"`
}

type Response struct {
	Changed bool    `json:"changed"`
	Failed  bool    `json:"failed"`
	Msg     string  `json:"msg,omitempty"`
	Stdout  *string `json:"stdout,omitempty"`
	Stderr  *string `json:"stderr,omitempty"`
	RC      *int    `json:"rc,omitempty"`
}
