//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerStackParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	prefix := fmt.Sprintf("dibra-st-%d", time.Now().UnixNano()%100000000)
	stackName := prefix
	pruneName := prefix + "-p"
	docsName := prefix + "-d"
	flagsName := prefix + "-f"
	baseFile := "/tmp/" + prefix + "-base.yml"
	twoFile := "/tmp/" + prefix + "-two.yml"
	oneFile := "/tmp/" + prefix + "-one.yml"
	customCLI := "/tmp/" + prefix + "-docker"

	mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-st-*.json /tmp/.dibra-agent "+baseFile+" "+twoFile+" "+oneFile+" "+customCLI)
	defer func() {
		mustRemote(t, client, "docker stack rm "+stackName+" "+pruneName+" "+docsName+" "+flagsName+" >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker swarm leave --force >/dev/null 2>&1 || true")
	}()

	templatePath := writeResultTemplate(t, "stack_result")
	run := func(t *testing.T, name, arguments, taskOptions string) (map[string]any, string) {
		t.Helper()
		remotePath := "/tmp/dibra-st-" + name + ".json"
		playbook := playbookHeader + `
  - name: Manage docker stack
    community.docker.docker_stack:
` + arguments + `
    register: stack_result
` + taskOptions + `

  - name: Persist stack result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") || strings.Contains(output, "Failed to load config") {
			return nil, output
		}
		return readRemoteJSONMap(t, client, remotePath), output
	}
	success := func(t *testing.T, name, arguments, taskOptions string) map[string]any {
		t.Helper()
		result, output := run(t, name, arguments, taskOptions)
		if result == nil {
			t.Fatalf("%s failed: %s", name, output)
		}
		return result
	}
	serviceNames := func(stack string) string {
		t.Helper()
		return strings.TrimSpace(remoteExec(t, client, "docker stack services "+stack+" --format '{{.Name}}' 2>/dev/null"))
	}

	t.Run("missing required name", func(t *testing.T) {
		_, output := run(t, "missing-name", "      state: present\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "missing required arguments: name") {
			t.Fatalf("missing name = %s", output)
		}
	})

	t.Run("present without compose", func(t *testing.T) {
		_, output := run(t, "missing-compose", "      name: "+stackName+"\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "compose parameter must be a list containing at least one element") {
			t.Fatalf("missing compose = %s", output)
		}
	})

	t.Run("invalid compose element", func(t *testing.T) {
		_, output := run(t, "invalid-compose", "      name: "+stackName+"\n      compose:\n        - 1\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "compose element '1' must be a string or a dictionary") {
			t.Fatalf("invalid compose = %s", output)
		}
	})

	mustRemote(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	mustRemote(t, client, "docker pull alpine:latest >/dev/null")
	mustRemote(t, client, "cat > "+baseFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "cat > "+twoFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\n  extra:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "cat > "+oneFile+" << 'EOF'\nversion: \"3\"\nservices:\n  busybox:\n    image: alpine:latest\n    command: sleep 3600\nEOF")
	mustRemote(t, client, "ln -sf \"$(command -v docker)\" "+customCLI)

	t.Run("absent retries when missing is unchanged", func(t *testing.T) {
		result := success(t, "absent-missing", "      name: "+stackName+"\n      state: absent\n      absent_retries: 30\n", "")
		if result["changed"] != false {
			t.Fatalf("absent missing = %#v", result)
		}
	})

	t.Run("check mode is skipped", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Check-mode stack deploy
    community.docker.docker_stack:
      name: ` + stackName + `
      compose:
        - ` + baseFile + `
`
		output := runPlaybookWithArgs(t, playbook, "--check")
		if strings.Contains(output, "FAILED") || !strings.Contains(output, "SKIPPED") {
			t.Fatalf("check-mode output = %s", output)
		}
		if names := serviceNames(stackName); names != "" && !strings.Contains(names, "Nothing found") {
			t.Fatalf("check mode deployed services: %s", names)
		}
	})

	t.Run("create with compose file path", func(t *testing.T) {
		created := success(t, "create", "      name: "+stackName+"\n      compose:\n        - "+baseFile+"\n", "")
		if created["changed"] != true {
			t.Fatalf("create = %#v", created)
		}
		diff, _ := created["stack_spec_diff"].(map[string]any)
		if diff[stackName+"_busybox"] == nil {
			t.Fatalf("stack_spec_diff = %#v", created["stack_spec_diff"])
		}
		if !strings.Contains(serviceNames(stackName), stackName+"_busybox") {
			t.Fatalf("missing service: %s", serviceNames(stackName))
		}
	})

	t.Run("second identical deploy is unchanged", func(t *testing.T) {
		repeat := success(t, "idempotent", "      name: "+stackName+"\n      compose:\n        - "+baseFile+"\n", "")
		if repeat["changed"] != false {
			t.Fatalf("idempotent = %#v", repeat)
		}
		if _, found := repeat["stack_spec_diff"]; found {
			t.Fatalf("unexpected stack_spec_diff = %#v", repeat["stack_spec_diff"])
		}
	})

	t.Run("yaml override updates env and stack_spec_diff", func(t *testing.T) {
		updated := success(t, "override", `      name: `+stackName+`
      compose:
        - `+baseFile+`
        - version: "3"
          services:
            busybox:
              environment:
                envvar: value
`, "")
		if updated["changed"] != true {
			t.Fatalf("override = %#v", updated)
		}
		raw, _ := json.Marshal(updated["stack_spec_diff"])
		if !strings.Contains(string(raw), "envvar=value") {
			t.Fatalf("stack_spec_diff = %s", raw)
		}
	})

	t.Run("compose_file alias deploys", func(t *testing.T) {
		result := success(t, "compose-file", "      name: "+stackName+"\n      compose_file: "+baseFile+"\n", "")
		if result["failed"] == true {
			t.Fatalf("compose_file = %#v", result)
		}
	})

	t.Run("delete existing then absent idempotency", func(t *testing.T) {
		removed := success(t, "delete", "      name: "+stackName+"\n      state: absent\n      absent_retries: 30\n", "")
		if removed["changed"] != true {
			t.Fatalf("delete = %#v", removed)
		}
		if names := serviceNames(stackName); names != "" && !strings.Contains(names, "Nothing found") {
			t.Fatalf("stack still present: %s", names)
		}
		again := success(t, "delete-again", "      name: "+stackName+"\n      state: absent\n      absent_retries: 30\n", "")
		if again["changed"] != false {
			t.Fatalf("delete again = %#v", again)
		}
	})

	t.Run("docs override example and docker_cli", func(t *testing.T) {
		created := success(t, "docs", `      name: `+docsName+`
      docker_cli: `+customCLI+`
      compose:
        - `+baseFile+`
        - version: "3"
          services:
            busybox:
              environment:
                ENVVAR: envvar
`, "")
		if created["changed"] != true {
			t.Fatalf("docs create = %#v", created)
		}
		raw, _ := json.Marshal(created["stack_spec_diff"])
		if !strings.Contains(string(raw), "ENVVAR=envvar") {
			t.Fatalf("docs stack_spec_diff = %s", raw)
		}
		removed := success(t, "docs-rm", "      name: "+docsName+"\n      state: absent\n      absent_retries: 30\n      docker_cli: "+customCLI+"\n", "")
		if removed["changed"] != true {
			t.Fatalf("docs rm = %#v", removed)
		}
	})

	t.Run("prune removes extra services", func(t *testing.T) {
		created := success(t, "prune-create", "      name: "+pruneName+"\n      compose:\n        - "+twoFile+"\n", "")
		if created["changed"] != true {
			t.Fatalf("prune create = %#v", created)
		}
		names := serviceNames(pruneName)
		if !strings.Contains(names, pruneName+"_busybox") || !strings.Contains(names, pruneName+"_extra") {
			t.Fatalf("expected two services, got %s", names)
		}
		pruned := success(t, "prune", "      name: "+pruneName+"\n      prune: true\n      compose:\n        - "+oneFile+"\n", "")
		if pruned["changed"] != true {
			t.Fatalf("prune = %#v", pruned)
		}
		names = serviceNames(pruneName)
		if !strings.Contains(names, pruneName+"_busybox") || strings.Contains(names, pruneName+"_extra") {
			t.Fatalf("prune leftover = %s", names)
		}
		success(t, "prune-rm", "      name: "+pruneName+"\n      state: absent\n      absent_retries: 30\n", "")
	})

	t.Run("detach resolve_image with_registry_auth and connection alias", func(t *testing.T) {
		created := success(t, "flags", `      name: `+flagsName+`
      compose:
        - `+baseFile+`
      detach: false
      resolve_image: never
      with_registry_auth: true
      docker_url: unix:///var/run/docker.sock
`, "")
		if created["changed"] != true {
			t.Fatalf("flags create = %#v", created)
		}
		state := strings.TrimSpace(mustRemote(t, client, "docker service ps "+flagsName+"_busybox --format '{{.CurrentState}}' | head -n1"))
		if !strings.Contains(strings.ToLower(state), "running") {
			t.Fatalf("detach=false did not wait for running task: %s", state)
		}
		success(t, "flags-rm", "      name: "+flagsName+"\n      state: absent\n      absent_retries: 30\n      detach: false\n", "")
	})

	t.Run("missing compose file fails with deploy message", func(t *testing.T) {
		_, output := run(t, "missing-file", "      name: "+stackName+"\n      compose:\n        - /tmp/"+prefix+"-missing.yml\n", "")
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "docker stack up deploy command failed") {
			t.Fatalf("missing file = %s", output)
		}
	})
}
