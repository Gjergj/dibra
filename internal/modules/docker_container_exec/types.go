package docker_container_exec

import (
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Container       string         `json:"container"`
	Argv            []string       `json:"argv"`
	Command         string         `json:"command"`
	Chdir           string         `json:"chdir"`
	Detach          bool           `json:"detach"`
	User            string         `json:"user"`
	Privileged      bool           `json:"privileged"`
	Stdin           *string        `json:"stdin"`
	StdinAddNewline *bool          `json:"stdin_add_newline"`
	StripEmptyEnds  *bool          `json:"strip_empty_ends"`
	TTY             bool           `json:"tty"`
	Env             map[string]any `json:"env"`

	providedArguments map[string]bool
}

// SetProvidedArguments records canonical argument keys after registry alias
// normalization so empty command, argv, stdin, and chdir values remain
// distinguishable from omitted values.
func (request *Request) SetProvidedArguments(names []string) {
	request.providedArguments = make(map[string]bool, len(names))
	for _, name := range names {
		request.providedArguments[name] = true
	}
}

// ProvidedArguments exposes argument presence to the controller-side registry
// projection, preventing omitted zero values from becoming supplied options.
func (request Request) ProvidedArguments() map[string]bool {
	return request.providedArguments
}

func (request Request) argumentProvided(name string, directFallback bool) bool {
	if request.providedArguments == nil {
		return directFallback
	}
	return request.providedArguments[name]
}

type Response struct {
	Changed bool    `json:"changed"`
	Failed  bool    `json:"failed"`
	Msg     string  `json:"msg,omitempty"`
	Stdout  *string `json:"stdout,omitempty"`
	Stderr  *string `json:"stderr,omitempty"`
	RC      *int    `json:"rc,omitempty"`
	ExecID  string  `json:"exec_id,omitempty"`
}
