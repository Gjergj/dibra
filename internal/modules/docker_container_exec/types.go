package docker_container_exec

import (
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Container       string            `json:"container"`
	Argv            []string          `json:"argv"`
	Command         string            `json:"command"`
	Chdir           string            `json:"chdir"`
	Detach          bool              `json:"detach"`
	User            string            `json:"user"`
	Privileged      bool              `json:"privileged"`
	Stdin           string            `json:"stdin"`
	StdinAddNewline bool              `json:"stdin_add_newline"`
	StripEmptyEnds  bool              `json:"strip_empty_ends"`
	TTY             bool              `json:"tty"`
	Env             map[string]string `json:"env"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
	RC      int    `json:"rc"`
	ExecID  string `json:"exec_id,omitempty"`
}
