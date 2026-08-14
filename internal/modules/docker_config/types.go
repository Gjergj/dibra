package docker_config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

const (
	ansibleKeyLabel     = "ansible_key"
	ansibleVersionLabel = "ansible_version"
	defaultVersionsKeep = 5
)

// LabelMap decodes the pinned labels dict, converting JSON integers to strings
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

	Name            string   `json:"name"`
	Data            *string  `json:"data"`
	DataIsB64       bool     `json:"data_is_b64"`
	DataSrc         string   `json:"data_src"`
	Labels          LabelMap `json:"labels"`
	Force           bool     `json:"force"`
	RollingVersions bool     `json:"rolling_versions"`
	VersionsToKeep  *int     `json:"versions_to_keep"`
	State           string   `json:"state"`
	TemplateDriver  string   `json:"template_driver"`
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Msg        string `json:"msg,omitempty"`
	ConfigID   string `json:"config_id,omitempty"`
	ConfigName string `json:"config_name,omitempty"`
}
