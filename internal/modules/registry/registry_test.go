package registry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker_container"
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
	if request.Name != "web" || request.Image != "nginx:alpine" || !request.ValidateCerts {
		t.Fatalf("decoded request = %#v", request)
	}
}

func TestDecodeRejectsUnknownArguments(t *testing.T) {
	_, err := Decode("docker_container", json.RawMessage(`{"name":"web","typo":true}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "typo"`) {
		t.Fatalf("Decode() error = %v", err)
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
		"docker_container": {"registry_password"},
		"docker_image":     {"registry_password"},
		"docker_login":     {"password"},
		"docker_secret":    {"data"},
		"docker_config":    {"data"},
		"docker_swarm":     {"join_token"},
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
