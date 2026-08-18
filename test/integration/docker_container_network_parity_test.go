//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerNetworkEndpointParity independently ports the
// pinned static-address, alias, reconnect, check-mode, and idempotency cases.
func TestPlaybook_DockerContainerNetworkEndpointParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		name    = "dibra-container-network-endpoint-parity"
		linked  = "dibra-container-network-endpoint-linked"
		network = "dibra-container-network-endpoint-net"
		ipv4A   = "172.29.91.10"
		ipv4B   = "172.29.91.11"
		ipv6A   = "fd00:29:91::10"
		ipv6B   = "fd00:29:91::11"
	)
	cleanup := func() {
		remoteExec(t, client, "docker rm -f "+name+" "+linked+" || true")
		remoteExec(t, client, "docker network rm "+network+" || true")
	}
	cleanup()
	defer cleanup()
	mustRemote(t, client, "docker network create --ipv6 --subnet 172.29.91.0/24 --subnet fd00:29:91::/64 "+network)
	mustRemote(t, client, "docker run -d --name "+linked+" --network "+network+" alpine:latest sleep 600")

	networkArgs := func(ipv4, ipv6, alias string) string {
		return `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: stopped
      force_kill: true
      networks:
        - name: ` + network + `
          ipv4_address: ` + ipv4 + `
          ipv6_address: ` + ipv6 + `
          mac_address: "02:42:ac:1d:5b:10"
          aliases:
            - ` + alias + `
          links:
            - ` + linked + `:linked-alias
      comparisons:
        networks: strict
`
	}

	created := runContainerStateTask(t, client, "network-endpoint-create", networkArgs(ipv4A, ipv6A, "first-alias"), "--diff")
	assertChanged(t, created, true)
	containerID := containerResultID(t, created)
	assertNetworkEndpoint(t, client, name, network, ipv4A, ipv6A, "first-alias", linked+":linked-alias")

	idempotent := runContainerStateTask(t, client, "network-endpoint-idempotent", networkArgs(ipv4A, ipv6A, "first-alias"), "--diff")
	assertChanged(t, idempotent, false)
	if got := containerResultID(t, idempotent); got != containerID {
		t.Fatalf("idempotent network run recreated container: before=%s after=%s", containerID, got)
	}

	ipv4Check := runContainerStateTask(t, client, "network-endpoint-ipv4-check", networkArgs(ipv4B, ipv6A, "first-alias"), "--check", "--diff")
	assertChanged(t, ipv4Check, true)
	assertNetworkEndpoint(t, client, name, network, ipv4A, ipv6A, "first-alias", linked+":linked-alias")

	ipv4Changed := runContainerStateTask(t, client, "network-endpoint-ipv4", networkArgs(ipv4B, ipv6A, "first-alias"), "--diff")
	assertChanged(t, ipv4Changed, true)
	if got := containerResultID(t, ipv4Changed); got != containerID {
		t.Fatalf("IPv4 reconnect recreated container: before=%s after=%s", containerID, got)
	}
	assertNetworkEndpoint(t, client, name, network, ipv4B, ipv6A, "first-alias", linked+":linked-alias")

	ipv6Changed := runContainerStateTask(t, client, "network-endpoint-ipv6", networkArgs(ipv4B, ipv6B, "first-alias"), "--diff")
	assertChanged(t, ipv6Changed, true)
	if got := containerResultID(t, ipv6Changed); got != containerID {
		t.Fatalf("IPv6 reconnect recreated container: before=%s after=%s", containerID, got)
	}
	assertNetworkEndpoint(t, client, name, network, ipv4B, ipv6B, "first-alias", linked+":linked-alias")

	aliasChanged := runContainerStateTask(t, client, "network-endpoint-alias", networkArgs(ipv4B, ipv6B, "second-alias"), "--diff")
	assertChanged(t, aliasChanged, true)
	if got := containerResultID(t, aliasChanged); got != containerID {
		t.Fatalf("alias reconnect recreated container: before=%s after=%s", containerID, got)
	}
	assertNetworkEndpoint(t, client, name, network, ipv4B, ipv6B, "second-alias", linked+":linked-alias")
	assertChanged(t, runContainerStateTask(t, client, "network-endpoint-final-idempotent", networkArgs(ipv4B, ipv6B, "second-alias"), "--diff"), false)
}

func assertNetworkEndpoint(
	t *testing.T,
	client *ssh.Client,
	containerName, network, ipv4, ipv6, alias, link string,
) {
	t.Helper()
	raw := mustRemote(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+containerName)
	var networks map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &networks); err != nil {
		t.Fatalf("decode network endpoints: %v\n%s", err, raw)
	}
	endpoint, found := networks[network]
	if !found {
		t.Fatalf("network %s missing from %#v", network, networks)
	}
	ipam, _ := endpoint["IPAMConfig"].(map[string]any)
	if got, _ := ipam["IPv4Address"].(string); got != ipv4 {
		t.Fatalf("network IPv4 = %q, want %q", got, ipv4)
	}
	if got, _ := ipam["IPv6Address"].(string); got != ipv6 {
		t.Fatalf("network IPv6 = %q, want %q", got, ipv6)
	}
	aliases, _ := json.Marshal(endpoint["Aliases"])
	if !strings.Contains(string(aliases), alias) {
		t.Fatalf("network aliases = %q, want %q", aliases, alias)
	}
	links, _ := json.Marshal(endpoint["Links"])
	if !strings.Contains(string(links), link) {
		t.Fatalf("network links = %q, want %q", links, link)
	}
}
