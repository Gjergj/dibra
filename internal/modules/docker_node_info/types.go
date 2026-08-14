package docker_node_info

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type StringList []string

func (values *StringList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*values = nil
		return nil
	}
	if data[0] == '"' {
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
	Self bool       `json:"self"`
}

type Response struct {
	Changed bool             `json:"changed"`
	Failed  bool             `json:"failed"`
	Msg     string           `json:"msg,omitempty"`
	Nodes   []map[string]any `json:"nodes"`
}
