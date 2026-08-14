package docker_node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

// LabelMap decodes the pinned labels dict, converting JSON numbers to strings
// and rejecting bools and floats the way community.docker sanitize_labels does.
type LabelMap map[string]string

func (labels *LabelMap) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*labels = nil
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("labels must be a dictionary: %w", err)
	}
	result := make(map[string]string, len(raw))
	for key, value := range raw {
		text, err := labelValueToString(key, value)
		if err != nil {
			return err
		}
		result[key] = text
	}
	*labels = result
	return nil
}

func labelValueToString(key string, value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool:
		return "", fmt.Errorf("The value %v for %q of labels is not a string or something than can be safely converted to a string!", typed, key)
	case json.Number:
		text := typed.String()
		if strings.ContainsAny(text, ".eE") {
			return "", fmt.Errorf("The value %s for %q of labels is not a string or something than can be safely converted to a string!", text, key)
		}
		return text, nil
	case float64:
		return "", fmt.Errorf("The value %v for %q of labels is not a string or something than can be safely converted to a string!", typed, key)
	default:
		return fmt.Sprint(typed), nil
	}
}

type Request struct {
	docker.CommonArgs

	Hostname       string   `json:"hostname"`
	Self           bool     `json:"self"`
	Availability   string   `json:"availability"`
	Role           string   `json:"role"`
	Labels         LabelMap `json:"labels"`
	LabelsState    string   `json:"labels_state"`
	LabelsToRemove []string `json:"labels_to_remove"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Node    map[string]any `json:"node,omitempty"`
}
