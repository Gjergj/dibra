package docker_volume

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

// LabelMap matches community.docker sanitize_labels: strings, integers, and
// null are accepted, while booleans and floating-point values are rejected.
type LabelMap map[string]string

func (labels *LabelMap) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
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
		text, err := volumeLabelString(key, value)
		if err != nil {
			return err
		}
		result[key] = text
	}
	*labels = result
	return nil
}

func volumeLabelString(key string, value any) (string, error) {
	invalid := func(value any) (string, error) {
		return "", fmt.Errorf("The value %v for '%s' of labels is not a string or something than can be safely converted to a string!", value, key)
	}
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool:
		return invalid(typed)
	case json.Number:
		text := typed.String()
		if strings.ContainsAny(text, ".eE") {
			return invalid(text)
		}
		return text, nil
	case float64:
		return invalid(typed)
	default:
		return fmt.Sprint(typed), nil
	}
}

type Request struct {
	docker.CommonArgs

	VolumeName    string            `json:"volume_name"`
	Name          string            `json:"name"`
	State         string            `json:"state"`
	Driver        string            `json:"driver"`
	DriverOptions map[string]string `json:"driver_options"`
	Labels        LabelMap          `json:"labels"`
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
