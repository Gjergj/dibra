package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DebugParams describes the controller-side debug action.
type DebugParams struct {
	Msg       interface{} `json:"msg,omitempty" yaml:"msg,omitempty"`
	Var       string      `json:"var,omitempty" yaml:"var,omitempty"`
	Verbosity int         `json:"verbosity,omitempty" yaml:"verbosity,omitempty"`
	MsgSet    bool        `json:"-" yaml:"-"`
}

func (p *DebugParams) UnmarshalYAML(node *yaml.Node) error {
	if isNullYAMLNode(node) {
		return nil
	}
	if err := requirePrimitiveMapping("debug", node, "msg", "var", "verbosity"); err != nil {
		return err
	}
	type plain DebugParams
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*p = DebugParams(decoded)
	p.MsgSet = mappingHasKey(node, "msg")
	return nil
}

// FailParams describes the controller-side fail action.
type FailParams struct {
	Msg    interface{} `json:"msg,omitempty" yaml:"msg,omitempty"`
	MsgSet bool        `json:"-" yaml:"-"`
}

func (p *FailParams) UnmarshalYAML(node *yaml.Node) error {
	if isNullYAMLNode(node) {
		return nil
	}
	if err := requirePrimitiveMapping("fail", node, "msg"); err != nil {
		return err
	}
	type plain FailParams
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*p = FailParams(decoded)
	p.MsgSet = mappingHasKey(node, "msg")
	return nil
}

// AssertParams describes the controller-side assert action.
type AssertParams struct {
	That       When        `json:"that,omitempty" yaml:"that,omitempty"`
	FailMsg    interface{} `json:"fail_msg,omitempty" yaml:"fail_msg,omitempty"`
	Msg        interface{} `json:"msg,omitempty" yaml:"msg,omitempty"`
	SuccessMsg interface{} `json:"success_msg,omitempty" yaml:"success_msg,omitempty"`
	Quiet      bool        `json:"quiet,omitempty" yaml:"quiet,omitempty"`
}

func (p *AssertParams) UnmarshalYAML(node *yaml.Node) error {
	if isNullYAMLNode(node) {
		return nil
	}
	if err := requirePrimitiveMapping("assert", node, "that", "fail_msg", "msg", "success_msg", "quiet"); err != nil {
		return err
	}
	type plain AssertParams
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	if mappingHasKey(node, "fail_msg") && mappingHasKey(node, "msg") {
		return fmt.Errorf("assert: fail_msg and msg are aliases and cannot both be supplied")
	}
	*p = AssertParams(decoded)
	return nil
}

// SetFactParams keeps arbitrary fact names separate from the cacheable option.
type SetFactParams struct {
	Facts     map[string]interface{} `json:"facts,omitempty" yaml:"-"`
	Cacheable bool                   `json:"cacheable,omitempty" yaml:"-"`
}

func (p *SetFactParams) UnmarshalYAML(node *yaml.Node) error {
	if isNullYAMLNode(node) {
		p.Facts = map[string]interface{}{}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("set_fact must be a mapping of variable names to values")
	}

	p.Facts = make(map[string]interface{})
	for index := 0; index < len(node.Content); index += 2 {
		keyNode, valueNode := node.Content[index], node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			return fmt.Errorf("set_fact variable names must be strings")
		}
		if keyNode.Value == "cacheable" {
			if err := valueNode.Decode(&p.Cacheable); err != nil {
				return fmt.Errorf("set_fact cacheable: %w", err)
			}
			continue
		}
		var value interface{}
		if err := valueNode.Decode(&value); err != nil {
			return fmt.Errorf("set_fact %q: %w", keyNode.Value, err)
		}
		p.Facts[keyNode.Value] = value
	}
	return nil
}

// IncludeVarsParams is the controller-supported include_vars contract.
type IncludeVarsParams struct {
	File                    string   `json:"file,omitempty" yaml:"file,omitempty"`
	Dir                     string   `json:"dir,omitempty" yaml:"dir,omitempty"`
	Name                    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Depth                   int      `json:"depth,omitempty" yaml:"depth,omitempty"`
	FilesMatching           string   `json:"files_matching,omitempty" yaml:"files_matching,omitempty"`
	IgnoreFiles             []string `json:"ignore_files,omitempty" yaml:"ignore_files,omitempty"`
	Extensions              []string `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	IgnoreUnknownExtensions bool     `json:"ignore_unknown_extensions,omitempty" yaml:"ignore_unknown_extensions,omitempty"`
	HashBehaviour           string   `json:"hash_behaviour,omitempty" yaml:"hash_behaviour,omitempty"`
}

func (p *IncludeVarsParams) UnmarshalYAML(node *yaml.Node) error {
	if isNullYAMLNode(node) {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		if node.Tag != "!!str" {
			return fmt.Errorf("include_vars free-form value must be a string")
		}
		p.File = strings.TrimSuffix(node.Value, "\n")
		return nil
	}
	if err := requirePrimitiveMapping(
		"include_vars",
		node,
		"file",
		"dir",
		"name",
		"depth",
		"files_matching",
		"ignore_files",
		"extensions",
		"ignore_unknown_extensions",
		"hash_behaviour",
	); err != nil {
		return err
	}
	type plain IncludeVarsParams
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*p = IncludeVarsParams(decoded)
	return nil
}

// PauseParams supports timed pauses and safe non-interactive continuation.
// The duration fields remain raw until execution so templated numbers work.
type PauseParams struct {
	Minutes interface{} `json:"minutes,omitempty" yaml:"minutes,omitempty"`
	Seconds interface{} `json:"seconds,omitempty" yaml:"seconds,omitempty"`
	Prompt  string      `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Echo    interface{} `json:"echo,omitempty" yaml:"echo,omitempty"`
}

func (p *PauseParams) UnmarshalYAML(node *yaml.Node) error {
	if isNullYAMLNode(node) {
		return nil
	}
	if err := requirePrimitiveMapping("pause", node, "minutes", "seconds", "prompt", "echo"); err != nil {
		return err
	}
	type plain PauseParams
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	if decoded.Minutes != nil && decoded.Seconds != nil {
		return fmt.Errorf("pause: minutes and seconds are mutually exclusive")
	}
	*p = PauseParams(decoded)
	return nil
}

func requirePrimitiveMapping(action string, node *yaml.Node, allowed ...string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s arguments must be a mapping", action)
	}
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		if _, ok := known[keyNode.Value]; !ok {
			return fmt.Errorf("%s: unknown argument %q", action, keyNode.Value)
		}
	}
	return nil
}

func mappingHasKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return true
		}
	}
	return false
}

func isNullYAMLNode(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}
