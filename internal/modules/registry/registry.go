// Package registry contains the authoritative registration and dispatch metadata
// for modules whose playbook and agent wiring is registry-driven.
package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/current_container_facts"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_exec"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_pull"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_run"
	"github.com/gjergjiramku/dibra/internal/modules/docker_config"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_copy_into"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_exec"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_context_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_host_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_build"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_export"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_load"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_pull"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_push"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_remove"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_tag"
	"github.com/gjergjiramku/dibra/internal/modules/docker_login"
	"github.com/gjergjiramku/dibra/internal/modules/docker_network"
	"github.com/gjergjiramku/dibra/internal/modules/docker_network_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_plugin"
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
	CanonicalName           string
	ShortAliases            []string
	Deprecations            map[string]Deprecation
	Capabilities            Capabilities
	ImplementedCapabilities Capabilities
	Sensitivity             Sensitivity
	Decoder                 Decoder
	Handler                 Handler
}

// Invocation is a decoded playbook module ready for rendering by the controller.
type Invocation struct {
	CanonicalName      string
	Arguments          any
	DeprecationWarning string
}

type argumentNormalizer func(map[string]json.RawMessage) error

type argumentPresenceSetter interface {
	SetProvidedArguments([]string)
}

type argumentPresence interface {
	ProvidedArguments() map[string]bool
}

var definitions = []Definition{
	stateModule("docker_container", Capabilities{SupportPartial, SupportFull}, sensitivity("registry_password"), normalizeDockerContainerArguments, docker_container.ExecuteWithState),
	stateModule("docker_image", Capabilities{SupportPartial, SupportNone}, sensitivity("build.args", "registry_password"), normalizeDockerImageArguments, docker_image.ExecuteWithState),
	stateModule("docker_network", Capabilities{SupportFull, SupportFull}, sensitivity(), normalizeDockerNetworkArguments, docker_network.ExecuteWithState),
	stateModule("docker_volume", Capabilities{SupportFull, SupportFull}, sensitivity(), normalizeDockerVolumeArguments, docker_volume.ExecuteWithState),
	moduleWithAliases("docker_prune", []string{"docker_prune"}, Capabilities{SupportNone, SupportNone}, sensitivity(), normalizeDockerPruneArguments, docker_prune.Execute),
	stateModule("docker_login", Capabilities{SupportFull, SupportNone}, sensitivityWithResults([]string{"password"}, []string{"token", "login_result.IdentityToken"}), normalizeDockerLoginArguments, docker_login.ExecuteWithState),
	stateModule("docker_plugin", Capabilities{SupportFull, SupportFull}, sensitivity(), normalizeDockerAPIArguments, docker_plugin.ExecuteWithState),
	stateModule("docker_swarm", Capabilities{SupportFull, SupportFull}, sensitivityWithResults([]string{"join_token", "signing_ca_key"}, []string{"swarm_facts.JoinTokens", "swarm_facts.UnlockKey"}), normalizeDockerAPIArguments, docker_swarm.ExecuteWithState),
	stateModule("docker_swarm_service", Capabilities{SupportFull, SupportFull}, sensitivity(), normalizeDockerAPIArguments, docker_swarm_service.ExecuteWithState),
	stateModule("docker_node", Capabilities{SupportFull, SupportNone}, sensitivity(), normalizeDockerAPIArguments, docker_node.ExecuteWithState),
	stateModuleWithDeprecatedAlias("docker_compose_v2", "docker_compose", Capabilities{SupportFull, SupportNone}, sensitivity(), normalizeDockerComposeArguments, docker_compose.ExecuteWithState),
	moduleWithAliases("docker_compose_v2_exec", []string{"docker_compose_v2_exec"}, Capabilities{SupportNone, SupportNone}, sensitivity("stdin"), normalizeDockerAPIArguments, docker_compose_v2_exec.Execute),
	stateModule("docker_compose_v2_pull", Capabilities{SupportFull, SupportNone}, sensitivity(), normalizeDockerAPIArguments, docker_compose_v2_pull.ExecuteWithState),
	moduleWithAliases("docker_compose_v2_run", []string{"docker_compose_v2_run"}, Capabilities{SupportNone, SupportNone}, sensitivity("stdin"), normalizeDockerAPIArguments, docker_compose_v2_run.Execute),
	stateModule("docker_secret", Capabilities{SupportFull, SupportNone}, sensitivity("data"), normalizeDockerAPIArguments, docker_secret.ExecuteWithState),
	stateModule("docker_config", Capabilities{SupportFull, SupportNone}, sensitivity("data"), normalizeDockerAPIArguments, docker_config.ExecuteWithState),
	moduleWithAliases("docker_stack", []string{"docker_stack"}, Capabilities{SupportNone, SupportNone}, sensitivity(), normalizeDockerAPIArguments, docker_stack.Execute),
	moduleWithAliases("docker_container_exec", []string{"docker_container_exec"}, Capabilities{SupportNone, SupportNone}, sensitivity("stdin"), normalizeDockerContainerExecArguments, docker_container_exec.Execute),
	stateModule("docker_container_copy_into", Capabilities{SupportFull, SupportFull}, sensitivity("content"), normalizeDockerAPIArguments, docker_container_copy_into.ExecuteWithState),
	stateModule("docker_image_build", Capabilities{SupportFull, SupportNone}, sensitivity("args", "secrets.value"), normalizeDockerAPIArguments, docker_image_build.ExecuteWithState),
	moduleWithAliases("docker_image_load", []string{"docker_image_load"}, Capabilities{SupportNone, SupportNone}, sensitivity(), normalizeDockerAPIArguments, docker_image_load.Execute),
	stateModule("docker_image_export", Capabilities{SupportFull, SupportNone}, sensitivity(), normalizeImageExportArguments, docker_image_export.ExecuteWithState),
	stateModule("docker_image_pull", Capabilities{SupportPartial, SupportFull}, sensitivity(), normalizeDockerAPIArguments, docker_image_pull.ExecuteWithState),
	moduleWithAliases("docker_image_push", []string{"docker_image_push"}, Capabilities{SupportNone, SupportNone}, sensitivity(), normalizeDockerAPIArguments, docker_image_push.Execute),
	stateModule("docker_image_remove", Capabilities{SupportFull, SupportFull}, sensitivity(), normalizeDockerAPIArguments, docker_image_remove.ExecuteWithState),
	stateModule("docker_image_tag", Capabilities{SupportFull, SupportFull}, sensitivity(), normalizeDockerAPIArguments, docker_image_tag.ExecuteWithState),
	readOnlyModuleWithNormalizer("docker_container_info", sensitivity(), normalizeDockerContainerInfoArguments, docker_container_info.Execute),
	readOnlyModuleWithNormalizer("docker_image_info", sensitivity(), normalizeDockerAPIArguments, docker_image_info.Execute),
	readOnlyModuleWithNormalizer("docker_network_info", sensitivity(), normalizeDockerAPIArguments, docker_network_info.Execute),
	readOnlyModuleWithNormalizer("docker_volume_info", sensitivity(), normalizeDockerAPIArguments, docker_volume_info.Execute),
	readOnlyModuleWithNormalizer("docker_host_info", sensitivity(), normalizeDockerAPIArguments, docker_host_info.Execute),
	readOnlyModuleWithNormalizer("docker_swarm_info", sensitivityWithResults(nil, []string{"swarm_facts.JoinTokens", "swarm_unlock_key"}), normalizeDockerSwarmInfoArguments, docker_swarm_info.Execute),
	readOnlyModuleWithNormalizer("docker_swarm_service_info", sensitivity(), normalizeDockerAPIArguments, docker_swarm_service_info.Execute),
	readOnlyModuleWithNormalizer("docker_node_info", sensitivity(), normalizeDockerAPIArguments, docker_node_info.Execute),
	readOnlyModule("docker_context_info", sensitivity(), docker_context_info.Execute),
	readOnlyModule("current_container_facts", sensitivity(), current_container_facts.Execute),
}

var definitionsByName = mustIndex(definitions)

func stateModule[Request, Response any](shortName string, capabilities Capabilities, sensitive Sensitivity, normalizer argumentNormalizer, handler func(Request, execution.State) Response) Definition {
	definition := moduleWithAliases(shortName, []string{shortName}, capabilities, sensitive, normalizer, func(request Request) Response {
		return handler(request, execution.State{})
	})
	definition.ImplementedCapabilities = capabilities
	definition.Handler = func(decoded any, state execution.State) (any, error) {
		request, ok := decoded.(Request)
		if !ok {
			return nil, fmt.Errorf("handler for %s received %T, want its registered request type", definition.CanonicalName, decoded)
		}
		return handler(request, state), nil
	}
	return definition
}

func readOnlyModule[Request, Response any](shortName string, sensitive Sensitivity, handler func(Request) Response) Definition {
	return readOnlyModuleWithNormalizer(shortName, sensitive, nil, handler)
}

func readOnlyModuleWithNormalizer[Request, Response any](shortName string, sensitive Sensitivity, normalizer argumentNormalizer, handler func(Request) Response) Definition {
	capabilities := Capabilities{SupportFull, SupportNA}
	definition := moduleWithAliases(shortName, []string{shortName}, capabilities, sensitive, normalizer, handler)
	definition.ImplementedCapabilities = capabilities
	return definition
}

func moduleWithAliases[Request, Response any](shortName string, aliases []string, capabilities Capabilities, sensitive Sensitivity, normalizer argumentNormalizer, handler func(Request) Response) Definition {
	canonicalName := "community.docker." + shortName
	return Definition{
		CanonicalName:           canonicalName,
		ShortAliases:            aliases,
		Capabilities:            capabilities,
		ImplementedCapabilities: Capabilities{SupportNone, SupportNone},
		Sensitivity:             sensitive,
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

func stateModuleWithDeprecatedAlias[Request, Response any](shortName, deprecatedAlias string, capabilities Capabilities, sensitive Sensitivity, normalizer argumentNormalizer, handler func(Request, execution.State) Response) Definition {
	definition := stateModule(shortName, capabilities, sensitive, normalizer, handler)
	definition.ShortAliases = []string{shortName, deprecatedAlias}
	definition.Deprecations = composeDeprecation(shortName, deprecatedAlias, definition.CanonicalName)
	return definition
}

func composeDeprecation(shortName, deprecatedAlias, canonicalName string) map[string]Deprecation {
	return map[string]Deprecation{
		deprecatedAlias: {
			Replacement: shortName,
			Message: fmt.Sprintf(
				"module alias %q is deprecated and will be removed in a future release; use %q or %q instead",
				deprecatedAlias,
				shortName,
				canonicalName,
			),
		},
	}
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
	validSupport := map[Support]bool{
		SupportNone: true, SupportPartial: true, SupportFull: true, SupportNA: true,
	}
	for index, entry := range entries {
		if entry.CanonicalName == "" || entry.Decoder == nil || entry.Handler == nil {
			panic("module registry contains an incomplete definition")
		}
		if !validSupport[entry.Capabilities.CheckMode] || !validSupport[entry.Capabilities.DiffMode] ||
			!validSupport[entry.ImplementedCapabilities.CheckMode] || !validSupport[entry.ImplementedCapabilities.DiffMode] {
			panic(fmt.Sprintf("module registry contains invalid capabilities for %q", entry.CanonicalName))
		}
		if !implementationFitsContract(entry.Capabilities.CheckMode, entry.ImplementedCapabilities.CheckMode) ||
			!implementationFitsContract(entry.Capabilities.DiffMode, entry.ImplementedCapabilities.DiffMode) {
			panic(fmt.Sprintf("module registry implementation capabilities exceed the upstream contract for %q", entry.CanonicalName))
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

func implementationFitsContract(contract, implemented Support) bool {
	switch implemented {
	case SupportNone:
		return true
	case SupportPartial:
		return contract == SupportPartial || contract == SupportFull
	case SupportFull, SupportNA:
		return implemented == contract
	default:
		return false
	}
}

// ImplementsCheckMode reports whether Dibra can execute this definition
// without changing the target while check mode is active.
func (definition Definition) ImplementsCheckMode() bool {
	return definition.ImplementedCapabilities.CheckMode == SupportPartial ||
		definition.ImplementedCapabilities.CheckMode == SupportFull
}

func decodeStrict(data json.RawMessage, destination any, normalizer argumentNormalizer) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		data = json.RawMessage(`{}`)
	}
	var provided []string
	if normalizer != nil || implementsArgumentPresence(destination) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		if fields == nil {
			fields = make(map[string]json.RawMessage)
		}
		if normalizer != nil {
			if err := normalizer(fields); err != nil {
				return err
			}
		}
		provided = make([]string, 0, len(fields))
		for name := range fields {
			provided = append(provided, name)
		}
		sort.Strings(provided)
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
	if setter, ok := destination.(argumentPresenceSetter); ok {
		setter.SetProvidedArguments(provided)
	}
	return nil
}

func implementsArgumentPresence(destination any) bool {
	_, ok := destination.(argumentPresenceSetter)
	return ok
}

func normalizeImageExportArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	name, hasName := fields["name"]
	_, hasNames := fields["names"]
	if hasName && hasNames {
		return fmt.Errorf("both names and its alias name are specified")
	}
	if hasName && !hasNames {
		if len(bytes.TrimSpace(name)) > 0 && bytes.TrimSpace(name)[0] == '[' {
			fields["names"] = name
		} else {
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
	}
	delete(fields, "name")
	return nil
}

func normalizeDockerImageArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}

	if err := normalizeArgumentAliases(fields, "force_absent", "force_remove"); err != nil {
		return err
	}
	if err := normalizeArgumentAliases(fields, "force_source", "force_pull"); err != nil {
		return err
	}

	build := map[string]json.RawMessage{}
	if raw, found := fields["build"]; found {
		if err := json.Unmarshal(raw, &build); err != nil {
			return fmt.Errorf("decode build: %w", err)
		}
	}
	if raw, found := fields["build_path"]; found {
		if _, duplicate := build["path"]; duplicate {
			return fmt.Errorf("build_path and build.path are mutually exclusive")
		}
		build["path"] = raw
	}
	if raw, found := fields["dockerfile"]; found {
		if _, duplicate := build["dockerfile"]; duplicate {
			return fmt.Errorf("dockerfile and build.dockerfile are mutually exclusive")
		}
		build["dockerfile"] = raw
	}
	if len(build) > 0 {
		encoded, err := json.Marshal(build)
		if err != nil {
			return fmt.Errorf("encode build: %w", err)
		}
		fields["build"] = encoded
	}
	delete(fields, "build_path")
	delete(fields, "dockerfile")
	return nil
}

func normalizeDockerContainerExecArguments(fields map[string]json.RawMessage) error {
	return normalizeDockerAPIArguments(fields)
}

func normalizeDockerContainerInfoArguments(fields map[string]json.RawMessage) error {
	return normalizeDockerAPIArguments(fields)
}

func normalizeDockerAPIArguments(fields map[string]json.RawMessage) error {
	aliases := map[string][]string{
		"docker_host":    {"docker_url"},
		"api_version":    {"docker_api_version"},
		"ca_path":        {"ca_cert", "tls_ca_cert", "cacert_path"},
		"client_cert":    {"tls_client_cert", "cert_path"},
		"client_key":     {"tls_client_key", "key_path"},
		"validate_certs": {"tls_verify"},
	}
	for canonical, names := range aliases {
		if err := normalizeArgumentAliases(fields, canonical, names...); err != nil {
			return err
		}
	}
	return nil
}

func normalizeDockerNetworkArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	aliases := map[string][]string{
		"name":           {"network_name"},
		"appends":        {"incremental"},
		"connected":      {"containers"},
		"driver_options": {"options"},
	}
	for canonical, names := range aliases {
		if err := normalizeArgumentAliases(fields, canonical, names...); err != nil {
			return err
		}
	}
	return nil
}

func normalizeDockerVolumeArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	return normalizeArgumentAliases(fields, "volume_name", "name")
}

func normalizeDockerLoginArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	if err := normalizeArgumentAliases(fields, "registry_url", "registry", "url"); err != nil {
		return err
	}
	if err := normalizeArgumentAliases(fields, "reauthorize", "reauth", "relogin"); err != nil {
		return err
	}
	return normalizeArgumentAliases(fields, "config_path", "dockercfg_path")
}

func normalizeDockerPruneArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	return normalizeArgumentAliases(fields, "builder_cache", "builder")
}

func normalizeDockerComposeArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	return normalizeArgumentAliases(fields, "timeout", "stop_timeout")
}

func normalizeDockerSwarmInfoArguments(fields map[string]json.RawMessage) error {
	if err := normalizeDockerAPIArguments(fields); err != nil {
		return err
	}
	return normalizeArgumentAliases(fields, "verbose_output", "verbose")
}

func normalizeArgumentAliases(fields map[string]json.RawMessage, canonical string, aliases ...string) error {
	value, found := fields[canonical]
	suppliedName := canonical
	for _, alias := range aliases {
		aliasValue, hasAlias := fields[alias]
		if !hasAlias {
			continue
		}
		if found {
			return fmt.Errorf("both %s and its alias %s are specified", suppliedName, alias)
		}
		value, found, suppliedName = aliasValue, true, alias
	}
	for _, alias := range aliases {
		delete(fields, alias)
	}
	if found {
		fields[canonical] = value
	}
	return nil
}

func normalizeDockerContainerArguments(fields map[string]json.RawMessage) error {
	aliases := map[string]string{
		"cap_add":      "capabilities",
		"security_opt": "security_opts",
		"ports":        "published_ports",
		"forcekill":    "force_kill",
		"log_opt":      "log_options",
		"exposed":      "exposed_ports",
		"expose":       "exposed_ports",
	}
	for alias, canonical := range aliases {
		value, hasAlias := fields[alias]
		_, hasCanonical := fields[canonical]
		if hasAlias && hasCanonical {
			return fmt.Errorf("both %s and its alias %s are specified", canonical, alias)
		}
		if hasAlias {
			fields[canonical] = value
			delete(fields, alias)
		}
	}
	if raw, found := fields["comparisons"]; found {
		var comparisons map[string]string
		if err := json.Unmarshal(raw, &comparisons); err != nil {
			return fmt.Errorf("decode comparisons: %w", err)
		}
		for alias, canonical := range aliases {
			mode, hasAlias := comparisons[alias]
			_, hasCanonical := comparisons[canonical]
			if hasAlias && hasCanonical {
				return fmt.Errorf("both %s and its alias %s are specified in comparisons", canonical, alias)
			}
			if hasAlias {
				comparisons[canonical] = mode
				delete(comparisons, alias)
			}
		}
		encoded, err := json.Marshal(comparisons)
		if err != nil {
			return err
		}
		fields["comparisons"] = encoded
	}
	if value, found := fields["networks_append"]; found {
		var appendNetworks bool
		if err := json.Unmarshal(value, &appendNetworks); err != nil {
			return fmt.Errorf("decode networks_append compatibility alias: %w", err)
		}
		var comparisons map[string]string
		if raw, exists := fields["comparisons"]; exists {
			if err := json.Unmarshal(raw, &comparisons); err != nil {
				return fmt.Errorf("decode comparisons: %w", err)
			}
		}
		if comparisons == nil {
			comparisons = make(map[string]string)
		}
		if _, exists := comparisons["networks"]; exists {
			return fmt.Errorf("networks_append and comparisons.networks cannot both be specified")
		}
		if appendNetworks {
			comparisons["networks"] = "allow_more_present"
			// The historical Dibra option means "connect these networks without
			// replacing existing attachments". The CLI-compatible default also
			// infers network_mode from the first requested network; comparing that
			// inferred value would recreate the container and defeat append mode.
			if _, exists := comparisons["network_mode"]; !exists {
				comparisons["network_mode"] = "ignore"
			}
		} else {
			comparisons["networks"] = "strict"
		}
		encoded, err := json.Marshal(comparisons)
		if err != nil {
			return err
		}
		fields["comparisons"] = encoded
		delete(fields, "networks_append")
	}
	if raw, found := fields["restart_policy"]; found {
		var policy string
		if err := json.Unmarshal(raw, &policy); err == nil && strings.HasPrefix(policy, "on-failure:") {
			parts := strings.SplitN(policy, ":", 2)
			retries, err := strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid restart_policy retry count %q", parts[1])
			}
			fields["restart_policy"] = json.RawMessage(`"on-failure"`)
			if _, exists := fields["restart_retries"]; !exists {
				fields["restart_retries"] = json.RawMessage(strconv.Itoa(retries))
			}
		}
	}
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
	response, err := executeDefinition(definition, data, state)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", definition.CanonicalName, RedactText(name, data, err.Error()))
	}
	result, err := execution.NormalizeResult(response)
	if err != nil {
		return nil, fmt.Errorf("%s returned an invalid result: %w", definition.CanonicalName, err)
	}
	return result, nil
}

func executeDefinition(definition Definition, data json.RawMessage, state execution.State) (any, error) {
	arguments, err := definition.Decoder(data)
	if err != nil {
		return nil, err
	}
	if state.CheckMode && !definition.ImplementsCheckMode() {
		return execution.UnsupportedCheckMode(definition.CanonicalName), nil
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
	if presence, ok := invocation.Arguments.(argumentPresence); ok {
		provided := presence.ProvidedArguments()
		for name := range arguments {
			if !provided[name] {
				delete(arguments, name)
			}
		}
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
