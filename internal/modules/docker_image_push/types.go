package docker_image_push

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs
	Name string `json:"name"`
	Tag  string `json:"tag"`

	providedArguments map[string]bool
}

func (request *Request) SetProvidedArguments(arguments []string) {
	request.providedArguments = make(map[string]bool, len(arguments))
	for _, argument := range arguments {
		request.providedArguments[argument] = true
	}
}

func (request Request) ProvidedArguments() map[string]bool {
	return request.providedArguments
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Actions []string       `json:"actions"`
	Image   map[string]any `json:"image"`
}
