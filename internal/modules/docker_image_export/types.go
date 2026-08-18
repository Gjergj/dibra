package docker_image_export

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type StringList []string

func (values *StringList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*values = StringList{value}
		return nil
	}
	var result []string
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("must be a string or list of strings: %w", err)
	}
	*values = result
	return nil
}

type Request struct {
	docker.CommonArgs

	Names    StringList `json:"names"`
	Tag      string     `json:"tag"`
	Path     string     `json:"path"`
	Force    bool       `json:"force"`
	Platform string     `json:"platform"`

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
	Changed bool             `json:"changed"`
	Failed  bool             `json:"failed"`
	Msg     string           `json:"msg,omitempty"`
	Images  []map[string]any `json:"images"`
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
