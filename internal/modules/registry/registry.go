// Package registry contains the authoritative registration and dispatch metadata
// for modules whose playbook and agent wiring is registry-driven.
package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_run"
	"github.com/gjergjiramku/dibra/internal/modules/docker_config"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_copy_into"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_exec"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_host_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_build"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_export"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_load"
	"github.com/gjergjiramku/dibra/internal/modules/docker_login"
	"github.com/gjergjiramku/dibra/internal/modules/docker_network"
	"github.com/gjergjiramku/dibra/internal/modules/docker_network_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_prune"
	"github.com/gjergjiramku/dibra/internal/modules/docker_secret"
	"github.com/gjergjiramku/dibra/internal/modules/docker_stack"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_service"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_service_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_volume"
	"github.com/gjergjiramku/dibra/internal/modules/docker_volume_info"
)

// Support describes the check-mode or diff-mode support advertised by a module.
type Support string

const (
	SupportNone    Support = "none"
	SupportPartial Support = "partial"
	SupportFull    Support = "full"
	SupportNA      Support = "n/a"
)

// Capabilities declares execution modes supported by a module.
type Capabilities struct {
	CheckMode Support
	DiffMode  Support
}

// Sensitivity identifies argument and result paths that must not be logged
// without redaction. Paths use the JSON field names in the invocation envelope.
type Sensitivity struct {
	Arguments []string
	Results   []string
}

// Decoder strictly decodes a JSON argument object into a module's request type.
type Decoder func(json.RawMessage) (any, error)

// Handler invokes a module using the concrete request returned by its Decoder
// and the effective controller execution state.
type Handler func(any, execution.State) (any, error)

// Deprecation describes an accepted alias that should no longer be used.
type Deprecation struct {
	Replacement string
	Message     string
}

// Definition is all registration, decoding, capability, sensitivity, and
// dispatch information for one canonical module.
type Definition struct {
	CanonicalName string
	ShortAliases  []string
	Deprecations  map[string]Deprecation
	Capabilities  Capabilities
	Sensitivity   Sensitivity
	Decoder       Decoder
	Handler       Handler
}

// Invocation is a decoded playbook module ready for rendering by the controller.
type Invocation struct {
	CanonicalName      string
	Arguments          any
	DeprecationWarning string
}

type argumentNormalizer func(map[string]json.RawMessage) error

var definitions = []Definition{
	module("docker_container", Capabilities{SupportPartial, SupportFull}, sensitivity("registry_password"), docker_container.Execute),
	moduleWithAliases("docker_image", []string{"docker_image"}, Capabilities{SupportPartial, SupportNone}, sensitivity("registry_password"), normalizeDockerImageArguments, docker_image.Execute),
	module("docker_network", Capabilities{SupportFull, SupportFull}, sensitivity(), docker_network.Execute),
	module("docker_volume", Capabilities{SupportFull, SupportFull}, sensitivity(), docker_volume.Execute),
	module("docker_prune", Capabilities{SupportNone, SupportNone}, sensitivity(), docker_prune.Execute),
	module("docker_login", Capabilities{SupportFull, SupportNone}, sensitivityWithResults([]string{"password"}, []string{"token"}), docker_login.Execute),
	module("docker_swarm", Capabilities{SupportFull, SupportFull}, sensitivityWithResults([]string{"join_token"}, []string{"join_tokens"}), docker_swarm.Execute),
	module("docker_swarm_service", Capabilities{SupportFull, SupportFull}, sensitivity(), docker_swarm_service.Execute),
	module("docker_node", Capabilities{SupportFull, SupportNone}, sensitivity(), docker_node.Execute),
	moduleWithDeprecatedAlias("docker_compose_v2", "docker_compose", Capabilities{SupportFull, SupportNone}, sensitivity(), docker_compose.Execute),
	module("docker_compose_v2_run", Capabilities{SupportNone, SupportNone}, sensitivity(), docker_compose_v2_run.Execute),
	module("docker_secret", Capabilities{SupportFull, SupportNone}, sensitivity("data"), docker_secret.Execute),
	module("docker_config", Capabilities{SupportFull, SupportNone}, sensitivity("data"), docker_config.Execute),
	module("docker_stack", Capabilities{SupportNone, SupportNone}, sensitivity(), docker_stack.Execute),
	module("docker_container_exec", Capabilities{SupportNone, SupportNone}, sensitivity("stdin"), docker_container_exec.Execute),
	module("docker_container_copy_into", Capabilities{SupportFull, SupportFull}, sensitivity("content"), docker_container_copy_into.Execute),
	module("docker_image_build", Capabilities{SupportFull, SupportNone}, sensitivity("args"), docker_image_build.Execute),
	module("docker_image_load", Capabilities{SupportNone, SupportNone}, sensitivity(), docker_image_load.Execute),
	moduleWithAliases("docker_image_export", []string{"docker_image_export"}, Capabilities{SupportFull, SupportNone}, sensitivity(), normalizeImageExportArguments, docker_image_export.Execute),
	module("docker_container_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_container_info.Execute),
	module("docker_image_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_image_info.Execute),
	module("docker_network_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_network_info.Execute),
	module("docker_volume_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_volume_info.Execute),
	module("docker_host_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_host_info.Execute),
	module("docker_swarm_info", Capabilities{SupportFull, SupportNA}, sensitivityWithResults(nil, []string{"swarm_info.JoinTokens"}), docker_swarm_info.Execute),
	module("docker_swarm_service_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_swarm_service_info.Execute),
	module("docker_node_info", Capabilities{SupportFull, SupportNA}, sensitivity(), docker_node_info.Execute),
}

var definitionsByName = mustIndex(definitions)

func module[Request, Response any](shortName string, capabilities Capabilities, sensitive Sensitivity, handler func(Request) Response) Definition {
	return moduleWithAliases(shortName, []string{shortName}, capabilities, sensitive, nil, handler)
}

func moduleWithAliases[Request, Response any](shortName string, aliases []string, capabilities Capabilities, sensitive Sensitivity, normalizer argumentNormalizer, handler func(Request) Response) Definition {
	canonicalName := "community.docker." + shortName
	return Definition{
		CanonicalName: canonicalName,
		ShortAliases:  aliases,
		Capabilities:  capabilities,
		Sensitivity:   sensitive,
		Decoder: func(data json.RawMessage) (any, error) {
			var request Request
			if err := decodeStrict(data, &request, normalizer); err != nil {
				return nil, fmt.Errorf("decode %s arguments: %w", canonicalName, err)
			}
			return request, nil
		},
		Handler: func(decoded any, _ execution.State) (any, error) {
			request, ok := decoded.(Request)
			if !ok {
				return nil, fmt.Errorf("handler for %s received %T, want its registered request type", canonicalName, decoded)
			}
			return handler(request), nil
		},
	}
}

func moduleWithDeprecatedAlias[Request, Response any](shortName, deprecatedAlias string, capabilities Capabilities, sensitive Sensitivity, handler func(Request) Response) Definition {
	definition := moduleWithAliases(shortName, []string{shortName, deprecatedAlias}, capabilities, sensitive, nil, handler)
	definition.Deprecations = map[string]Deprecation{
		deprecatedAlias: {
			Replacement: shortName,
			Message: fmt.Sprintf(
				"module alias %q is deprecated and will be removed in a future release; use %q or %q instead",
				deprecatedAlias,
				shortName,
				definition.CanonicalName,
			),
		},
	}
	return definition
}

func sensitivity(arguments ...string) Sensitivity {
	return sensitivityWithResults(arguments, nil)
}

func sensitivityWithResults(arguments, results []string) Sensitivity {
	argumentPaths := make([]string, 0, len(arguments)+1)
	seen := make(map[string]bool)
	for _, path := range append([]string{"client_key"}, arguments...) {
		if !seen[path] {
			argumentPaths = append(argumentPaths, path)
			seen[path] = true
		}
	}
	return Sensitivity{Arguments: argumentPaths, Results: append([]string(nil), results...)}
}

func mustIndex(entries []Definition) map[string]int {
	byName := make(map[string]int)
	for index, entry := range entries {
		if entry.CanonicalName == "" || entry.Decoder == nil || entry.Handler == nil {
			panic("module registry contains an incomplete definition")
		}
		names := append([]string{entry.CanonicalName}, entry.ShortAliases...)
		aliases := make(map[string]bool, len(entry.ShortAliases))
		for _, alias := range entry.ShortAliases {
			aliases[alias] = true
		}
		for alias, deprecation := range entry.Deprecations {
			if !aliases[alias] {
				panic(fmt.Sprintf("module registry deprecation %q is not a short alias of %q", alias, entry.CanonicalName))
			}
			if deprecation.Replacement == "" || deprecation.Message == "" {
				panic(fmt.Sprintf("module registry deprecation %q for %q is incomplete", alias, entry.CanonicalName))
			}
		}
		for _, name := range names {
			if name == "" {
				panic("module registry contains an empty name")
			}
			if previous, exists := byName[name]; exists {
				panic(fmt.Sprintf("module registry name %q is shared by %q and %q", name, entries[previous].CanonicalName, entry.CanonicalName))
			}
			byName[name] = index
		}
	}
	return byName
}

func decodeStrict(data json.RawMessage, destination any, normalizer argumentNormalizer) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		data = json.RawMessage(`{}`)
	}
	if normalizer != nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		if fields == nil {
			fields = make(map[string]json.RawMessage)
		}
		if err := normalizer(fields); err != nil {
			return err
		}
		var err error
		data, err = json.Marshal(fields)
		if err != nil {
			return err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func normalizeImageExportArguments(fields map[string]json.RawMessage) error {
	name, hasName := fields["name"]
	_, hasNames := fields["names"]
	if hasName && !hasNames {
		var value string
		if err := json.Unmarshal(name, &value); err != nil {
			return fmt.Errorf("decode name alias: %w", err)
		}
		encoded, err := json.Marshal([]string{value})
		if err != nil {
			return err
		}
		fields["names"] = encoded
	}
	delete(fields, "name")
	return nil
}

func normalizeDockerImageArguments(fields map[string]json.RawMessage) error {
	buildPath, hasBuildPath := fields["build_path"]
	_, hasLegacyBuildPath := fields["build.path"]
	if hasBuildPath && !hasLegacyBuildPath {
		fields["build.path"] = buildPath
	}
	delete(fields, "build_path")
	return nil
}

// Lookup resolves a canonical or short module name.
func Lookup(name string) (Definition, bool) {
	index, ok := definitionsByName[name]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(definitions[index]), true
}

// Names returns every canonical name and accepted short alias.
func Names() []string {
	names := make([]string, 0, len(definitionsByName))
	for name := range definitionsByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Definitions returns a copy of every canonical registry entry.
func Definitions() []Definition {
	entries := make([]Definition, len(definitions))
	for index, entry := range definitions {
		entries[index] = cloneDefinition(entry)
	}
	return entries
}

// Decode resolves name and strictly decodes data using the registered request type.
func Decode(name string, data json.RawMessage) (*Invocation, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown registered module %q", name)
	}
	arguments, err := definition.Decoder(data)
	if err != nil {
		return nil, err
	}
	invocation := &Invocation{CanonicalName: definition.CanonicalName, Arguments: arguments}
	if deprecation, deprecated := definition.Deprecations[name]; deprecated {
		invocation.DeprecationWarning = deprecation.Message
	}
	return invocation, nil
}

// Execute decodes and dispatches one module invocation.
func Execute(name string, data json.RawMessage, state execution.State) (any, error) {
	definition, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown registered module %q", name)
	}
	return executeDefinition(definition, data, state)
}

func executeDefinition(definition Definition, data json.RawMessage, state execution.State) (any, error) {
	arguments, err := definition.Decoder(data)
	if err != nil {
		return nil, err
	}
	return definition.Handler(arguments, state)
}

// ArgumentsMap converts typed decoded arguments to the controller's renderable map.
func ArgumentsMap(invocation *Invocation) (map[string]any, error) {
	if invocation == nil {
		return nil, fmt.Errorf("nil module invocation")
	}
	definition, ok := Lookup(invocation.CanonicalName)
	if !ok {
		return nil, fmt.Errorf("unknown registered module %q", invocation.CanonicalName)
	}
	data, err := json.Marshal(invocation.Arguments)
	if err != nil {
		return nil, fmt.Errorf("marshal %s arguments: %w", definition.CanonicalName, err)
	}
	var arguments map[string]any
	if err := json.Unmarshal(data, &arguments); err != nil {
		return nil, fmt.Errorf("convert %s arguments: %w", definition.CanonicalName, err)
	}
	return arguments, nil
}

func cloneDefinition(definition Definition) Definition {
	definition.ShortAliases = append([]string(nil), definition.ShortAliases...)
	deprecations := make(map[string]Deprecation, len(definition.Deprecations))
	for alias, deprecation := range definition.Deprecations {
		deprecations[alias] = deprecation
	}
	definition.Deprecations = deprecations
	definition.Sensitivity.Arguments = append([]string(nil), definition.Sensitivity.Arguments...)
	definition.Sensitivity.Results = append([]string(nil), definition.Sensitivity.Results...)
	return definition
}

// ShortName returns the unqualified canonical module name.
func (definition Definition) ShortName() string {
	return strings.TrimPrefix(definition.CanonicalName, "community.docker.")
}
