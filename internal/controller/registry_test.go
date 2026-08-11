package controller

import (
	"encoding/json"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/registry"
)

func TestRenderRegisteredModuleUsesCanonicalNameAndTypedArguments(t *testing.T) {
	invocation, err := registry.Decode("docker_container_info", json.RawMessage(`{"name":"{{ container_name }}"}`))
	if err != nil {
		t.Fatal(err)
	}

	request, err := renderRegisteredModule(invocation, map[string]interface{}{"container_name": "web"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Module != "community.docker.docker_container_info" {
		t.Fatalf("module = %q", request.Module)
	}
	arguments, ok := request.Args.(map[string]interface{})
	if !ok {
		t.Fatalf("arguments type = %T", request.Args)
	}
	if arguments["name"] != "web" {
		t.Fatalf("name = %#v", arguments["name"])
	}
}
