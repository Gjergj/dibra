package docker_image_load

import (
	"encoding/json"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Path string `json:"path"` // Path to .tar archive (required)
}

type Response struct {
	Changed    bool             `json:"changed"`
	Failed     bool             `json:"failed"`
	Msg        string           `json:"msg,omitempty"`
	ImageNames []string         `json:"image_names"`
	Images     []map[string]any `json:"images"`
	Stdout     string           `json:"stdout,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}

func (response Response) MarshalJSON() ([]byte, error) {
	type responseAlias Response
	if !response.Failed {
		return json.Marshal(responseAlias(response))
	}
	return json.Marshal(struct {
		Changed  bool     `json:"changed"`
		Failed   bool     `json:"failed"`
		Msg      string   `json:"msg,omitempty"`
		Stdout   string   `json:"stdout,omitempty"`
		Warnings []string `json:"warnings,omitempty"`
	}{
		Changed:  response.Changed,
		Failed:   response.Failed,
		Msg:      response.Msg,
		Stdout:   response.Stdout,
		Warnings: response.Warnings,
	})
}
