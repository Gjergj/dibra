package docker_volume

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	VolumeName    string            `json:"volume_name"`
	Name          string            `json:"name"`
	State         string            `json:"state"`
	Driver        string            `json:"driver"`
	DriverOptions map[string]string `json:"driver_options"`
	Labels        map[string]string `json:"labels"`
	Recreate      string            `json:"recreate"`

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

func (request Request) argumentProvided(name string) bool {
	return request.providedArguments[name]
}

func (request Request) volumeName() string {
	if request.VolumeName != "" {
		return request.VolumeName
	}
	return request.Name
}

type Diff struct {
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Volume  map[string]any `json:"volume"`
	Diff    *Diff          `json:"diff,omitempty"`
}
