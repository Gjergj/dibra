package docker_image_build

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type StringList []string

type OptionMap map[string]any

func (values *OptionMap) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*values = decoded
	return nil
}

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

type Secret struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Src   string `json:"src"`
	Env   string `json:"env"`
	Value string `json:"value"`
}

type Output struct {
	Type    string     `json:"type"`
	Dest    string     `json:"dest"`
	Context string     `json:"context"`
	Name    StringList `json:"name"`
	Push    bool       `json:"push"`
}

type Request struct {
	docker.CommonArgs

	Name       string     `json:"name"`
	Tag        string     `json:"tag"`
	Path       string     `json:"path"`
	Dockerfile string     `json:"dockerfile"`
	CacheFrom  StringList `json:"cache_from"`
	Pull       bool       `json:"pull"`
	Network    string     `json:"network"`
	NoCache    bool       `json:"nocache"`
	EtcHosts   OptionMap  `json:"etc_hosts"`
	Args       OptionMap  `json:"args"`
	Target     string     `json:"target"`
	Platform   StringList `json:"platform"`
	ShmSize    string     `json:"shm_size"`
	Labels     OptionMap  `json:"labels"`
	Rebuild    string     `json:"rebuild"`
	Secrets    []Secret   `json:"secrets"`
	Outputs    []Output   `json:"outputs"`
	DockerCLI  string     `json:"docker_cli"`
}

type Response struct {
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Actions []string       `json:"actions"`
	Image   map[string]any `json:"image"`
	Command []string       `json:"command,omitempty"`
	Stdout  string         `json:"stdout,omitempty"`
	Stderr  string         `json:"stderr,omitempty"`
}
