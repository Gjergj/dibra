package docker_stack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

// ComposeList decodes the pinned compose list: path strings or nested dictionaries.
type ComposeList []ComposeEntry

// ComposeEntry is one compose list element.
type ComposeEntry struct {
	Path    string
	Dict    map[string]any
	Invalid string
	raw     json.RawMessage
}

func (list *ComposeList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*list = nil
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("compose must be a list")
	}
	entries := make([]ComposeEntry, 0, len(raw))
	for _, item := range raw {
		entries = append(entries, parseComposeEntry(item))
	}
	*list = entries
	return nil
}

func (list ComposeList) MarshalJSON() ([]byte, error) {
	if list == nil {
		return []byte("null"), nil
	}
	values := make([]json.RawMessage, 0, len(list))
	for _, entry := range list {
		raw, err := entry.MarshalJSON()
		if err != nil {
			return nil, err
		}
		values = append(values, raw)
	}
	return json.Marshal(values)
}

func (entry ComposeEntry) MarshalJSON() ([]byte, error) {
	if len(entry.raw) > 0 {
		return entry.raw, nil
	}
	if entry.Invalid != "" {
		if json.Valid([]byte(entry.Invalid)) {
			return []byte(entry.Invalid), nil
		}
		return json.Marshal(entry.Invalid)
	}
	if entry.Dict != nil {
		return json.Marshal(entry.Dict)
	}
	return json.Marshal(entry.Path)
}

func parseComposeEntry(raw json.RawMessage) ComposeEntry {
	entry := ComposeEntry{raw: append(json.RawMessage(nil), raw...)}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		entry.Invalid = "None"
		return entry
	}
	var path string
	if err := json.Unmarshal(trimmed, &path); err == nil && trimmed[0] == '"' {
		entry.Path = path
		return entry
	}
	var dict map[string]any
	if err := json.Unmarshal(trimmed, &dict); err == nil && trimmed[0] == '{' {
		entry.Dict = dict
		return entry
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		entry.Invalid = strings.TrimSpace(string(trimmed))
		return entry
	}
	entry.Invalid = fmt.Sprint(value)
	return entry
}

type Request struct {
	docker.CommonArgs

	Name                  string      `json:"name"`
	Compose               ComposeList `json:"compose"`
	ComposeFile           string      `json:"compose_file"`
	State                 string      `json:"state"`
	Prune                 bool        `json:"prune"`
	Detach                *bool       `json:"detach"`
	WithRegistryAuth      bool        `json:"with_registry_auth"`
	ResolveImage          string      `json:"resolve_image"`
	AbsentRetries         *int        `json:"absent_retries"`
	AbsentRetriesInterval *int        `json:"absent_retries_interval"`
	DockerCLI             string      `json:"docker_cli"`
}

type Response struct {
	Changed       bool           `json:"changed"`
	Failed        bool           `json:"failed"`
	Msg           string         `json:"msg,omitempty"`
	RC            *int           `json:"rc,omitempty"`
	Stdout        string         `json:"stdout,omitempty"`
	Stderr        string         `json:"stderr,omitempty"`
	StackSpecDiff map[string]any `json:"stack_spec_diff,omitempty"`
}
