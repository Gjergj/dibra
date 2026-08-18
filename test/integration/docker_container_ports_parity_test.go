//go:build integration

package integration

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerPortsParity independently ports pinned port
// validation, random host range, duplicate binding, and publish-all behavior.
func TestPlaybook_DockerContainerPortsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const name = "dibra-container-ports-parity"
	remoteExec(t, client, "docker rm -f "+name+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")

	t.Run("one container port host range", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		args := func(hostRange string) string {
			return `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
      published_ports:
        - "127.0.0.1:` + hostRange + `:80/tcp"
      comparisons:
        published_ports: strict
`
		}
		created := runContainerStateTask(t, client, "ports-host-range", args("48110-48120"), "--diff")
		assertChanged(t, created, true)
		containerID := containerResultID(t, created)
		hostPort := containerPublishedHostPorts(t, client, name, "80/tcp")
		if len(hostPort) != 1 {
			t.Fatalf("host range bindings = %#v", hostPort)
		}
		value, err := strconv.Atoi(hostPort[0])
		if err != nil || value < 48110 || value > 48120 {
			t.Fatalf("assigned host port = %q, want 48110-48120", hostPort[0])
		}

		idempotent := runContainerStateTask(t, client, "ports-host-range-idempotent", args("48110-48120"), "--diff")
		assertChanged(t, idempotent, false)
		if got := containerResultID(t, idempotent); got != containerID {
			t.Fatalf("host range idempotency recreated container: before=%s after=%s", containerID, got)
		}

		changed := runContainerStateTask(t, client, "ports-host-range-changed", args("48130-48140"), "--diff")
		assertChanged(t, changed, true)
		if got := containerResultID(t, changed); got == containerID {
			t.Fatalf("different host range kept container ID %s", containerID)
		}
	})

	t.Run("validation messages match upstream", func(t *testing.T) {
		tests := map[string]string{
			"[::1:2000:3000": `Cannot find closing "]" in input "[::1:2000:3000" for opening "[" at index 1!`,
			"::1:2000:3000":  `Invalid port description "::1:2000:3000" - expected 1 to 3 colon-separated parts, but got 5. Maybe you forgot to use square brackets ([...]) around an IPv6 address?`,
			"foo:2000:3000":  `Bind addresses for published ports must be IPv4 or IPv6 addresses, not hostnames. Use the dig lookup to resolve hostnames. (Found hostname: foo)`,
		}
		for port, want := range tests {
			result := runContainerAgentRequest(t, client, map[string]any{
				"name": name, "image": "alpine:latest", "state": "started",
				"published_ports": []string{port},
			})
			if result["failed"] != true || result["msg"] != want {
				t.Fatalf("published port %q result = %#v, want %q", port, result, want)
			}
		}
	})

	t.Run("duplicate bindings normalize protocol", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		args := func(protocol bool) string {
			suffix := ""
			if protocol {
				suffix = "/tcp"
			}
			return `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
      published_ports:
        - "127.0.0.1:48150:80` + suffix + `"
        - "127.0.0.1:48151:80` + suffix + `"
      comparisons:
        published_ports: strict
`
		}
		assertChanged(t, runContainerStateTask(t, client, "ports-duplicates", args(false), "--diff"), true)
		assertChanged(t, runContainerStateTask(t, client, "ports-duplicates-idempotent", args(false), "--diff"), false)
		assertChanged(t, runContainerStateTask(t, client, "ports-duplicates-protocol", args(true), "--diff"), false)
		ports := containerPublishedHostPorts(t, client, name, "80/tcp")
		if len(ports) != 2 || !containsString(ports, "48150") || !containsString(ports, "48151") {
			t.Fatalf("duplicate host bindings = %#v", ports)
		}
	})

	t.Run("publish all exposed ports", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		args := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
      exposed_ports: ["80", "81/udp"]
      publish_all_ports: true
`
		assertChanged(t, runContainerStateTask(t, client, "ports-publish-all", args, "--diff"), true)
		assertChanged(t, runContainerStateTask(t, client, "ports-publish-all-idempotent", args, "--diff"), false)
		publishAll := mustRemote(t, client, "docker inspect --format '{{.HostConfig.PublishAllPorts}}' "+name)
		if !strings.EqualFold(publishAll, "true") {
			t.Fatalf("PublishAllPorts = %q", publishAll)
		}
		raw := mustRemote(t, client, "docker inspect --format '{{json .NetworkSettings.Ports}}' "+name)
		if !strings.Contains(raw, `"80/tcp"`) || !strings.Contains(raw, `"81/udp"`) {
			t.Fatalf("published exposed ports = %s", raw)
		}
	})
}

func containerPublishedHostPorts(t *testing.T, client interface {
	Run(string) (string, string, error)
}, name, key string) []string {
	t.Helper()
	raw, stderr, err := client.Run("docker inspect --format '{{json .NetworkSettings.Ports}}' " + name)
	if err != nil {
		t.Fatalf("inspect port bindings: %v\n%s", err, stderr)
	}
	var bindings map[string][]map[string]string
	if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
		t.Fatalf("decode port bindings: %v\n%s", err, raw)
	}
	result := make([]string, 0, len(bindings[key]))
	for _, binding := range bindings[key] {
		result = append(result, binding["HostPort"])
	}
	return result
}
