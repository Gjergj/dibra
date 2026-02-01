//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerNetworkLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	netName := "test-ansible-net"

	// Ensure clean state
	remoteExec(t, client, "docker network rm "+netName+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent") // Force update agent

	// 1. Create Network
	t.Log("Step 1: Create Network")
	playbookCreate := playbookHeader + `
  - name: Create network
    docker_network:
      name: ` + netName + `
      state: present
      driver: bridge
      labels:
        ansible_test: "true"
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first create")
	}

	// Verify
	nets := remoteExec(t, client, "docker network ls --format '{{.Name}}'")
	if !strings.Contains(nets, netName) {
		t.Errorf("Network %s not found in docker network ls. Got: %s", netName, nets)
	}

	// 2. Idempotency (Create again)
	t.Log("Step 2: Idempotency")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second create")
	}

	// 3. Remove Network
	t.Log("Step 3: Remove Network")
	playbookRemove := playbookHeader + `
  - name: Remove network
    docker_network:
      name: ` + netName + `
      state: absent
`
	output3 := runPlaybook(t, playbookRemove)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Remove failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify removal
	netsAfter := remoteExec(t, client, "docker network ls --format '{{.Name}}'")
	if strings.Contains(netsAfter, netName) {
		t.Errorf("Network %s still exists after removal", netName)
	}

	// 4. Remove Idempotency
	t.Log("Step 4: Remove Idempotency")
	output4 := runPlaybook(t, playbookRemove)
	if strings.Contains(output4, "CHANGED") {
		t.Error("Expected no changes on second remove")
	}
}
