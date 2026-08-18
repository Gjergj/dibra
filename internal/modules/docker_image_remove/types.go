package docker_image_remove

import (
	"encoding/json"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs
	Name  string `json:"name"`
	Tag   string `json:"tag"`
	Force bool   `json:"force"`
	Prune bool   `json:"prune"`

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
	Exists  bool      `json:"exists"`
	ID      string    `json:"id,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
	Digests *[]string `json:"digests,omitempty"`
}

type Diff struct {
	Before ImageState `json:"before"`
	After  ImageState `json:"after"`
}

type Response struct {
	Changed  bool           `json:"changed"`
	Failed   bool           `json:"failed"`
	Msg      string         `json:"msg,omitempty"`
	Actions  []string       `json:"actions"`
	Image    map[string]any `json:"image"`
	Deleted  []string       `json:"deleted"`
	Untagged []string       `json:"untagged"`
	Diff     *Diff          `json:"diff,omitempty"`
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
