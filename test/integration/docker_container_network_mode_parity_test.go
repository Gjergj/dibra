//go:build integration

package integration

import (
	"encoding/json"
	"testing"
)

// TestPlaybook_DockerContainerNetworkModeParity ports the pinned network_mode
// normalization and networks_cli_compatible default-network behavior.
func TestPlaybook_DockerContainerNetworkModeParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		net1 = "dibra-container-network-mode-one"
		net2 = "dibra-container-network-mode-two"
	)
	cleanup := func() {
		remoteExec(t, client, "docker rm -f dibra-container-network-mode dibra-container-net-cli-true dibra-container-net-cli-false || true")
		remoteExec(t, client, "docker network rm "+net1+" "+net2+" || true")
	}
	cleanup()
	defer cleanup()
	mustRemote(t, client, "docker network create "+net1)
	mustRemote(t, client, "docker network create "+net2)

	t.Run("network_mode", func(t *testing.T) {
		const name = "dibra-container-network-mode"
		run := func(suffix, mode string) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "network-mode-"+suffix, `
      name: `+name+`
      image: alpine:latest
      command: ["sleep", "300"]
      state: started
      force_kill: true
      network_mode: `+mode+`
`, "--diff")
		}

		created := run("default", "default")
		assertChanged(t, created, true)
		initialID := containerResultID(t, created)
		assertChanged(t, run("bridge-equivalent", "bridge"), false)

		none := run("none", "none")
		assertChanged(t, none, true)
		if got := containerResultID(t, none); got == initialID {
			t.Fatalf("network_mode change did not recreate container: %s", initialID)
		}
		assertChanged(t, run("none-idempotent", "none"), false)
		if got := remoteExec(t, client, "docker inspect --format '{{.HostConfig.NetworkMode}}' "+name); got != "none" {
			t.Fatalf("network mode = %q, want none", got)
		}
	})

	runNetworks := func(suffix, name string, cliCompatible bool, strict bool, networks ...string) map[string]any {
		t.Helper()
		networkYAML := ""
		for _, network := range networks {
			networkYAML += "        - name: " + network + "\n"
		}
		comparisons := ""
		if strict {
			comparisons = "      comparisons:\n        networks: strict\n"
		}
		return runContainerStateTask(t, client, "networks-cli-"+suffix, `
      name: `+name+`
      image: alpine:latest
      command: ["sleep", "300"]
      state: started
      force_kill: true
      networks_cli_compatible: `+boolString(cliCompatible)+`
      networks:
`+networkYAML+comparisons, "--diff")
	}

	t.Run("networks_cli_compatible_true", func(t *testing.T) {
		const name = "dibra-container-net-cli-true"
		created := runNetworks("true-create", name, true, false, net1, net2)
		assertChanged(t, created, true)
		assertChanged(t, runNetworks("true-idempotent", name, true, false, net1, net2), false)
		networks := inspectedContainerNetworks(t, client, name)
		if _, found := networks["bridge"]; found || len(networks) != 2 {
			t.Fatalf("CLI-compatible networks = %#v, want only custom networks", networks)
		}
	})

	t.Run("networks_cli_compatible_false", func(t *testing.T) {
		const name = "dibra-container-net-cli-false"
		created := runNetworks("false-create", name, false, false, net1, net2)
		assertChanged(t, created, true)
		containerID := containerResultID(t, created)
		assertChanged(t, runNetworks("false-idempotent", name, false, false, net1, net2), false)
		networks := inspectedContainerNetworks(t, client, name)
		for _, network := range []string{"bridge", net1, net2} {
			if _, found := networks[network]; !found {
				t.Fatalf("non-CLI-compatible networks = %#v, missing %s", networks, network)
			}
		}

		purged := runNetworks("false-strict", name, false, true, "bridge", net1)
		assertChanged(t, purged, true)
		if got := containerResultID(t, purged); got != containerID {
			t.Fatalf("strict network reconciliation recreated container: before=%s after=%s", containerID, got)
		}
		networks = inspectedContainerNetworks(t, client, name)
		if _, found := networks[net2]; found || len(networks) != 2 {
			t.Fatalf("strict reconciled networks = %#v, want bridge and %s", networks, net1)
		}
		assertChanged(t, runNetworks("false-strict-idempotent", name, false, true, net1, "bridge"), false)
	})
}

func inspectedContainerNetworks(t *testing.T, client interface {
	Run(command string) (string, string, error)
}, name string) map[string]any {
	t.Helper()
	stdout, stderr, err := client.Run("docker inspect --format '{{json .NetworkSettings.Networks}}' " + name)
	if err != nil {
		t.Fatalf("inspect container networks: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	var networks map[string]any
	if err := json.Unmarshal([]byte(stdout), &networks); err != nil {
		t.Fatalf("decode container networks: %v\n%s", err, stdout)
	}
	return networks
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
