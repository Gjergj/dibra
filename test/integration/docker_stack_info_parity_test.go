//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerStackInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	prefix := fmt.Sprintf("dibra-si-%d", time.Now().UnixNano()%100000000)
	stackName := prefix
	secondName := prefix + "-b"
	twoName := prefix + "-two"
	baseFile := "/tmp/" + prefix + "-base.yml"
	twoFile := "/tmp/" + prefix + "-two.yml"
	customCLI := "/tmp/" + prefix + "-docker"

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-si-*.json /tmp/.dibra-agent "+baseFile+" "+twoFile+" "+customCLI)
	defer func() {
		mustRemote(t, client, "docker stack rm "+stackName+" "+secondName+" "+twoName+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	}()

	templatePath := writeResultTemplate(t, "stack_info_result")
	run := func(t *testing.T, name, arguments, taskOptions string, extra ...string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-si-" + name + ".json"
		playbook := playbookHeader + `
  - name: List docker stacks
    community.docker.docker_stack_info:
` + arguments + `
    register: stack_info_result
` + taskOptions + `

  - name: Persist stack info result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybookWithArgs(t, playbook, extra...)
		if strings.Contains(output, "FAILED") || strings.Contains(output, "Failed to load config") {
			return nil, output
		}
		return readRemoteJSONMap(t, client, remotePath), output
	}
	success := func(t *testing.T, name, arguments, taskOptions string, extra ...string) map[string]any {
		t.Helper()
		result, output := run(t, name, arguments, taskOptions, extra...)
		if result == nil {
			t.Fatalf("%s failed: %s", name, output)
		}
		if result["changed"] != false {
			t.Fatalf("%s changed: %#v", name, result)
		}
		return result
	}
	stackByName := func(result map[string]any, name string) map[string]any {
		t.Helper()
		raw, _ := result["results"].([]any)
		for _, item := range raw {
			entry, _ := item.(map[string]any)
			if entry["Name"] == name {
				return entry
			}
		}
		t.Fatalf("stack %q missing from %#v", name, result["results"])
		return nil
	}

	t.Run("fails when not a swarm manager", func(t *testing.T) {
		_, output := run(t, "not-manager", "", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error response from daemon: This node is not a swarm manager") {
			t.Fatalf("not manager = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")
	mustRemote(t, client, "cat > "+baseFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "cat > "+twoFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\n  extra:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "ln -sf \"$(command -v docker)\" "+customCLI)

	t.Run("empty swarm returns an empty results list", func(t *testing.T) {
		result := success(t, "empty", "", "")
		results, _ := result["results"].([]any)
		if results == nil || len(results) != 0 {
			t.Fatalf("empty results = %#v", result["results"])
		}
		if result["stdout"] != "" {
			t.Fatalf("empty stdout = %#v", result["stdout"])
		}
		if _, found := result["rc"]; !found {
			t.Fatalf("missing rc: %#v", result)
		}
	})

	t.Run("host and cli_context are mutually exclusive", func(t *testing.T) {
		_, output := run(t, "conflict", "      docker_host: unix:///var/run/docker.sock\n      cli_context: default\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "docker_host and cli_context are mutually exclusive") {
			t.Fatalf("conflict = %s", output)
		}
	})

	deploy := func(t *testing.T, name, file string) {
		t.Helper()
		playbook := playbookHeader + `
  - name: Deploy stack
    community.docker.docker_stack:
      name: ` + name + `
      compose:
        - ` + file + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("deploy %s failed: %s", name, output)
		}
	}
	remove := func(t *testing.T, name string) {
		t.Helper()
		playbook := playbookHeader + `
  - name: Remove stack
    community.docker.docker_stack:
      name: ` + name + `
      state: absent
      absent_retries: 30
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("remove %s failed: %s", name, output)
		}
	}

	t.Run("lists created stack with Name and Services", func(t *testing.T) {
		deploy(t, stackName, baseFile)
		result := success(t, "one", "", "")
		entry := stackByName(result, stackName)
		if entry["Services"] != "1" {
			t.Fatalf("services = %#v", entry)
		}
		if orchestrator, found := entry["Orchestrator"]; found && orchestrator != "Swarm" {
			t.Fatalf("orchestrator = %#v", entry)
		}
		for _, key := range []string{"name", "services", "orchestrator", "namespace"} {
			if _, found := entry[key]; found {
				t.Fatalf("snake_case key %q leaked: %#v", key, entry)
			}
		}
	})

	t.Run("canonical name docker_cli and connection alias", func(t *testing.T) {
		result := success(t, "alias", "      docker_cli: "+customCLI+"\n      docker_url: unix:///var/run/docker.sock\n", "")
		entry := stackByName(result, stackName)
		if entry["Services"] != "1" {
			t.Fatalf("alias services = %#v", entry)
		}
	})

	t.Run("short name is unchanged on a second run", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: List stacks with short name
    docker_stack_info:
    register: stack_info_result

  - name: Persist short-name result
    template:
      src: ` + templatePath + `
      dest: /tmp/dibra-si-short.json
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("short name failed: %s", output)
		}
		first := readRemoteJSONMap(t, client, "/tmp/dibra-si-short.json")
		again := success(t, "idempotent", "", "")
		if first["changed"] != false || again["changed"] != false {
			t.Fatalf("idempotent changed first=%#v again=%#v", first, again)
		}
		if fmt.Sprint(first["results"]) != fmt.Sprint(again["results"]) {
			t.Fatalf("idempotent results first=%#v again=%#v", first["results"], again["results"])
		}
	})

	t.Run("check and diff modes stay read only", func(t *testing.T) {
		before := strings.TrimSpace(remoteExec(t, client, "docker stack ls --format '{{.Name}}'"))
		checked := success(t, "check", "", "", "--check", "--diff")
		stackByName(checked, stackName)
		if _, found := checked["diff"]; found {
			t.Fatalf("unexpected diff = %#v", checked["diff"])
		}
		after := strings.TrimSpace(remoteExec(t, client, "docker stack ls --format '{{.Name}}'"))
		if before != after {
			t.Fatalf("check/diff mutated stacks %q -> %q", before, after)
		}
	})

	t.Run("matches docker stack ls json and lists multiple stacks", func(t *testing.T) {
		deploy(t, secondName, baseFile)
		result := success(t, "multi", "", "")
		stackByName(result, stackName)
		stackByName(result, secondName)

		raw := strings.TrimSpace(remoteExec(t, client, "docker stack ls --format '{{json .}}'"))
		cliNames := map[string]string{}
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("decode docker stack ls: %v (%s)", err, line)
			}
			name, _ := entry["Name"].(string)
			services, _ := entry["Services"].(string)
			cliNames[name] = services
		}
		moduleResults, _ := result["results"].([]any)
		if len(moduleResults) != len(cliNames) {
			t.Fatalf("module=%#v cli=%#v", result["results"], cliNames)
		}
		for _, item := range moduleResults {
			entry, _ := item.(map[string]any)
			name, _ := entry["Name"].(string)
			if cliNames[name] != entry["Services"] {
				t.Fatalf("services for %s: module=%#v cli=%#v", name, entry["Services"], cliNames[name])
			}
		}
	})

	t.Run("two-service stack reports Services 2", func(t *testing.T) {
		deploy(t, twoName, twoFile)
		result := success(t, "two-services", "", "")
		entry := stackByName(result, twoName)
		if entry["Services"] != "2" {
			t.Fatalf("two-service stack = %#v", entry)
		}
		remove(t, twoName)
	})

	t.Run("removed stack disappears from results", func(t *testing.T) {
		remove(t, secondName)
		result := success(t, "after-remove", "", "")
		raw, _ := result["results"].([]any)
		for _, item := range raw {
			entry, _ := item.(map[string]any)
			if entry["Name"] == secondName {
				t.Fatalf("removed stack still listed: %#v", result["results"])
			}
		}
		stackByName(result, stackName)
	})
}
