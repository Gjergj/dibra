package registry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image"
	"github.com/gjergjiramku/dibra/internal/modules/docker_image_export"
)

func TestDefinitionsAreCompleteAndResolvable(t *testing.T) {
	entries := Definitions()
	if got, want := len(entries), 27; got != want {
		t.Fatalf("Definitions() has %d entries, want %d", got, want)
	}

	validSupport := map[Support]bool{
		SupportNone: true, SupportPartial: true, SupportFull: true, SupportNA: true,
	}
	for _, entry := range entries {
		t.Run(entry.CanonicalName, func(t *testing.T) {
			if !strings.HasPrefix(entry.CanonicalName, "community.docker.") {
				t.Fatalf("canonical name %q is not fully qualified", entry.CanonicalName)
			}
			if entry.Decoder == nil || entry.Handler == nil {
				t.Fatal("decoder and handler must both be registered")
			}
			if !validSupport[entry.Capabilities.CheckMode] || !validSupport[entry.Capabilities.DiffMode] {
				t.Fatalf("invalid capabilities: %#v", entry.Capabilities)
			}
			if !validSupport[entry.ImplementedCapabilities.CheckMode] || !validSupport[entry.ImplementedCapabilities.DiffMode] {
				t.Fatalf("invalid implemented capabilities: %#v", entry.ImplementedCapabilities)
			}
			if !contains(entry.Sensitivity.Arguments, "client_key") {
				t.Fatalf("TLS client-key sensitivity is missing: %#v", entry.Sensitivity)
			}

			canonical, ok := Lookup(entry.CanonicalName)
			if !ok || canonical.CanonicalName != entry.CanonicalName {
				t.Fatalf("canonical lookup failed: %#v, %v", canonical, ok)
			}
			if len(entry.ShortAliases) == 0 {
				t.Fatal("at least one short alias is required")
			}
			for _, alias := range entry.ShortAliases {
				resolved, ok := Lookup(alias)
				if !ok || resolved.CanonicalName != entry.CanonicalName {
					t.Fatalf("alias %q resolved to %#v, %v", alias, resolved, ok)
				}
			}
		})
	}
}

func TestDecodeUsesRegisteredConcreteRequestType(t *testing.T) {
	invocation, err := Decode("docker_container", json.RawMessage(`{
		"name":"web",
		"image":"nginx:alpine",
		"registry_password":"secret",
		"validate_certs":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if invocation.CanonicalName != "community.docker.docker_container" {
		t.Fatalf("canonical name = %q", invocation.CanonicalName)
	}
	request, ok := invocation.Arguments.(docker_container.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if request.Name != "web" || request.Image != "nginx:alpine" || request.ValidateCerts == nil || !*request.ValidateCerts {
		t.Fatalf("decoded request = %#v", request)
	}
}

func TestDecodePreservesExplicitFalseDockerConnectionArguments(t *testing.T) {
	explicit, err := Decode("docker_container_info", json.RawMessage(`{"name":"web","tls":false,"validate_certs":false}`))
	if err != nil {
		t.Fatal(err)
	}
	explicitRequest := explicit.Arguments.(docker_container_info.Request)
	if explicitRequest.TLS == nil || *explicitRequest.TLS || explicitRequest.ValidateCerts == nil || *explicitRequest.ValidateCerts {
		t.Fatalf("explicit false values were not preserved: %#v", explicitRequest.CommonArgs)
	}

	omitted, err := Decode("docker_container_info", json.RawMessage(`{"name":"web"}`))
	if err != nil {
		t.Fatal(err)
	}
	omittedRequest := omitted.Arguments.(docker_container_info.Request)
	if omittedRequest.TLS != nil || omittedRequest.ValidateCerts != nil {
		t.Fatalf("omitted values were not preserved: %#v", omittedRequest.CommonArgs)
	}
}

func TestDecodeRejectsUnknownArguments(t *testing.T) {
	_, err := Decode("docker_container", json.RawMessage(`{"name":"web","typo":true}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "typo"`) {
		t.Fatalf("Decode() error = %v", err)
	}
}

func TestExecuteDefinitionPassesExecutionStateToHandler(t *testing.T) {
	want := execution.State{CheckMode: true, DiffMode: true}
	var received execution.State
	definition := Definition{
		ImplementedCapabilities: Capabilities{CheckMode: SupportFull, DiffMode: SupportNone},
		Decoder: func(data json.RawMessage) (any, error) {
			return string(data), nil
		},
		Handler: func(arguments any, state execution.State) (any, error) {
			received = state
			return arguments, nil
		},
	}
	if _, err := executeDefinition(definition, json.RawMessage(`{}`), want); err != nil {
		t.Fatal(err)
	}
	if received != want {
		t.Fatalf("handler state = %#v, want %#v", received, want)
	}
}

func TestCheckModeSkipsModuleWithoutImplementedSupport(t *testing.T) {
	response, err := Execute(
		"docker_compose_v2",
		json.RawMessage(`{"project_src":"/path/that/must/not/be-accessed"}`),
		execution.State{CheckMode: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	skipped, ok := response.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T, want map[string]any", response)
	}
	if skipped["skipped"] != true || skipped["changed"] != false || skipped["failed"] != false || skipped["msg"] == "" {
		t.Fatalf("response = %#v", skipped)
	}
}

func TestOnlyReadOnlyModulesInitiallyImplementCheckMode(t *testing.T) {
	implemented := map[string]bool{
		"docker_container_info":     true,
		"docker_image_info":         true,
		"docker_network_info":       true,
		"docker_volume_info":        true,
		"docker_host_info":          true,
		"docker_swarm_info":         true,
		"docker_swarm_service_info": true,
		"docker_node_info":          true,
	}
	for _, definition := range Definitions() {
		want := implemented[definition.ShortName()]
		if got := definition.ImplementsCheckMode(); got != want {
			t.Errorf("%s ImplementsCheckMode() = %v, want %v", definition.ShortName(), got, want)
		}
	}
}

func TestCompatibilityAliasesResolveToCanonicalModules(t *testing.T) {
	for _, name := range []string{
		"docker_compose_v2",
		"docker_compose",
		"community.docker.docker_compose_v2",
	} {
		definition, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) failed", name)
		}
		if definition.CanonicalName != "community.docker.docker_compose_v2" {
			t.Fatalf("Lookup(%q) canonical name = %q", name, definition.CanonicalName)
		}
	}

	definition, _ := Lookup("docker_compose")
	deprecation, ok := definition.Deprecations["docker_compose"]
	if !ok {
		t.Fatal("docker_compose alias does not carry deprecation metadata")
	}
	if deprecation.Replacement != "docker_compose_v2" || !strings.Contains(deprecation.Message, "deprecated") {
		t.Fatalf("docker_compose deprecation = %#v", deprecation)
	}
}

func TestDecodeWarnsOnlyForDeprecatedComposeAlias(t *testing.T) {
	legacy, err := Decode("docker_compose", json.RawMessage(`{"project_src":"/srv/app"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(legacy.DeprecationWarning, `"docker_compose" is deprecated`) {
		t.Fatalf("legacy deprecation warning = %q", legacy.DeprecationWarning)
	}

	for _, name := range []string{"docker_compose_v2", "community.docker.docker_compose_v2"} {
		invocation, err := Decode(name, json.RawMessage(`{"project_src":"/srv/app"}`))
		if err != nil {
			t.Fatal(err)
		}
		if invocation.DeprecationWarning != "" {
			t.Errorf("Decode(%q) warning = %q", name, invocation.DeprecationWarning)
		}
	}
}

func TestImageExportNameArgumentAliasIsNormalizedByTypedDecoder(t *testing.T) {
	invocation, err := Decode("docker_image_export", json.RawMessage(`{"name":"alpine:latest","path":"/tmp/alpine.tar"}`))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := invocation.Arguments.(docker_image_export.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if len(request.Names) != 1 || request.Names[0] != "alpine:latest" {
		t.Fatalf("names = %#v", request.Names)
	}
}

func TestDockerImageBuildPathCompatibilityAliasIsNormalized(t *testing.T) {
	invocation, err := Decode("docker_image", json.RawMessage(`{"name":"example","source":"build","build_path":"/src"}`))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := invocation.Arguments.(docker_image.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if request.BuildPath != "/src" {
		t.Fatalf("build path = %q", request.BuildPath)
	}
}

func TestSensitiveArgumentsAreDeclared(t *testing.T) {
	tests := map[string][]string{
		"docker_container":           {"registry_password"},
		"docker_image":               {"registry_password"},
		"docker_login":               {"password"},
		"docker_secret":              {"data"},
		"docker_config":              {"data"},
		"docker_swarm":               {"join_token"},
		"docker_container_exec":      {"stdin"},
		"docker_compose_v2_run":      {"stdin"},
		"docker_container_copy_into": {"content"},
		"docker_image_build":         {"args"},
	}
	for moduleName, fields := range tests {
		definition, ok := Lookup(moduleName)
		if !ok {
			t.Fatalf("Lookup(%q) failed", moduleName)
		}
		for _, field := range fields {
			if !contains(definition.Sensitivity.Arguments, field) {
				t.Errorf("%s does not mark %q sensitive: %#v", moduleName, field, definition.Sensitivity.Arguments)
			}
		}
	}
}

func TestSensitiveResultsAreDeclared(t *testing.T) {
	tests := map[string][]string{
		"docker_login":      {"token"},
		"docker_swarm":      {"join_tokens"},
		"docker_swarm_info": {"swarm_info.join_tokens"},
	}
	for moduleName, fields := range tests {
		definition, ok := Lookup(moduleName)
		if !ok {
			t.Fatalf("Lookup(%q) failed", moduleName)
		}
		for _, field := range fields {
			if !contains(definition.Sensitivity.Results, field) {
				t.Errorf("%s does not mark result %q sensitive: %#v", moduleName, field, definition.Sensitivity.Results)
			}
		}
	}
}

func TestRedactArgumentsHidesDeclaredValuesAndEchoes(t *testing.T) {
	arguments := map[string]any{
		"username":   "deploy",
		"password":   "registry-password",
		"client_key": "private-key-material",
		"note":       "registry-password private-key-material",
	}
	redacted, err := RedactArguments("docker_login", arguments)
	if err != nil {
		t.Fatal(err)
	}
	fields := redacted.(map[string]any)
	if fields["username"] != "deploy" {
		t.Fatalf("non-sensitive username was changed: %#v", fields)
	}
	for _, field := range []string{"password", "client_key"} {
		if fields[field] != execution.RedactedValue {
			t.Fatalf("%s = %#v", field, fields[field])
		}
	}
	if strings.Contains(fields["note"].(string), "registry-password") || strings.Contains(fields["note"].(string), "private-key-material") {
		t.Fatalf("echoed sensitive values were not scrubbed: %#v", fields)
	}
	if arguments["password"] != "registry-password" {
		t.Fatalf("original arguments were modified: %#v", arguments)
	}
}

func TestRedactResultHidesSensitiveReturnPathsAndEchoes(t *testing.T) {
	result := map[string]any{
		"changed":  false,
		"failed":   true,
		"msg":      "login failed for registry-password with registry-token",
		"stderr":   "registry-token",
		"token":    "registry-token",
		"registry": "registry.example.test",
	}
	redacted, err := RedactResult(
		"docker_login",
		map[string]any{"password": "registry-password"},
		result,
	)
	if err != nil {
		t.Fatal(err)
	}
	if redacted["token"] != execution.RedactedValue {
		t.Fatalf("token = %#v", redacted["token"])
	}
	for _, field := range []string{"msg", "stderr"} {
		text := redacted[field].(string)
		if strings.Contains(text, "registry-password") || strings.Contains(text, "registry-token") {
			t.Fatalf("%s was not scrubbed: %q", field, text)
		}
	}
	if result["token"] != "registry-token" {
		t.Fatalf("original result was modified: %#v", result)
	}
}

func TestRedactNestedSwarmJoinTokens(t *testing.T) {
	result := map[string]any{
		"changed": false,
		"failed":  false,
		"msg":     "worker-token",
		"swarm_info": map[string]any{
			"join_tokens": map[string]any{"worker": "worker-token", "manager": "manager-token"},
		},
	}
	redacted, err := RedactResult("docker_swarm_info", map[string]any{}, result)
	if err != nil {
		t.Fatal(err)
	}
	swarmInfo := redacted["swarm_info"].(map[string]any)
	if swarmInfo["join_tokens"] != execution.RedactedValue {
		t.Fatalf("join tokens = %#v", swarmInfo["join_tokens"])
	}
	if strings.Contains(redacted["msg"].(string), "worker-token") {
		t.Fatalf("join token echo was not scrubbed: %#v", redacted)
	}
}

func TestCapabilitiesMatchUpstreamDeclarations(t *testing.T) {
	expected := map[string]Capabilities{
		"docker_container":           {SupportPartial, SupportFull},
		"docker_image":               {SupportPartial, SupportNone},
		"docker_network":             {SupportFull, SupportFull},
		"docker_volume":              {SupportFull, SupportFull},
		"docker_prune":               {SupportNone, SupportNone},
		"docker_login":               {SupportFull, SupportNone},
		"docker_swarm":               {SupportFull, SupportFull},
		"docker_swarm_service":       {SupportFull, SupportFull},
		"docker_node":                {SupportFull, SupportNone},
		"docker_compose_v2":          {SupportFull, SupportNone},
		"docker_compose_v2_run":      {SupportNone, SupportNone},
		"docker_secret":              {SupportFull, SupportNone},
		"docker_config":              {SupportFull, SupportNone},
		"docker_stack":               {SupportNone, SupportNone},
		"docker_container_exec":      {SupportNone, SupportNone},
		"docker_container_copy_into": {SupportFull, SupportFull},
		"docker_image_build":         {SupportFull, SupportNone},
		"docker_image_load":          {SupportNone, SupportNone},
		"docker_image_export":        {SupportFull, SupportNone},
		"docker_container_info":      {SupportFull, SupportNA},
		"docker_image_info":          {SupportFull, SupportNA},
		"docker_network_info":        {SupportFull, SupportNA},
		"docker_volume_info":         {SupportFull, SupportNA},
		"docker_host_info":           {SupportFull, SupportNA},
		"docker_swarm_info":          {SupportFull, SupportNA},
		"docker_swarm_service_info":  {SupportFull, SupportNA},
		"docker_node_info":           {SupportFull, SupportNA},
	}
	for name, want := range expected {
		definition, ok := Lookup(name)
		if !ok {
			t.Errorf("Lookup(%q) failed", name)
			continue
		}
		if definition.Capabilities != want {
			t.Errorf("%s capabilities = %#v, want %#v", name, definition.Capabilities, want)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
