//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerNetworkEnableIPv6 tests IPv6 network creation (Phase 4.2.1)
func TestPlaybook_DockerNetworkEnableIPv6(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-ipv6-net"

	// Cleanup
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	t.Log("Step 1: Create IPv6-enabled network")
	// Use unique subnets to avoid pool overlap with existing networks
	playbook := playbookHeader + `
  - name: Create IPv6 network
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      enable_ipv6: true
      ipam_config:
        - subnet: "10.128.0.0/16"
          gateway: "10.128.0.1"
        - subnet: "fd12:3456:789a::/64"
          gateway: "fd12:3456:789a::1"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify IPv6 is enabled
	inspect := remoteExec(t, client, "docker network inspect "+netName+" --format '{{.EnableIPv6}}'")
	if !strings.Contains(inspect, "true") {
		t.Errorf("Expected EnableIPv6=true, got: %s", inspect)
	}
}

// TestPlaybook_DockerNetworkConnectedContainers tests container connection management (Phase 4.1)
func TestPlaybook_DockerNetworkConnectedContainers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-connected-net"
	container1 := "test-connected-c1"
	container2 := "test-connected-c2"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+container1+" "+container2+" 2>/dev/null || true")
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	// Create test containers
	remoteExec(t, client, "docker run -d --name "+container1+" alpine sleep 3600")
	remoteExec(t, client, "docker run -d --name "+container2+" alpine sleep 3600")
	defer func() {
		remoteExec(t, client, "docker rm -f "+container1+" "+container2+" 2>/dev/null || true")
		remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	}()

	t.Log("Step 1: Create network and connect container1")
	playbook1 := playbookHeader + `
  - name: Create network with connected container
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      connected:
        - name: ` + container1 + `
          aliases:
            - web
            - frontend
`
	output := runPlaybook(t, playbook1)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify container is connected
	inspect := remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.Name}} {{end}}'")
	if !strings.Contains(inspect, container1) {
		t.Errorf("Container %s not connected. Got: %s", container1, inspect)
	}

	t.Log("Step 2: Add container2, keep container1 (appends=true)")
	playbook2 := playbookHeader + `
  - name: Add another container
    docker_network:
      name: ` + netName + `
      state: present
      appends: true
      connected:
        - name: ` + container2 + `
`
	output = runPlaybook(t, playbook2)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Add container failed: %s", output)
	}

	// Verify both containers are connected
	inspect = remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.Name}} {{end}}'")
	if !strings.Contains(inspect, container1) || !strings.Contains(inspect, container2) {
		t.Errorf("Both containers should be connected. Got: %s", inspect)
	}

	t.Log("Step 3: Replace with only container2 (appends=false)")
	playbook3 := playbookHeader + `
  - name: Replace connected containers
    docker_network:
      name: ` + netName + `
      state: present
      appends: false
      connected:
        - name: ` + container2 + `
`
	output = runPlaybook(t, playbook3)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Replace failed: %s", output)
	}

	// Verify only container2 is connected
	inspect = remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.Name}} {{end}}'")
	if strings.Contains(inspect, container1) {
		t.Errorf("Container1 should be disconnected. Got: %s", inspect)
	}
	if !strings.Contains(inspect, container2) {
		t.Errorf("Container2 should still be connected. Got: %s", inspect)
	}
}

// TestPlaybook_DockerNetworkStaticIP tests static IP assignment (Phase 4.1.5)
func TestPlaybook_DockerNetworkStaticIP(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-staticip-net"
	container := "test-staticip-container"
	staticIP := "172.29.0.100"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+container+" 2>/dev/null || true")
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	// Create test container
	remoteExec(t, client, "docker run -d --name "+container+" alpine sleep 3600")
	defer func() {
		remoteExec(t, client, "docker rm -f "+container+" 2>/dev/null || true")
		remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	}()

	t.Log("Create network and connect container with static IP")
	playbook := playbookHeader + `
  - name: Create network with static IP container
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      ipam_config:
        - subnet: "172.29.0.0/16"
          gateway: "172.29.0.1"
      connected:
        - name: ` + container + `
          ipv4_address: "` + staticIP + `"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}

	// Verify IP address
	inspect := remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .Containers}}{{.IPv4Address}}{{end}}'")
	if !strings.Contains(inspect, staticIP) {
		t.Errorf("Expected IP %s, got: %s", staticIP, inspect)
	}
}

// TestPlaybook_DockerNetworkIdempotency tests improved idempotency (Phase 4.3)
func TestPlaybook_DockerNetworkIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-idem-net"

	// Cleanup
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Create network with all options
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      attachable: true
      labels:
        env: test
        app: integration
      options:
        com.docker.network.bridge.enable_ip_masquerade: "true"
      ipam_config:
        - subnet: "172.30.0.0/16"
          gateway: "172.30.0.1"
`

	t.Log("Step 1: Create network")
	output1 := runPlaybook(t, playbook)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first run")
	}

	t.Log("Step 2: Run again - should be idempotent")
	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Second run failed: %s", output2)
	}
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

// TestPlaybook_DockerNetworkForceRecreate tests force recreation (Phase 4.3.4)
func TestPlaybook_DockerNetworkForceRecreate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-force-net"

	// Cleanup
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	t.Log("Step 1: Create network")
	playbook1 := playbookHeader + `
  - name: Create network
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      ipam_config:
        - subnet: "172.31.0.0/16"
`
	output := runPlaybook(t, playbook1)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}

	// Get original network ID
	origID := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+netName+" --format '{{.Id}}'"))

	t.Log("Step 2: Try to change immutable field without force - should fail")
	playbook2 := playbookHeader + `
  - name: Change IPAM (immutable)
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      ipam_config:
        - subnet: "172.32.0.0/16"
`
	output = runPlaybook(t, playbook2)
	if !strings.Contains(output, "FAILED") {
		t.Error("Expected failure when changing immutable field without force")
	}

	t.Log("Step 3: Force recreate with new config")
	playbook3 := playbookHeader + `
  - name: Force recreate with new IPAM
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      force: true
      ipam_config:
        - subnet: "172.32.0.0/16"
`
	output = runPlaybook(t, playbook3)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Force recreate failed: %s", output)
	}

	// Verify network was recreated (different ID)
	newID := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+netName+" --format '{{.Id}}'"))
	if origID == newID {
		t.Error("Network should have been recreated with new ID")
	}

	// Verify new IPAM
	subnet := remoteExec(t, client, "docker network inspect "+netName+" --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'")
	if !strings.Contains(subnet, "172.32.0.0") {
		t.Errorf("Expected new subnet 172.32.0.0/16, got: %s", subnet)
	}
}

// TestPlaybook_DockerNetworkIPAMDriver tests custom IPAM driver (Phase 4.2)
func TestPlaybook_DockerNetworkIPAMDriver(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-ipam-driver-net"

	// Cleanup
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	t.Log("Create network with explicit default IPAM driver")
	playbook := playbookHeader + `
  - name: Create network with IPAM driver
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      ipam_driver: default
      ipam_config:
        - subnet: "172.33.0.0/16"
          gateway: "172.33.0.1"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify network was created
	inspect := remoteExec(t, client, "docker network inspect "+netName+" --format '{{.IPAM.Driver}}'")
	if !strings.Contains(inspect, "default") {
		t.Errorf("Expected IPAM driver 'default', got: %s", inspect)
	}
}

// TestPlaybook_DockerNetworkAttachable tests attachable network (Phase 4.2)
func TestPlaybook_DockerNetworkAttachable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-attachable-net"

	// Cleanup
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	t.Log("Create attachable network")
	playbook := playbookHeader + `
  - name: Create attachable network
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      attachable: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}

	// Verify attachable flag
	inspect := remoteExec(t, client, "docker network inspect "+netName+" --format '{{.Attachable}}'")
	if !strings.Contains(inspect, "true") {
		t.Errorf("Expected Attachable=true, got: %s", inspect)
	}
}

// TestPlaybook_DockerNetworkInternal tests internal network (Phase 4.2)
func TestPlaybook_DockerNetworkInternal(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-internal-net"

	// Cleanup
	remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker network rm "+netName+" 2>/dev/null || true")

	t.Log("Create internal network")
	playbook := playbookHeader + `
  - name: Create internal network
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      internal: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create failed: %s", output)
	}

	// Verify internal flag
	inspect := remoteExec(t, client, "docker network inspect "+netName+" --format '{{.Internal}}'")
	if !strings.Contains(inspect, "true") {
		t.Errorf("Expected Internal=true, got: %s", inspect)
	}
}
