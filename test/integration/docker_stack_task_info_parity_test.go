//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerStackTaskInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	prefix := fmt.Sprintf("dibra-stask-%d", time.Now().UnixNano()%100000000)
	stackName := prefix
	twoName := prefix + "-two"
	baseFile := "/tmp/" + prefix + "-base.yml"
	twoFile := "/tmp/" + prefix + "-two.yml"
	customCLI := "/tmp/" + prefix + "-docker"

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-stask-*.json /tmp/.dibra-agent "+baseFile+" "+twoFile+" "+customCLI)
	defer func() {
		mustRemote(t, client, "docker stack rm "+stackName+" "+twoName+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	}()

	templatePath := writeResultTemplate(t, "stack_task_info_result")
	run := func(t *testing.T, name, arguments, taskOptions string, extra ...string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-stask-" + name + ".json"
		playbook := playbookHeader + `
  - name: List docker stack tasks
    community.docker.docker_stack_task_info:
` + arguments + `
    register: stack_task_info_result
` + taskOptions + `

  - name: Persist stack task info result
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
	taskByName := func(result map[string]any, name string) map[string]any {
		t.Helper()
		raw, _ := result["results"].([]any)
		for _, item := range raw {
			entry, _ := item.(map[string]any)
			if entry["Name"] == name {
				return entry
			}
		}
		t.Fatalf("task %q missing from %#v", name, result["results"])
		return nil
	}
	waitRunning := func(t *testing.T, stack, taskName string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			raw := strings.TrimSpace(remoteExec(t, client, "docker stack ps "+stack+" --format '{{json .}}' 2>/dev/null"))
			for _, line := range strings.Split(raw, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					continue
				}
				current, _ := entry["CurrentState"].(string)
				if entry["Name"] == taskName && strings.HasPrefix(current, "Running") {
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s in stack %s", taskName, stack)
	}

	t.Run("missing required name", func(t *testing.T) {
		_, output := run(t, "missing-name", "", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "missing required arguments: name") {
			t.Fatalf("missing name = %s", output)
		}
	})

	t.Run("fails when not a swarm manager", func(t *testing.T) {
		_, output := run(t, "not-manager", "      name: "+stackName+"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Error response from daemon: This node is not a swarm manager") {
			t.Fatalf("not manager = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")
	mustRemote(t, client, "cat > "+baseFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "cat > "+twoFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\n  extra:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "ln -sf \"$(command -v docker)\" "+customCLI)

	t.Run("missing stack fails", func(t *testing.T) {
		_, output := run(t, "missing-stack", "      name: "+stackName+"-missing\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(strings.ToLower(output), "nothing found in stack") {
			t.Fatalf("missing stack = %s", output)
		}
	})

	t.Run("host and cli_context are mutually exclusive", func(t *testing.T) {
		_, output := run(t, "conflict", "      name: "+stackName+"\n      docker_host: unix:///var/run/docker.sock\n      cli_context: default\n", "")
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

	t.Run("lists created stack tasks with DesiredState Image and Name", func(t *testing.T) {
		deploy(t, stackName, baseFile)
		waitRunning(t, stackName, stackName+"_busybox.1")
		result := success(t, "one", "      name: "+stackName+"\n", "")
		entry := taskByName(result, stackName+"_busybox.1")
		if entry["DesiredState"] != "Running" {
			t.Fatalf("desired state = %#v", entry)
		}
		image, _ := entry["Image"].(string)
		if !strings.Contains(image, "alpine:latest") {
			t.Fatalf("image = %#v", entry)
		}
		current, _ := entry["CurrentState"].(string)
		if !strings.HasPrefix(current, "Running") {
			t.Fatalf("current state = %#v", entry)
		}
		if _, found := entry["ID"]; !found {
			t.Fatalf("missing ID: %#v", entry)
		}
		for _, key := range []string{"name", "image", "desired_state", "current_state"} {
			if _, found := entry[key]; found {
				t.Fatalf("snake_case key %q leaked: %#v", key, entry)
			}
		}
	})

	t.Run("canonical name docker_cli and connection alias", func(t *testing.T) {
		result := success(t, "alias", "      name: "+stackName+"\n      docker_cli: "+customCLI+"\n      docker_url: unix:///var/run/docker.sock\n", "")
		entry := taskByName(result, stackName+"_busybox.1")
		if entry["DesiredState"] != "Running" {
			t.Fatalf("alias task = %#v", entry)
		}
	})

	t.Run("short name is unchanged on a second run", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: List stack tasks with short name
    docker_stack_task_info:
      name: ` + stackName + `
    register: stack_task_info_result

  - name: Persist short-name result
    template:
      src: ` + templatePath + `
      dest: /tmp/dibra-stask-short.json
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("short name failed: %s", output)
		}
		first := readRemoteJSONMap(t, client, "/tmp/dibra-stask-short.json")
		again := success(t, "idempotent", "      name: "+stackName+"\n", "")
		if first["changed"] != false || again["changed"] != false {
			t.Fatalf("idempotent changed first=%#v again=%#v", first, again)
		}
		firstTask := taskByName(first, stackName+"_busybox.1")
		againTask := taskByName(again, stackName+"_busybox.1")
		if firstTask["Name"] != againTask["Name"] || firstTask["DesiredState"] != againTask["DesiredState"] || firstTask["ID"] != againTask["ID"] {
			t.Fatalf("idempotent tasks first=%#v again=%#v", firstTask, againTask)
		}
	})

	t.Run("check and diff modes stay read only", func(t *testing.T) {
		before := strings.TrimSpace(remoteExec(t, client, "docker stack ps "+stackName+" --format '{{.ID}}'"))
		checked := success(t, "check", "      name: "+stackName+"\n", "", "--check", "--diff")
		taskByName(checked, stackName+"_busybox.1")
		if _, found := checked["diff"]; found {
			t.Fatalf("unexpected diff = %#v", checked["diff"])
		}
		after := strings.TrimSpace(remoteExec(t, client, "docker stack ps "+stackName+" --format '{{.ID}}'"))
		if before != after {
			t.Fatalf("check/diff mutated tasks %q -> %q", before, after)
		}
	})

	t.Run("matches docker stack ps json", func(t *testing.T) {
		result := success(t, "cli-match", "      name: "+stackName+"\n", "")
		raw := strings.TrimSpace(remoteExec(t, client, "docker stack ps "+stackName+" --format '{{json .}}'"))
		cliByID := map[string]map[string]any{}
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("decode docker stack ps: %v (%s)", err, line)
			}
			id, _ := entry["ID"].(string)
			cliByID[id] = entry
		}
		moduleResults, _ := result["results"].([]any)
		if len(moduleResults) != len(cliByID) {
			t.Fatalf("module=%#v cli=%#v", result["results"], cliByID)
		}
		for _, item := range moduleResults {
			entry, _ := item.(map[string]any)
			id, _ := entry["ID"].(string)
			cli := cliByID[id]
			if cli == nil {
				t.Fatalf("module ID %s missing from CLI: %#v", id, entry)
			}
			if cli["Name"] != entry["Name"] || cli["DesiredState"] != entry["DesiredState"] {
				t.Fatalf("mismatch id=%s module=%#v cli=%#v", id, entry, cli)
			}
		}
	})

	t.Run("two-service stack lists both tasks", func(t *testing.T) {
		deploy(t, twoName, twoFile)
		waitRunning(t, twoName, twoName+"_busybox.1")
		waitRunning(t, twoName, twoName+"_extra.1")
		result := success(t, "two-services", "      name: "+twoName+"\n", "")
		taskByName(result, twoName+"_busybox.1")
		taskByName(result, twoName+"_extra.1")
		remove(t, twoName)
	})

	t.Run("removed stack fails lookup", func(t *testing.T) {
		remove(t, stackName)
		_, output := run(t, "after-remove", "      name: "+stackName+"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(strings.ToLower(output), "nothing found in stack") {
			t.Fatalf("after remove = %s", output)
		}
	})
}
