package docker_compose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type PullPolicy string
type BuildPolicy string
type ScaleMap map[string]int

func (policy *PullPolicy) UnmarshalJSON(data []byte) error {
	value, err := unmarshalPolicy(data)
	if err != nil {
		return err
	}
	*policy = PullPolicy(value)
	return nil
}

func (policy *BuildPolicy) UnmarshalJSON(data []byte) error {
	value, err := unmarshalPolicy(data)
	if err != nil {
		return err
	}
	*policy = BuildPolicy(value)
	return nil
}

func unmarshalPolicy(data []byte) (string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return "", nil
	}
	if bytes.Equal(data, []byte("true")) {
		return "always", nil
	}
	if bytes.Equal(data, []byte("false")) {
		return "policy", nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("must be a string or boolean")
	}
	return value, nil
}

func (scale *ScaleMap) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*scale = nil
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("scale must be a dictionary")
	}
	result := make(ScaleMap, len(raw))
	for key, value := range raw {
		count, err := intFromScaleValue(value)
		if err != nil {
			return fmt.Errorf("The value %v for `scale[%q]` is not an integer", value, key)
		}
		if count < 0 {
			return fmt.Errorf("The value %d for `scale[%q]` is negative", count, key)
		}
		result[key] = count
	}
	*scale = result
	return nil
}

func intFromScaleValue(value any) (int, error) {
	switch typed := value.(type) {
	case float64:
		if typed != float64(int(typed)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int(typed), nil
	case string:
		return strconv.Atoi(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, err
		}
		return int(parsed), nil
	case int:
		return typed, nil
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

type Request struct {
	docker.CommonArgs
	docker.ComposeCommonArgs

	State             string      `json:"state"`
	Services          []string    `json:"services"`
	Scale             ScaleMap    `json:"scale"`
	Build             BuildPolicy `json:"build"`
	Pull              PullPolicy  `json:"pull"`
	Dependencies      *bool       `json:"dependencies"`
	IgnoreBuildEvents *bool       `json:"ignore_build_events"`
	Recreate          string      `json:"recreate"`
	RenewAnonVolumes  bool        `json:"renew_anon_volumes"`
	RemoveOrphans     bool        `json:"remove_orphans"`
	RemoveVolumes     bool        `json:"remove_volumes"`
	RemoveImages      string      `json:"remove_images"`
	Timeout           *int        `json:"timeout"`
	Wait              bool        `json:"wait"`
	WaitTimeout       *int        `json:"wait_timeout"`
	AssumeYes         bool        `json:"assume_yes"`
	DockerCLI         string      `json:"docker_cli"`
	NoDeps            bool        `json:"no_deps"`
	ForceRecreate     bool        `json:"force_recreate"`
	NoRecreate        bool        `json:"no_recreate"`
}

type Response struct {
	Changed    bool                   `json:"changed"`
	Failed     bool                   `json:"failed"`
	Msg        string                 `json:"msg,omitempty"`
	Stdout     string                 `json:"stdout,omitempty"`
	Stderr     string                 `json:"stderr,omitempty"`
	Actions    []docker.ComposeAction `json:"actions"`
	Containers []map[string]any       `json:"containers,omitempty"`
	Images     []any                  `json:"images,omitempty"`
	Cmd        string                 `json:"cmd,omitempty"`
	RC         int                    `json:"rc,omitempty"`
	Warnings   []string               `json:"warnings,omitempty"`
}
