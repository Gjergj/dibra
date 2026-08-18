package docker_image_info

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
	Name StringList `json:"name"`
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
