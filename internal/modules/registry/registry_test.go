package registry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_exec"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_pull"
	"github.com/gjergjiramku/dibra/internal/modules/docker_compose_v2_run"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_copy_into"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_exec"
	"github.com/gjergjiramku/dibra/internal/modules/docker_container_info"
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
	"github.com/gjergjiramku/dibra/internal/modules/docker_node"
	"github.com/gjergjiramku/dibra/internal/modules/docker_node_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_prune"
	"github.com/gjergjiramku/dibra/internal/modules/docker_stack"
	"github.com/gjergjiramku/dibra/internal/modules/docker_stack_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_stack_task_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_swarm_info"
	"github.com/gjergjiramku/dibra/internal/modules/docker_volume"
)

func TestDefinitionsAreCompleteAndResolvable(t *testing.T) {
	entries := Definitions()
	if got, want := len(entries), 38; got != want {
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

func TestDockerContainerCopyIntoDecodesCanonicalArgumentsAndAliases(t *testing.T) {
	invocation, err := Decode("community.docker.docker_container_copy_into", json.RawMessage(`{
		"container":"web",
		"content":"",
		"container_path":"tmp/empty",
		"mode":"0600",
		"mode_parse":"modern",
		"local_follow":false,
		"force":false,
		"docker_url":"unix:///explicit.sock",
		"tls_verify":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := invocation.Arguments.(docker_container_copy_into.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if request.Content == nil || *request.Content != "" || request.Path != nil ||
		request.LocalFollow == nil || *request.LocalFollow ||
		request.Force == nil || *request.Force ||
		string(request.Mode) != `"0600"` || request.ModeParse != "modern" {
		t.Fatalf("request = %#v", request)
	}
	if request.DockerHost == nil || *request.DockerHost != "unix:///explicit.sock" ||
		request.ValidateCerts == nil || *request.ValidateCerts {
		t.Fatalf("connection arguments = %#v", request.CommonArgs)
	}
}

func TestDockerContainerCompatibilityAliasesNormalizeToCanonicalContract(t *testing.T) {
	invocation, err := Decode("docker_container", json.RawMessage(`{
		"name":"web",
		"cap_add":["NET_ADMIN"],
		"ports":["8080:80"],
		"security_opt":["no-new-privileges:true"],
		"comparisons":{"cap_add":"strict","ports":"ignore"},
		"networks_append":false,
		"restart_policy":"on-failure:3",
		"ulimits":[{"name":"nofile","soft":1024,"hard":2048}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_container.Request)
	if len(request.Capabilities) != 1 || request.Capabilities[0] != "NET_ADMIN" || len(request.PublishedPorts) != 1 {
		t.Fatalf("aliases were not normalized: %#v", request)
	}
	if request.Comparisons["networks"] != "strict" || request.RestartPolicy != "on-failure" || request.RestartRetries == nil || *request.RestartRetries != 3 {
		t.Fatalf("behavior aliases were not normalized: %#v", request)
	}
	if request.Comparisons["capabilities"] != "strict" || request.Comparisons["published_ports"] != "ignore" {
		t.Fatalf("comparison aliases were not normalized: %#v", request.Comparisons)
	}
	if len(request.Ulimits) != 1 || request.Ulimits[0] != "nofile:1024:2048" {
		t.Fatalf("legacy ulimits were not normalized: %#v", request.Ulimits)
	}
}

func TestDockerContainerComparisonAliasesRejectDuplicates(t *testing.T) {
	_, err := Decode("docker_container", json.RawMessage(`{
		"name":"web",
		"comparisons":{"cap_add":"strict","capabilities":"ignore"}
	}`))
	if err == nil || !strings.Contains(err.Error(), "both capabilities and its alias cap_add") {
		t.Fatalf("duplicate comparison aliases error = %v", err)
	}
}

func TestDockerContainerNetworksAppendAliasPreservesExistingNetworks(t *testing.T) {
	invocation, err := Decode("docker_container", json.RawMessage(`{
		"name":"web",
		"networks":[{"name":"frontend"}],
		"networks_append":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_container.Request)
	if request.Comparisons["networks"] != "allow_more_present" || request.Comparisons["network_mode"] != "ignore" {
		t.Fatalf("networks_append normalization failed: %#v", request.Comparisons)
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

func TestDockerContainerExecAliasesDefaultsAndPresence(t *testing.T) {
	invocation, err := Decode("docker_container_exec", json.RawMessage(`{
		"container":"web",
		"command":"printf 'hello world'",
		"docker_url":"unix:///tmp/docker.sock",
		"docker_api_version":"1.55",
		"tls_verify":false,
		"stdin_add_newline":false,
		"strip_empty_ends":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_container_exec.Request)
	if request.DockerHost == nil || *request.DockerHost != "unix:///tmp/docker.sock" ||
		request.APIVersion == nil || *request.APIVersion != "1.55" ||
		request.ValidateCerts == nil || *request.ValidateCerts {
		t.Fatalf("connection aliases were not normalized: %#v", request.CommonArgs)
	}
	if request.StdinAddNewline == nil || *request.StdinAddNewline ||
		request.StripEmptyEnds == nil || *request.StripEmptyEnds {
		t.Fatalf("explicit false defaults were not preserved: %#v", request)
	}

	arguments, err := ArgumentsMap(invocation)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"container", "command", "docker_host", "api_version", "validate_certs", "stdin_add_newline", "strip_empty_ends"} {
		if _, found := arguments[name]; !found {
			t.Errorf("canonical argument %q was not forwarded: %#v", name, arguments)
		}
	}
	for _, name := range []string{"argv", "stdin", "detach", "tty", "tls", "docker_url", "docker_api_version", "tls_verify"} {
		if _, found := arguments[name]; found {
			t.Errorf("omitted or alias argument %q was forwarded: %#v", name, arguments)
		}
	}
}

func TestDockerContainerExecRejectsDuplicateAliases(t *testing.T) {
	_, err := Decode("docker_container_exec", json.RawMessage(`{
		"container":"web",
		"argv":["true"],
		"ca_cert":"/certs/one.pem",
		"tls_ca_cert":"/certs/two.pem"
	}`))
	if err == nil || !strings.Contains(err.Error(), "both ca_cert and its alias tls_ca_cert") {
		t.Fatalf("duplicate aliases error = %v", err)
	}
}

func TestDockerContainerInfoAliasesAndPresence(t *testing.T) {
	invocation, err := Decode("community.docker.docker_container_info", json.RawMessage(`{
		"name":"",
		"docker_url":"unix:///tmp/docker.sock",
		"docker_api_version":"1.55",
		"ca_cert":"/certs/ca.pem",
		"tls_client_cert":"/certs/cert.pem",
		"tls_client_key":"/certs/key.pem",
		"tls_verify":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_container_info.Request)
	if request.Name != "" ||
		request.DockerHost == nil || *request.DockerHost != "unix:///tmp/docker.sock" ||
		request.APIVersion == nil || *request.APIVersion != "1.55" ||
		request.CAPath == nil || *request.CAPath != "/certs/ca.pem" ||
		request.ClientCert == nil || *request.ClientCert != "/certs/cert.pem" ||
		request.ClientKey == nil || *request.ClientKey != "/certs/key.pem" ||
		request.ValidateCerts == nil || *request.ValidateCerts {
		t.Fatalf("decoded request = %#v", request)
	}

	arguments, err := ArgumentsMap(invocation)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"name", "docker_host", "api_version", "ca_path", "client_cert", "client_key", "validate_certs"} {
		if _, found := arguments[name]; !found {
			t.Errorf("canonical argument %q was not forwarded: %#v", name, arguments)
		}
	}
	for _, name := range []string{"tls", "debug", "timeout", "docker_url", "docker_api_version", "ca_cert", "tls_verify"} {
		if _, found := arguments[name]; found {
			t.Errorf("omitted or alias argument %q was forwarded: %#v", name, arguments)
		}
	}
}

func TestDockerContainerArgumentsMapPreservesExplicitEmptyScalars(t *testing.T) {
	invocation, err := Decode("docker_container", json.RawMessage(`{"name":"web","hostname":"","user":"","network_mode":""}`))
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := ArgumentsMap(invocation)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"name", "hostname", "user", "network_mode"} {
		if _, found := arguments[name]; !found {
			t.Fatalf("explicit argument %q was dropped: %#v", name, arguments)
		}
	}
	for _, name := range []string{"domainname", "working_dir", "privileged"} {
		if _, found := arguments[name]; found {
			t.Fatalf("omitted argument %q was emitted: %#v", name, arguments)
		}
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
		"docker_compose_v2_run",
		json.RawMessage(`{"project_src":"/path/that/must/not/be-accessed","service":"web"}`),
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

func TestModulesWithImplementedCheckModeAreDeclared(t *testing.T) {
	implemented := map[string]bool{
		"docker_container":           true,
		"docker_image":               true,
		"docker_image_build":         true,
		"docker_image_export":        true,
		"docker_image_pull":          true,
		"docker_image_remove":        true,
		"docker_image_tag":           true,
		"docker_network":             true,
		"docker_volume":              true,
		"docker_login":               true,
		"docker_plugin":              true,
		"docker_swarm":               true,
		"docker_swarm_service":       true,
		"docker_node":                true,
		"docker_config":              true,
		"docker_secret":              true,
		"docker_compose_v2":          true,
		"docker_compose_v2_pull":     true,
		"docker_container_copy_into": true,
		"docker_container_info":      true,
		"docker_image_info":          true,
		"docker_network_info":        true,
		"docker_volume_info":         true,
		"docker_host_info":           true,
		"docker_context_info":        true,
		"current_container_facts":    true,
		"docker_swarm_info":          true,
		"docker_swarm_service_info":  true,
		"docker_node_info":           true,
		"docker_stack_info":          true,
		"docker_stack_task_info":     true,
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

	invocation, err = Decode("docker_image_export", json.RawMessage(`{"name":["alpine","busybox"],"path":"/tmp/images.tar"}`))
	if err != nil {
		t.Fatal(err)
	}
	request = invocation.Arguments.(docker_image_export.Request)
	if len(request.Names) != 2 || request.Names[1] != "busybox" {
		t.Fatalf("list-form name alias decoded as %#v", request.Names)
	}

	if _, err := Decode("docker_image_export", json.RawMessage(`{"name":"alpine","names":["busybox"],"path":"/tmp/images.tar"}`)); err == nil {
		t.Fatal("name and names together were accepted")
	}
}

func TestDockerImageBuildCanonicalOptionsDecodeStrictly(t *testing.T) {
	invocation, err := Decode("community.docker.docker_image_build", json.RawMessage(`{
		"name":"example:v1",
		"path":"/src",
		"platform":"linux/amd64",
		"args":{"COUNT":3},
		"secrets":[{"id":"token","type":"value","value":"secret"}],
		"outputs":[{"type":"image","name":"registry.test/example:v1","push":true}],
		"docker_cli":"/usr/local/bin/docker",
		"docker_url":"unix:///run/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := invocation.Arguments.(docker_image_build.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if len(request.Platform) != 1 || request.Platform[0] != "linux/amd64" ||
		len(request.Outputs) != 1 || len(request.Outputs[0].Name) != 1 ||
		request.DockerCLI != "/usr/local/bin/docker" || request.DockerHost == nil ||
		*request.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("request = %#v", request)
	}

	for _, payload := range []string{
		`{"name":"example","path":"/src","secrets":[{"id":"token","type":"value","unknown":true}]}`,
		`{"name":"example","path":"/src","outputs":[{"type":"image","unknown":true}]}`,
	} {
		if _, err := Decode("docker_image_build", json.RawMessage(payload)); err == nil {
			t.Fatalf("Decode(%s) succeeded", payload)
		}
	}
}

func TestDockerImageInfoAndLoadDecodePinnedContracts(t *testing.T) {
	infoInvocation, err := Decode("community.docker.docker_image_info", json.RawMessage(`{
		"name":["alpine","busybox:stable"],
		"docker_url":"unix:///run/docker.sock",
		"docker_api_version":"auto"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	infoRequest, ok := infoInvocation.Arguments.(docker_image_info.Request)
	if !ok || len(infoRequest.Name) != 2 || infoRequest.Name[0] != "alpine" ||
		infoRequest.DockerHost == nil || *infoRequest.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("info request = %#v", infoInvocation.Arguments)
	}
	scalarInvocation, err := Decode("docker_image_info", json.RawMessage(`{"name":"alpine"}`))
	if err != nil {
		t.Fatal(err)
	}
	if request := scalarInvocation.Arguments.(docker_image_info.Request); len(request.Name) != 1 || request.Name[0] != "alpine" {
		t.Fatalf("scalar info request = %#v", request)
	}
	allInvocation, err := Decode("docker_image_info", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if request := allInvocation.Arguments.(docker_image_info.Request); request.Name != nil {
		t.Fatalf("all-images request = %#v", request)
	}

	loadInvocation, err := Decode("community.docker.docker_image_load", json.RawMessage(`{
		"path":"/tmp/images.tar",
		"tls_ca_cert":"/tmp/ca.pem",
		"docker_api_version":"1.52"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	loadRequest, ok := loadInvocation.Arguments.(docker_image_load.Request)
	if !ok || loadRequest.Path != "/tmp/images.tar" || loadRequest.CAPath == nil ||
		*loadRequest.CAPath != "/tmp/ca.pem" || loadRequest.APIVersion == nil ||
		*loadRequest.APIVersion != "1.52" {
		t.Fatalf("load request = %#v", loadInvocation.Arguments)
	}
}

func TestDockerImagePullAndPushDecodePinnedContracts(t *testing.T) {
	pullInvocation, err := Decode("community.docker.docker_image_pull", json.RawMessage(`{
		"name":"registry.test/team/app:v2",
		"tag":"ignored",
		"platform":"linux/amd64",
		"pull":"not_present",
		"docker_url":"unix:///run/docker.sock",
		"docker_api_version":"1.52"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	pullRequest, ok := pullInvocation.Arguments.(docker_image_pull.Request)
	if !ok || pullRequest.Name != "registry.test/team/app:v2" || pullRequest.Platform != "linux/amd64" ||
		pullRequest.Pull != "not_present" || pullRequest.DockerHost == nil ||
		*pullRequest.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("pull request = %#v", pullInvocation.Arguments)
	}

	pushInvocation, err := Decode("docker_image_push", json.RawMessage(`{
		"name":"registry.test/team/app",
		"tag":"v1",
		"tls_ca_cert":"/tmp/ca.pem",
		"docker_api_version":"auto"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	pushRequest, ok := pushInvocation.Arguments.(docker_image_push.Request)
	if !ok || pushRequest.Name != "registry.test/team/app" || pushRequest.Tag != "v1" ||
		pushRequest.CAPath == nil || *pushRequest.CAPath != "/tmp/ca.pem" {
		t.Fatalf("push request = %#v", pushInvocation.Arguments)
	}

	for _, module := range []string{"docker_image_pull", "docker_image_push"} {
		if _, err := Decode(module, json.RawMessage(`{"name":"alpine","unknown":true}`)); err == nil {
			t.Fatalf("%s accepted unknown option", module)
		}
	}
}

func TestDockerImageRemoveAndTagDecodePinnedContracts(t *testing.T) {
	removeInvocation, err := Decode("community.docker.docker_image_remove", json.RawMessage(`{
		"name":"example:v1",
		"tag":"ignored",
		"force":true,
		"prune":false,
		"docker_url":"unix:///run/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	removeRequest, ok := removeInvocation.Arguments.(docker_image_remove.Request)
	if !ok || removeRequest.Name != "example:v1" || !removeRequest.Force || removeRequest.Prune ||
		removeRequest.DockerHost == nil || *removeRequest.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("remove request = %#v", removeInvocation.Arguments)
	}
	if !removeRequest.ProvidedArguments()["prune"] {
		t.Fatalf("remove argument presence = %#v", removeRequest.ProvidedArguments())
	}

	tagInvocation, err := Decode("docker_image_tag", json.RawMessage(`{
		"name":"example:v1",
		"tag":"ignored",
		"repository":["target:latest","registry.test/team/target:v2"],
		"existing_images":"keep",
		"tls_ca_cert":"/tmp/ca.pem"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	tagRequest, ok := tagInvocation.Arguments.(docker_image_tag.Request)
	if !ok || len(tagRequest.Repository) != 2 || tagRequest.ExistingImages != "keep" ||
		tagRequest.CAPath == nil || *tagRequest.CAPath != "/tmp/ca.pem" {
		t.Fatalf("tag request = %#v", tagInvocation.Arguments)
	}

	for _, module := range []string{"docker_image_remove", "docker_image_tag"} {
		if _, err := Decode(module, json.RawMessage(`{"name":"alpine","unknown":true}`)); err == nil {
			t.Fatalf("%s accepted unknown option", module)
		}
	}
	if _, err := Decode("docker_image_tag", json.RawMessage(`{"name":"alpine","repository":"target"}`)); err == nil {
		t.Fatal("docker_image_tag accepted scalar repository")
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
	if request.Build == nil || request.Build.Path != "/src" {
		t.Fatalf("build = %#v", request.Build)
	}
}

func TestDockerImageCanonicalNestedOptionsAndPresence(t *testing.T) {
	invocation, err := Decode("community.docker.docker_image", json.RawMessage(`{
		"name":"example",
		"source":"build",
		"build":{
			"path":"/src",
			"dockerfile":"Containerfile",
			"cache_from":["base:latest"],
			"container_limits":{"memory":"7MB","memswap":"unlimited","cpushares":512,"cpusetcpus":"0-1"},
			"etc_hosts":{"host.test":"host-gateway"},
			"args":{"NUMBER":42},
			"pull":false,
			"rm":false,
			"platform":"linux/amd64",
			"labels":{"parity":"verified"}
		},
		"archive_path":"/tmp/image.tar",
		"force_absent":true,
		"force_source":true,
		"force_tag":true,
		"pull":{"platform":"linux/amd64"},
		"push":true,
		"repository":"registry.test/example:v1",
		"state":"present",
		"tag":"v1",
		"docker_url":"unix:///run/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := invocation.Arguments.(docker_image.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if request.Build == nil || request.Build.Path != "/src" || request.Build.Dockerfile != "Containerfile" ||
		request.Build.ContainerLimits == nil || request.Build.ContainerLimits.MemorySwap != "unlimited" ||
		request.Pull == nil || request.Pull.Platform != "linux/amd64" {
		t.Fatalf("request = %#v", request)
	}
	if request.Build.Pull == nil || *request.Build.Pull || request.Build.Remove == nil || *request.Build.Remove {
		t.Fatalf("explicit false values were not preserved: %#v", request.Build)
	}
	if request.DockerHost == nil || *request.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("docker_host = %#v", request.DockerHost)
	}
	for _, argument := range []string{"build", "archive_path", "force_source", "pull", "repository"} {
		if !request.ProvidedArguments()[argument] {
			t.Errorf("provided arguments = %#v, missing %q", request.ProvidedArguments(), argument)
		}
	}
}

func TestDockerImageRejectsUnknownNestedAndDuplicateCompatibilityOptions(t *testing.T) {
	for _, input := range []string{
		`{"name":"example","source":"build","build":{"path":"/src","unknown":true}}`,
		`{"name":"example","source":"build","build_path":"/src","build":{"path":"/other"}}`,
		`{"name":"example","source":"build","dockerfile":"Dockerfile","build":{"path":"/src","dockerfile":"Containerfile"}}`,
	} {
		if _, err := Decode("docker_image", json.RawMessage(input)); err == nil {
			t.Fatalf("Decode(%s) unexpectedly succeeded", input)
		}
	}
}

func TestSensitiveArgumentsAreDeclared(t *testing.T) {
	tests := map[string][]string{
		"docker_container":           {"registry_password"},
		"docker_image":               {"build.args", "registry_password"},
		"docker_login":               {"password"},
		"docker_secret":              {"data"},
		"docker_config":              {"data"},
		"docker_swarm":               {"join_token", "signing_ca_key"},
		"docker_container_exec":      {"stdin"},
		"docker_compose_v2_run":      {"stdin"},
		"docker_compose_v2_exec":     {"stdin"},
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
		"docker_login":      {"token", "login_result.IdentityToken"},
		"docker_swarm":      {"swarm_facts.JoinTokens", "swarm_facts.UnlockKey"},
		"docker_swarm_info": {"swarm_facts.JoinTokens", "swarm_unlock_key"},
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
		"swarm_facts": map[string]any{
			"JoinTokens": map[string]any{"Worker": "worker-token", "Manager": "manager-token"},
		},
		"swarm_unlock_key": "SWMKEY-1-secret",
	}
	redacted, err := RedactResult("docker_swarm_info", map[string]any{}, result)
	if err != nil {
		t.Fatal(err)
	}
	facts := redacted["swarm_facts"].(map[string]any)
	if facts["JoinTokens"] != execution.RedactedValue {
		t.Fatalf("join tokens = %#v", facts["JoinTokens"])
	}
	if redacted["swarm_unlock_key"] != execution.RedactedValue {
		t.Fatalf("unlock key = %#v", redacted["swarm_unlock_key"])
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
		"docker_plugin":              {SupportFull, SupportFull},
		"docker_swarm":               {SupportFull, SupportFull},
		"docker_swarm_service":       {SupportFull, SupportFull},
		"docker_node":                {SupportFull, SupportNone},
		"docker_compose_v2":          {SupportFull, SupportNone},
		"docker_compose_v2_pull":     {SupportFull, SupportNone},
		"docker_compose_v2_run":      {SupportNone, SupportNone},
		"docker_compose_v2_exec":     {SupportNone, SupportNone},
		"docker_secret":              {SupportFull, SupportNone},
		"docker_config":              {SupportFull, SupportNone},
		"docker_stack":               {SupportNone, SupportNone},
		"docker_container_exec":      {SupportNone, SupportNone},
		"docker_container_copy_into": {SupportFull, SupportFull},
		"docker_image_build":         {SupportFull, SupportNone},
		"docker_image_load":          {SupportNone, SupportNone},
		"docker_image_export":        {SupportFull, SupportNone},
		"docker_image_pull":          {SupportPartial, SupportFull},
		"docker_image_push":          {SupportNone, SupportNone},
		"docker_image_remove":        {SupportFull, SupportFull},
		"docker_image_tag":           {SupportFull, SupportFull},
		"docker_container_info":      {SupportFull, SupportNA},
		"docker_image_info":          {SupportFull, SupportNA},
		"docker_network_info":        {SupportFull, SupportNA},
		"docker_volume_info":         {SupportFull, SupportNA},
		"docker_host_info":           {SupportFull, SupportNA},
		"docker_context_info":        {SupportFull, SupportNA},
		"current_container_facts":    {SupportFull, SupportNA},
		"docker_swarm_info":          {SupportFull, SupportNA},
		"docker_swarm_service_info":  {SupportFull, SupportNA},
		"docker_node_info":           {SupportFull, SupportNA},
		"docker_stack_info":          {SupportFull, SupportNA},
		"docker_stack_task_info":     {SupportFull, SupportNA},
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

func TestMilestone6AliasesDecodeToCanonicalFields(t *testing.T) {
	networkInvocation, err := Decode("docker_network", json.RawMessage(`{
		"network_name":"app-net",
		"incremental":true,
		"containers":["web"],
		"options":{"com.docker.network.bridge.enable_icc":false}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	network := networkInvocation.Arguments.(docker_network.Request)
	if network.Name != "app-net" || !network.Appends || len(network.Connected) != 1 || network.DriverOptions["com.docker.network.bridge.enable_icc"] != false {
		t.Fatalf("network = %#v", network)
	}

	volumeInvocation, err := Decode("docker_volume", json.RawMessage(`{"name":"data","recreate":"never"}`))
	if err != nil {
		t.Fatal(err)
	}
	volume := volumeInvocation.Arguments.(docker_volume.Request)
	if volume.VolumeName != "data" || volume.Name != "" {
		t.Fatalf("volume = %#v", volume)
	}

	loginInvocation, err := Decode("docker_login", json.RawMessage(`{
		"username":"alice",
		"password":"secret",
		"registry":"localhost:5000",
		"relogin":true,
		"dockercfg_path":"/tmp/config.json"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	login := loginInvocation.Arguments.(docker_login.Request)
	if login.RegistryURL != "localhost:5000" || !login.Reauthorize || login.ConfigPath != "/tmp/config.json" {
		t.Fatalf("login = %#v", login)
	}

	pruneInvocation, err := Decode("docker_prune", json.RawMessage(`{"builder":true,"builder_cache_keep_storage":"1MB"}`))
	if err != nil {
		t.Fatal(err)
	}
	prune := pruneInvocation.Arguments.(docker_prune.Request)
	if !prune.BuilderCache || prune.BuilderCacheKeepStorage != "1MB" {
		t.Fatalf("prune = %#v", prune)
	}

	if _, err := Decode("docker_network", json.RawMessage(`{"name":"a","network_name":"b"}`)); err == nil {
		t.Fatal("expected duplicate alias error")
	}
	if _, err := Decode("docker_context_info", json.RawMessage(`{"docker_host":"unix:///var/run/docker.sock"}`)); err == nil {
		t.Fatal("expected unknown connection argument")
	}

	composeInvocation, err := Decode("docker_compose_v2", json.RawMessage(`{
		"project_src":"/srv/app",
		"stop_timeout":15,
		"build":true,
		"pull":false,
		"docker_url":"unix:///tmp/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	compose := composeInvocation.Arguments.(docker_compose.Request)
	if compose.ProjectSrc != "/srv/app" || compose.Timeout == nil || *compose.Timeout != 15 {
		t.Fatalf("compose timeout = %#v", compose)
	}
	if compose.Build != "always" || compose.Pull != "policy" {
		t.Fatalf("compose policies = build %q pull %q", compose.Build, compose.Pull)
	}
	if compose.DockerHost == nil || *compose.DockerHost != "unix:///tmp/docker.sock" {
		t.Fatalf("compose docker_host = %#v", compose.DockerHost)
	}

	pullInvocation, err := Decode("docker_compose_v2_pull", json.RawMessage(`{
		"project_src":"/srv/flask",
		"policy":"missing",
		"ignore_pull_failures":true,
		"docker_api_version":"1.44"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	pull := pullInvocation.Arguments.(docker_compose_v2_pull.Request)
	if pull.ProjectSrc != "/srv/flask" || pull.Policy != "missing" || !pull.IgnorePullFailures {
		t.Fatalf("pull = %#v", pull)
	}
	if pull.APIVersion == nil || *pull.APIVersion != "1.44" {
		t.Fatalf("pull api_version = %#v", pull.APIVersion)
	}

	runInvocation, err := Decode("docker_compose_v2_run", json.RawMessage(`{
		"project_src":"/srv/flask",
		"service":"web",
		"command":"echo ok",
		"env":{"BOOL_STRING":"true"},
		"docker_url":"unix:///tmp/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	run := runInvocation.Arguments.(docker_compose_v2_run.Request)
	if run.ProjectSrc != "/srv/flask" || run.Service != "web" || run.Command == nil || *run.Command != "echo ok" {
		t.Fatalf("run = %#v", run)
	}
	if run.Env["BOOL_STRING"] != "true" || run.DockerHost == nil || *run.DockerHost != "unix:///tmp/docker.sock" {
		t.Fatalf("run options = %#v", run)
	}

	execInvocation, err := Decode("docker_compose_v2_exec", json.RawMessage(`{
		"project_src":"/srv/flask",
		"service":"web",
		"argv":["id"],
		"index":2,
		"privileged":true,
		"docker_url":"unix:///tmp/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	execRequest := execInvocation.Arguments.(docker_compose_v2_exec.Request)
	if execRequest.ProjectSrc != "/srv/flask" || execRequest.Service != "web" || execRequest.Index == nil || *execRequest.Index != 2 || !execRequest.Privileged {
		t.Fatalf("exec = %#v", execRequest)
	}
	if execRequest.DockerHost == nil || *execRequest.DockerHost != "unix:///tmp/docker.sock" {
		t.Fatalf("exec docker_host = %#v", execRequest.DockerHost)
	}
}

func TestDockerSwarmInfoVerboseAliasAndFiltersDecode(t *testing.T) {
	invocation, err := Decode("docker_swarm_info", json.RawMessage(`{
		"nodes":true,
		"verbose":true,
		"nodes_filters":{"name":"manager-1"},
		"services":true,
		"services_filters":{"label":["env=test"]},
		"unlock_key":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_swarm_info.Request)
	if !request.Nodes || !request.VerboseOutput || !request.Services || !request.UnlockKey {
		t.Fatalf("request = %#v", request)
	}
	if got := request.NodesFilters["name"]; len(got) != 1 || got[0] != "manager-1" {
		t.Fatalf("nodes_filters = %#v", request.NodesFilters)
	}
	if got := request.ServicesFilters["label"]; len(got) != 1 || got[0] != "env=test" {
		t.Fatalf("services_filters = %#v", request.ServicesFilters)
	}
}

func TestDockerNodeAndNodeInfoDecode(t *testing.T) {
	nodeInvocation, err := Decode("community.docker.docker_node", json.RawMessage(`{
		"hostname":"testhost",
		"availability":"drain",
		"labels":{"count":1,"tier":"frontend"},
		"labels_state":"merge",
		"labels_to_remove":["old"],
		"docker_url":"unix:///run/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	node := nodeInvocation.Arguments.(docker_node.Request)
	if node.Hostname != "testhost" || node.Availability != "drain" || node.Labels["count"] != "1" || node.Labels["tier"] != "frontend" {
		t.Fatalf("node = %#v", node)
	}
	if node.LabelsState != "merge" || len(node.LabelsToRemove) != 1 || node.DockerHost == nil || *node.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("node options = %#v", node)
	}

	listInvocation, err := Decode("docker_node_info", json.RawMessage(`{"name":["testhost","missing"],"docker_url":"unix:///run/docker.sock"}`))
	if err != nil {
		t.Fatal(err)
	}
	list := listInvocation.Arguments.(docker_node_info.Request)
	if len(list.Name) != 2 || list.Name[0] != "testhost" || list.DockerHost == nil || *list.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("list = %#v", list)
	}

	scalarInvocation, err := Decode("community.docker.docker_node_info", json.RawMessage(`{"name":"testhost","self":false}`))
	if err != nil {
		t.Fatal(err)
	}
	scalar := scalarInvocation.Arguments.(docker_node_info.Request)
	if len(scalar.Name) != 1 || scalar.Name[0] != "testhost" || scalar.Self {
		t.Fatalf("scalar = %#v", scalar)
	}
}

func TestDockerStackDecode(t *testing.T) {
	invocation, err := Decode("community.docker.docker_stack", json.RawMessage(`{
		"name":"mystack",
		"compose":["/opt/stack.yml",{"version":"3","services":{"web":{"image":"nginx:latest"}}}],
		"prune":true,
		"detach":false,
		"with_registry_auth":true,
		"resolve_image":"never",
		"absent_retries":30,
		"absent_retries_interval":2,
		"docker_cli":"/usr/bin/docker",
		"docker_url":"unix:///run/docker.sock"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_stack.Request)
	if request.Name != "mystack" || len(request.Compose) != 2 || request.Compose[0].Path != "/opt/stack.yml" {
		t.Fatalf("request = %#v", request)
	}
	if request.Compose[1].Dict["version"] != "3" || !request.Prune || request.Detach == nil || *request.Detach {
		t.Fatalf("compose/detach = %#v", request)
	}
	if !request.WithRegistryAuth || request.ResolveImage != "never" || request.DockerCLI != "/usr/bin/docker" {
		t.Fatalf("flags = %#v", request)
	}
	if request.AbsentRetries == nil || *request.AbsentRetries != 30 || request.AbsentRetriesInterval == nil || *request.AbsentRetriesInterval != 2 {
		t.Fatalf("retries = %#v", request)
	}
	if request.DockerHost == nil || *request.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("docker_host = %#v", request.DockerHost)
	}

	aliasInvocation, err := Decode("docker_stack", json.RawMessage(`{"name":"mystack","compose_file":"/opt/stack.yml"}`))
	if err != nil {
		t.Fatal(err)
	}
	alias := aliasInvocation.Arguments.(docker_stack.Request)
	if alias.ComposeFile != "/opt/stack.yml" {
		t.Fatalf("compose_file alias = %#v", alias)
	}
}

func TestDockerStackInfoDecode(t *testing.T) {
	invocation, err := Decode("community.docker.docker_stack_info", json.RawMessage(`{
		"docker_cli":"/usr/bin/docker",
		"docker_url":"unix:///run/docker.sock",
		"api_version":"1.48",
		"tls":true,
		"validate_certs":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_stack_info.Request)
	if request.DockerCLI != "/usr/bin/docker" {
		t.Fatalf("docker_cli = %#v", request.DockerCLI)
	}
	if request.DockerHost == nil || *request.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("docker_host = %#v", request.DockerHost)
	}
	if request.APIVersion == nil || *request.APIVersion != "1.48" || request.TLS == nil || !*request.TLS {
		t.Fatalf("connection = %#v", request)
	}
	if request.ValidateCerts == nil || !*request.ValidateCerts {
		t.Fatalf("validate_certs = %#v", request.ValidateCerts)
	}

	short, err := Decode("docker_stack_info", json.RawMessage(`{"cli_context":"desktop-linux"}`))
	if err != nil {
		t.Fatal(err)
	}
	alias := short.Arguments.(docker_stack_info.Request)
	if alias.CLIContext == nil || *alias.CLIContext != "desktop-linux" {
		t.Fatalf("cli_context = %#v", alias)
	}
}

func TestDockerStackTaskInfoDecode(t *testing.T) {
	invocation, err := Decode("community.docker.docker_stack_task_info", json.RawMessage(`{
		"name":"test_stack",
		"docker_cli":"/usr/bin/docker",
		"docker_url":"unix:///run/docker.sock",
		"api_version":"1.48",
		"tls":true,
		"validate_certs":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.Arguments.(docker_stack_task_info.Request)
	if request.Name != "test_stack" || request.DockerCLI != "/usr/bin/docker" {
		t.Fatalf("request = %#v", request)
	}
	if request.DockerHost == nil || *request.DockerHost != "unix:///run/docker.sock" {
		t.Fatalf("docker_host = %#v", request.DockerHost)
	}
	if request.APIVersion == nil || *request.APIVersion != "1.48" || request.TLS == nil || !*request.TLS {
		t.Fatalf("connection = %#v", request)
	}

	short, err := Decode("docker_stack_task_info", json.RawMessage(`{"name":"web","cli_context":"desktop-linux"}`))
	if err != nil {
		t.Fatal(err)
	}
	alias := short.Arguments.(docker_stack_task_info.Request)
	if alias.Name != "web" || alias.CLIContext == nil || *alias.CLIContext != "desktop-linux" {
		t.Fatalf("short = %#v", alias)
	}
}
