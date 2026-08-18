package docker_image_push

import (
	"encoding/json"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

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

func (response Response) MarshalJSON() ([]byte, error) {
	type responseAlias Response
	if !response.Failed {
		return json.Marshal(responseAlias(response))
	}
	return json.Marshal(struct {
		Changed bool   `json:"changed"`
		Failed  bool   `json:"failed"`
		Msg     string `json:"msg,omitempty"`
	}{Changed: response.Changed, Failed: response.Failed, Msg: response.Msg})
}
