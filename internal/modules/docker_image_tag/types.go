package docker_image_tag

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs
	Name           string   `json:"name"`
	Tag            string   `json:"tag"`
	Repository     []string `json:"repository"`
	ExistingImages string   `json:"existing_images"`

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
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	ID     string `json:"id,omitempty"`
	Exists *bool  `json:"exists,omitempty"`
}

type DiffImages struct {
	Images []ImageState `json:"images"`
}

type Diff struct {
	Before DiffImages `json:"before"`
	After  DiffImages `json:"after"`
}

type Response struct {
	Changed      bool           `json:"changed"`
	Failed       bool           `json:"failed"`
	Msg          string         `json:"msg,omitempty"`
	Actions      []string       `json:"actions"`
	Image        map[string]any `json:"image"`
	TaggedImages []string       `json:"tagged_images"`
	Diff         Diff           `json:"diff"`
}
