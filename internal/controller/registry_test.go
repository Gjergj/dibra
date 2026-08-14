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

func TestNormalizeRegisteredResultPreservesConditionalReturnFields(t *testing.T) {
	detached := normalizeRegisteredResult(map[string]interface{}{"container_id": "abc"})
	for _, field := range []string{"rc", "stdout", "stderr", "stdout_lines", "stderr_lines"} {
		if _, found := detached[field]; found {
			t.Errorf("detached result unexpectedly contains %s: %#v", field, detached)
		}
	}

	synchronous := normalizeRegisteredResult(map[string]interface{}{
		"rc": 0, "stdout": "one\ntwo", "stderr": "",
	})
	if _, found := synchronous["rc"]; !found {
		t.Fatal("synchronous result lost rc")
	}
	if lines, ok := synchronous["stdout_lines"].([]interface{}); !ok || len(lines) != 2 {
		t.Fatalf("stdout_lines = %#v", synchronous["stdout_lines"])
	}
	if lines, ok := synchronous["stderr_lines"].([]interface{}); !ok || len(lines) != 0 {
		t.Fatalf("stderr_lines = %#v", synchronous["stderr_lines"])
	}

	trailing := normalizeRegisteredResult(map[string]interface{}{
		"rc": 0, "stdout": "Hello world!\n", "stderr": "Detach worked.\n",
	})
	if lines, ok := trailing["stdout_lines"].([]interface{}); !ok || len(lines) != 1 || lines[0] != "Hello world!" {
		t.Fatalf("trailing stdout_lines = %#v", trailing["stdout_lines"])
	}
	if lines, ok := trailing["stderr_lines"].([]interface{}); !ok || len(lines) != 1 || lines[0] != "Detach worked." {
		t.Fatalf("trailing stderr_lines = %#v", trailing["stderr_lines"])
	}
}
