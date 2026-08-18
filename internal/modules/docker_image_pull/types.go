package docker_image_pull

import (
	"encoding/json"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs
	Name     string `json:"name"`
	Tag      string `json:"tag"`
	Platform string `json:"platform"`
	Pull     string `json:"pull"`

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

type ImageState struct {
	ID     string `json:"id,omitempty"`
	Exists *bool  `json:"exists,omitempty"`
}

type Diff struct {
	Before ImageState `json:"before"`
	After  ImageState `json:"after"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Actions []string       `json:"actions"`
	Image   map[string]any `json:"image"`
	Diff    Diff           `json:"diff"`
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
