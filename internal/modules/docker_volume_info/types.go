package docker_volume_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name string `json:"name"`

	providedArguments map[string]bool
}

func (request *Request) SetProvidedArguments(names []string) {
	request.providedArguments = make(map[string]bool, len(names))
	for _, name := range names {
		request.providedArguments[name] = true
	}
}

func (request Request) ProvidedArguments() map[string]bool {
	return request.providedArguments
}

func (request Request) nameProvided() bool {
	if request.providedArguments == nil {
		return request.Name != ""
	}
	return request.providedArguments["name"]
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Exists  bool           `json:"exists"`
	Volume  map[string]any `json:"volume"`
}
