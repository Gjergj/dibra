//go:build integration

package integration

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerInfoParity independently ports the pinned
// community.docker docker_container_info integration target
// (tests/integration/targets/docker_container_info/tasks/main.yml), the
// 5.2.2 documentation examples, and documented name/short-ID lookup plus
// check-mode contracts that the upstream target does not run.
func TestPlaybook_DockerContainerInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		name  = "dibra-container-info-parity"
		image = "alpine:latest"
	)
	remoteExec(t, client, "docker pull "+image)
	remoteExec(t, client, "docker rm -f "+name+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent /tmp/dibra-container-info-*.json")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")

	t.Run("missing container returns exists false and container null", func(t *testing.T) {
		result := runContainerInfo(t, client, "missing", `
      name: `+name+`
`)
		if result["changed"] != false || result["exists"] != false || result["container"] != nil {
			t.Fatalf("missing result = %#v", result)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"container":null`) || !strings.Contains(string(encoded), `"exists":false`) {
			t.Fatalf("missing JSON = %s", encoded)
		}
	})

	remoteExec(t, client, "docker run -d --name "+name+" --label parity=container-info -e MODE=test "+image+" /bin/sh -c 'sleep 10m'")
	fullID := strings.TrimSpace(remoteExec(t, client, "docker inspect --format '{{.Id}}' "+name))
	shortID := strings.TrimPrefix(fullID, "sha256:")
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	t.Run("present container matches docker inspect", func(t *testing.T) {
		result := runContainerInfo(t, client, "present", `
      name: `+name+`
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
`)
		if result["changed"] != false || result["exists"] != true {
			t.Fatalf("present result = %#v", result)
		}
		assertContainerMatchesInspect(t, client, result, name)
	})

	t.Run("lookup by full ID", func(t *testing.T) {
		result := runContainerInfo(t, client, "full-id", `
      name: `+fullID+`
`)
		if result["changed"] != false || result["exists"] != true {
			t.Fatalf("full ID result = %#v", result)
		}
		assertContainerMatchesInspect(t, client, result, name)
	})

	t.Run("lookup by short ID", func(t *testing.T) {
		result := runContainerInfo(t, client, "short-id", `
      name: `+shortID+`
`)
		if result["changed"] != false || result["exists"] != true {
			t.Fatalf("short ID result = %#v", result)
		}
		assertContainerMatchesInspect(t, client, result, name)
	})

	t.Run("paused container still exists", func(t *testing.T) {
		remoteExec(t, client, "docker pause "+name)
		defer remoteExec(t, client, "docker unpause "+name+" || true")
		result := runContainerInfo(t, client, "paused", `
      name: `+name+`
`)
		if result["exists"] != true {
			t.Fatalf("paused result = %#v", result)
		}
		container, _ := result["container"].(map[string]any)
		state, _ := container["State"].(map[string]any)
		if state["Paused"] != true {
			t.Fatalf("paused container State = %#v", state)
		}
		assertContainerMatchesInspect(t, client, result, name)
	})

	t.Run("docs example exists flag", func(t *testing.T) {
		result := runContainerInfo(t, client, "docs", `
      name: `+name+`
`)
		if result["exists"] != true {
			t.Fatalf("docs exists = %#v", result)
		}
		container, _ := result["container"].(map[string]any)
		if container["Id"] != fullID {
			t.Fatalf("docs container Id = %#v", container["Id"])
		}
	})

	t.Run("stopped container still exists", func(t *testing.T) {
		remoteExec(t, client, "docker stop "+name)
		defer remoteExec(t, client, "docker start "+name+" || true")
		result := runContainerInfo(t, client, "stopped", `
      name: `+name+`
`)
		if result["exists"] != true {
			t.Fatalf("stopped result = %#v", result)
		}
		container, _ := result["container"].(map[string]any)
		state, _ := container["State"].(map[string]any)
		if state["Running"] == true {
			t.Fatalf("stopped container still running: %#v", state)
		}
		assertContainerMatchesInspect(t, client, result, name)
	})

	t.Run("check and diff modes stay read only", func(t *testing.T) {
		before := strings.TrimSpace(remoteExec(t, client, "docker inspect --format '{{.Id}} {{.State.Status}}' "+name))
		result := runContainerInfoWithArgs(t, client, "check", `
      name: `+name+`
`, "--check", "--diff")
		if result["changed"] != false || result["exists"] != true || result["skipped"] == true {
			t.Fatalf("check mode result = %#v", result)
		}
		assertContainerMatchesInspect(t, client, result, name)
		after := strings.TrimSpace(remoteExec(t, client, "docker inspect --format '{{.Id}} {{.State.Status}}' "+name))
		if before != after {
			t.Fatalf("check/diff mode mutated container %q -> %q", before, after)
		}
	})

	t.Run("missing name fails", func(t *testing.T) {
		output := runPlaybook(t, playbookHeader+`
  - name: Missing name
    docker_container_info:
`)
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, "name is required") {
			t.Fatalf("missing name: %s", output)
		}
	})
}

func runContainerInfo(t *testing.T, client *ssh.Client, suffix, arguments string) map[string]any {
	t.Helper()
	return runContainerInfoWithArgs(t, client, suffix, arguments)
}

func runContainerInfoWithArgs(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) map[string]any {
	t.Helper()
	remotePath := "/tmp/dibra-container-info-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "container_info")
	playbook := playbookHeader + `
  - name: Inspect container
    community.docker.docker_container_info:
` + arguments + `
    register: container_info

  - name: Persist container info result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extra...)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("container info failed: %s", output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}

func assertContainerMatchesInspect(t *testing.T, client *ssh.Client, result map[string]any, name string) {
	t.Helper()
	container, ok := result["container"].(map[string]any)
	if !ok {
		t.Fatalf("container = %T, want object in %#v", result["container"], result)
	}
	for _, key := range []string{"Id", "State", "Config", "HostConfig", "NetworkSettings", "Mounts"} {
		if _, found := container[key]; !found {
			t.Fatalf("raw inspection key %q missing from %#v", key, container)
		}
	}
	for _, key := range []string{"id", "state", "config", "host_config"} {
		if _, found := container[key]; found {
			t.Fatalf("snake_case key %q leaked: %#v", key, container)
		}
	}
	raw, stderr, err := client.Run("docker inspect " + name)
	if err != nil {
		t.Fatalf("docker inspect %s: %v: %s", name, err, stderr)
	}
	var inspected []map[string]any
	if err := json.Unmarshal([]byte(raw), &inspected); err != nil || len(inspected) != 1 {
		t.Fatalf("decode docker inspect %s: %v\n%s", name, err, raw)
	}
	if !reflect.DeepEqual(container, inspected[0]) {
		t.Fatalf("module result does not match docker inspect\nmodule: %#v\ndocker: %#v", container, inspected[0])
	}
}
