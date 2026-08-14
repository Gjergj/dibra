package docker_network

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Name              string            `json:"name"`
	State             string            `json:"state"`
	Driver            string            `json:"driver"`
	DriverOptions     map[string]any    `json:"driver_options"`
	Options           map[string]any    `json:"options"`
	Force             bool              `json:"force"`
	Appends           bool              `json:"appends"`
	Connected         ContainerNames    `json:"connected"`
	IPAMDriver        string            `json:"ipam_driver"`
	IPAMDriverOptions map[string]any    `json:"ipam_driver_options"`
	IPAMConfig        []IPAMConfig      `json:"ipam_config"`
	EnableIPv4        *bool             `json:"enable_ipv4"`
	EnableIPv6        *bool             `json:"enable_ipv6"`
	Internal          *bool             `json:"internal"`
	Attachable        *bool             `json:"attachable"`
	Ingress           *bool             `json:"ingress"`
	ConfigOnly        *bool             `json:"config_only"`
	ConfigFrom        string            `json:"config_from"`
	Scope             string            `json:"scope"`
	Labels            map[string]string `json:"labels"`

	providedArguments map[string]bool
}

func (request *Request) SetProvidedArguments(names []string) {
	request.providedArguments = make(map[string]bool, len(names))
	for _, name := range names {
		request.providedArguments[name] = true
	}
}

func (request Request) ProvidedArguments() map[string]bool {
	return request.providedArguments
}

func (request Request) argumentProvided(name string) bool {
	if request.providedArguments == nil {
		return false
	}
	return request.providedArguments[name]
}

// ContainerNames is a list of container names or IDs. Canonical JSON is a
// string list; the older Dibra object form {name, ipv4_address, ...} is
// accepted by extracting name only.
type ContainerNames []string

func (names *ContainerNames) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*names = nil
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("connected must be a list of container names: %w", err)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = bytes.TrimSpace(item)
		if len(item) == 0 || bytes.Equal(item, []byte("null")) {
			continue
		}
		if item[0] == '"' {
			var name string
			if err := json.Unmarshal(item, &name); err != nil {
				return err
			}
			result = append(result, name)
			continue
		}
		var object map[string]any
		if err := json.Unmarshal(item, &object); err != nil {
			return fmt.Errorf("connected entries must be names or objects with name: %w", err)
		}
		name, _ := object["name"].(string)
		if name == "" {
			return fmt.Errorf("connected object entries must include name")
		}
		result = append(result, name)
	}
	*names = result
	return nil
}

type IPAMConfig struct {
	Subnet       string            `json:"subnet"`
	IPRange      string            `json:"iprange"`
	Gateway      string            `json:"gateway"`
	AuxAddresses map[string]string `json:"aux_addresses"`
}

func (config *IPAMConfig) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*config = IPAMConfig{}
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	config.Subnet = unmarshalOptionalString(raw, "subnet")
	config.Gateway = unmarshalOptionalString(raw, "gateway")
	if _, found := raw["iprange"]; found {
		config.IPRange = unmarshalOptionalString(raw, "iprange")
	} else {
		config.IPRange = unmarshalOptionalString(raw, "ip_range")
	}
	auxRaw, found := raw["aux_addresses"]
	if !found {
		auxRaw, found = raw["aux_address"]
	}
	if found && len(bytes.TrimSpace(auxRaw)) > 0 && !bytes.Equal(bytes.TrimSpace(auxRaw), []byte("null")) {
		var aux map[string]any
		if err := json.Unmarshal(auxRaw, &aux); err != nil {
			return fmt.Errorf("aux_addresses must be a mapping: %w", err)
		}
		addresses := make(map[string]string, len(aux))
		for key, value := range aux {
			text, err := stringifyScalar(value)
			if err != nil {
				return fmt.Errorf("aux_addresses.%s: %w", key, err)
			}
			addresses[key] = text
		}
		config.AuxAddresses = addresses
	}
	return nil
}

func unmarshalOptionalString(raw map[string]json.RawMessage, key string) string {
	value, found := raw[key]
	if !found || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return text
	}
	var anyValue any
	if err := json.Unmarshal(value, &anyValue); err != nil {
		return ""
	}
	text, err := stringifyScalar(anyValue)
	if err != nil {
		return ""
	}
	return text
}

func stringifyScalar(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(bytes.Trim(encoded, `"`)), nil
	}
}

type Diff struct {
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Network map[string]any `json:"network"`
	Actions []string       `json:"actions,omitempty"`
	Diff    *Diff          `json:"diff,omitempty"`
}
