package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/execution"
)

func TestRunHandlersDeduplicateUseDefinitionOrderFlushAndRenotify(t *testing.T) {
	temporary := t.TempDir()
	capturedPath := filepath.Join(temporary, "requests.jsonl")
	agentPath := writeHandlerTestAgent(t, temporary, capturedPath)
	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := `
vars:
  service_name: caddy
tasks:
  - name: first change
    ping:
    notify:
      - handler b
      - restart web
  - name: duplicate notification
    ping:
    notify: handler b
  - name: flush handlers
    meta: flush_handlers
  - name: effective unchanged
    ping:
    changed_when: false
    notify: handler b
  - name: notify after flush
    ping:
    notify: handler b
handlers:
  - name: handler a
    listen: restart web
    command:
      cmd: handler-a-{{ service_name }}
  - name: handler b
    command:
      cmd: handler-b
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), RunOptions{
		ConfigPath: playbookPath, Local: true, LocalAgentPath: agentPath, WorkingDir: temporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed {
		t.Fatalf("Run() result = %#v", result)
	}
	requests := readHandlerTestRequests(t, capturedPath)
	got := make([]string, len(requests))
	for index, request := range requests {
		got[index] = request.Module
		if request.Module == "command" {
			var args map[string]interface{}
			if err := json.Unmarshal(request.Args, &args); err != nil {
				t.Fatal(err)
			}
			got[index] += ":" + fmt.Sprint(args["cmd"])
		}
	}
	want := []string{
		"ping", "ping",
		"command:handler-a-caddy", "command:handler-b",
		"ping", "ping",
		"command:handler-b",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("request order = %#v, want %#v", got, want)
	}
}

func TestRunDynamicIncludeHandler(t *testing.T) {
	temporary := t.TempDir()
	capturedPath := filepath.Join(temporary, "requests.jsonl")
	agentPath := writeHandlerTestAgent(t, temporary, capturedPath)
	if err := os.WriteFile(filepath.Join(temporary, "included.yaml"), []byte(`
- name: included handler task
  command:
    cmd: included-command
`), 0o600); err != nil {
		t.Fatal(err)
	}
	playbookPath := filepath.Join(temporary, "playbook.yaml")
	if err := os.WriteFile(playbookPath, []byte(`
tasks:
  - name: notify dynamic include
    ping:
    notify: included handler
handlers:
  - name: included handler
    include_tasks: included.yaml
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), RunOptions{
		ConfigPath: playbookPath, Local: true, LocalAgentPath: agentPath, WorkingDir: temporary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed {
		t.Fatalf("Run() result = %#v", result)
	}
	requests := readHandlerTestRequests(t, capturedPath)
	if len(requests) != 2 || requests[0].Module != "ping" || requests[1].Module != "command" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRunForceHandlersAfterFailure(t *testing.T) {
	temporary := t.TempDir()
	capturedPath := filepath.Join(temporary, "requests.jsonl")
	agentPath := writeHandlerTestAgent(t, temporary, capturedPath)
	playbookPath := filepath.Join(temporary, "playbook.yaml")
	if err := os.WriteFile(playbookPath, []byte(`
tasks:
  - name: queue handler
    ping:
    notify: forced handler
  - name: fail host
    shell:
      cmd: exit 1
handlers:
  - name: forced handler
    command:
      cmd: forced-command
`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(force bool) []execution.ModuleRequest[json.RawMessage] {
		t.Helper()
		if err := os.Remove(capturedPath); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		result, err := Run(context.Background(), RunOptions{
			ConfigPath: playbookPath, Local: true, LocalAgentPath: agentPath,
			WorkingDir: temporary, ForceHandlers: force,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Failed {
			t.Fatalf("Run(force=%t) unexpectedly succeeded", force)
		}
		return readHandlerTestRequests(t, capturedPath)
	}

	withoutForce := run(false)
	if len(withoutForce) != 2 {
		t.Fatalf("handlers ran without force: %#v", withoutForce)
	}
	withForce := run(true)
	if len(withForce) != 3 || withForce[2].Module != "command" {
		t.Fatalf("forced handler requests = %#v", withForce)
	}
}

func writeHandlerTestAgent(t *testing.T, directory, capturedPath string) string {
	t.Helper()
	agentPath := filepath.Join(directory, "agent")
	script := fmt.Sprintf(`#!/bin/sh
request=$(cat)
printf '%%s\n' "$request" >> %q
case "$request" in
  *'"module":"shell"'*) printf '%%s\n' '{"changed":false,"failed":true,"msg":"intentional failure"}' ;;
  *) printf '%%s\n' '{"changed":true,"failed":false}' ;;
esac
`, capturedPath)
	if err := os.WriteFile(agentPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return agentPath
}

func readHandlerTestRequests(t *testing.T, path string) []execution.ModuleRequest[json.RawMessage] {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	requests := make([]execution.ModuleRequest[json.RawMessage], len(lines))
	for index, line := range lines {
		if err := json.Unmarshal([]byte(line), &requests[index]); err != nil {
			t.Fatalf("decode request %d: %v (%s)", index, err, line)
		}
	}
	return requests
}
