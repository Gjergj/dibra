package docker_network_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

// Request represents the module arguments.
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

// Response represents the module return value. Network is always present and
// marshals as JSON null when the network does not exist.
type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Exists  bool           `json:"exists"`
	Network map[string]any `json:"network"`
}
