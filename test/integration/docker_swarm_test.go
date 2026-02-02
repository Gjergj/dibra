//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerSwarmLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Ensure clean state (Force leave if in swarm)
	// We check if swarm is active first to avoid error on leave
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) == "active" {
		remoteExec(t, client, "docker swarm leave --force")
	}
	remoteExec(t, client, "rm -f /tmp/.goansible-agent") // Force update agent

	// 1. Init Swarm
	t.Log("Step 1: Init Swarm")
	playbookInit := playbookHeader + `
  - name: Init Swarm
    docker_swarm:
      state: present
      advertise_addr: 127.0.0.1
`
	output1 := runPlaybook(t, playbookInit)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Init failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on init")
	}

	// Verify
	infoAfter := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if !strings.Contains(infoAfter, "active") {
		t.Errorf("Swarm not active after init. Got: %s", infoAfter)
	}

	// 2. Idempotency (Init again)
	t.Log("Step 2: Idempotency")
	output2 := runPlaybook(t, playbookInit)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second init")
	}

	// 3. Leave Swarm
	t.Log("Step 3: Leave Swarm")
	playbookLeave := playbookHeader + `
  - name: Leave Swarm
    docker_swarm:
      state: absent
      force: true
`
	output3 := runPlaybook(t, playbookLeave)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Leave failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on leave")
	}

	// Verify removal
	infoLeft := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(infoLeft) == "active" {
		t.Errorf("Swarm still active after leave. Got: %s", infoLeft)
	}
}
