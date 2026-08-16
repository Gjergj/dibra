package docker_plugin

import (
	"bytes"
	"encoding/json"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type PluginOptions map[string]any

func (options *PluginOptions) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*options = decoded
	return nil
}

type Request struct {
	docker.CommonArgs

	PluginName    string        `json:"plugin_name"`
	State         string        `json:"state"`
	Alias         string        `json:"alias"`
	PluginOptions PluginOptions `json:"plugin_options"`
	ForceRemove   bool          `json:"force_remove"`
	EnableTimeout int           `json:"enable_timeout"`
}

func (request Request) preferredName() string {
	if request.Alias != "" {
		return request.Alias
	}
	return request.PluginName
}

type Diff struct {
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Plugin  map[string]any `json:"plugin,omitempty"`
	Actions *[]string      `json:"actions,omitempty"`
	Diff    *Diff          `json:"diff,omitempty"`
}
