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

	destProvided    bool
	contextProvided bool
}

func (output *Output) UnmarshalJSON(data []byte) error {
	type outputAlias Output
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded outputAlias
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*output = Output(decoded)
	output.destProvided = jsonFieldIsNonNull(fields, "dest")
	output.contextProvided = jsonFieldIsNonNull(fields, "context")
	return nil
}

func jsonFieldIsNonNull(fields map[string]json.RawMessage, name string) bool {
	raw, found := fields[name]
	return found && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (output Output) hasDest() bool {
	return output.destProvided || output.Dest != ""
}

func (output Output) hasContext() bool {
	return output.contextProvided || output.Context != ""
}

func (output Output) MarshalJSON() ([]byte, error) {
	result := make(map[string]any)
	if output.Type != "" {
		result["type"] = output.Type
	}
	if output.hasDest() {
		result["dest"] = output.Dest
	}
	if output.hasContext() {
		result["context"] = output.Context
	}
	if output.Name != nil {
		result["name"] = output.Name
	}
	if output.Push {
		result["push"] = true
	}
	return json.Marshal(result)
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
	Changed bool           `json:"changed"`
	Failed  bool           `json:"failed"`
	Msg     string         `json:"msg,omitempty"`
	Actions []string       `json:"actions"`
	Image   map[string]any `json:"image"`
	Command []string       `json:"command,omitempty"`
	Stdout  string         `json:"stdout,omitempty"`
	Stderr  string         `json:"stderr,omitempty"`
}

func (response Response) MarshalJSON() ([]byte, error) {
	type responseAlias Response
	if !response.Failed {
		return json.Marshal(responseAlias(response))
	}
	return json.Marshal(struct {
		Changed bool     `json:"changed"`
		Failed  bool     `json:"failed"`
		Msg     string   `json:"msg,omitempty"`
		Command []string `json:"command,omitempty"`
		Stdout  string   `json:"stdout,omitempty"`
		Stderr  string   `json:"stderr,omitempty"`
	}{
		Changed: response.Changed,
		Failed:  response.Failed,
		Msg:     response.Msg,
		Command: response.Command,
		Stdout:  response.Stdout,
		Stderr:  response.Stderr,
	})
}
