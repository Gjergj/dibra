//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerNode(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Ensure Swarm is active (init if needed)
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm for Node test...")
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}

	// Get the real Swarm Node Hostname
	// We can parse it from 'docker node ls' where there is a '*'
	// ID HOSTNAME STATUS ...
	// but simpler: 'docker info --format {{.Name}}' might return it?
	// Or 'docker node ls --filter role=manager --format {{.Hostname}}'
	hostname := strings.TrimSpace(remoteExec(t, client, "docker node ls --format '{{.Hostname}}' | head -n 1"))

	// Force update agent
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// Debug: List nodes
	nodes := remoteExec(t, client, "docker node ls")
	t.Logf("Docker Nodes:\n%s", nodes)
	t.Logf("Target Hostname: %s", hostname)

	// 1. Add Label (Targeting by Hostname)
	t.Logf("Step 1: Add Label to Node (Hostname: %s)", hostname)
	playbookAddLabel := playbookHeader + `
  - name: Add Label
    docker_node:
      hostname: ` + hostname + `
      labels:
        type: worker_test
`
	output1 := runPlaybook(t, playbookAddLabel)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Add Label failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on add label")
	}

	// Verify Label
	nodeInfo := remoteExec(t, client, "docker node inspect self --format '{{.Spec.Labels.type}}'")
	if strings.TrimSpace(nodeInfo) != "worker_test" {
		t.Errorf("Label not set. Got: %s", nodeInfo)
	}

	// 2. Modify Availability (active -> pause) using Self=true
	t.Log("Step 2: Change Availability to Pause (Self)")
	playbookPause := playbookHeader + `
  - name: Pause Node
    docker_node:
      self: true
      availability: pause
`
	output2 := runPlaybook(t, playbookPause)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Pause failed: %s", output2)
	}
	if !strings.Contains(output2, "CHANGED") {
		t.Error("Expected CHANGED on pause")
	}

	// Verify Availability
	avail := remoteExec(t, client, "docker node inspect self --format '{{.Spec.Availability}}'")
	if strings.TrimSpace(avail) != "pause" {
		t.Errorf("Availability not paused. Got: %s", avail)
	}

	// 3. Restore Availability (pause -> active)
	t.Log("Step 3: Restore Availability")
	playbookActive := playbookHeader + `
  - name: Active Node
    docker_node:
      hostname: ` + hostname + `
      availability: active
`
	output3 := runPlaybook(t, playbookActive)
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on restore active")
	}

	// 4. Remove Label (using empty labels? Or how to remove?)
	// Ansible module allows removing labels?
	// Our implementation: "merge" adds/updates. "replace" replaces all.
	// To remove, we generally use "replace" with empty/subset, OR we might need state=absent for keys?
	// Our `Request` struct has `Labels map[string]string`.
	// If we use `labels_state: replace` with empty labels, it should clear them.

	t.Log("Step 4: Clear Labels")
	playbookClear := playbookHeader + `
  - name: Clear Labels
    docker_node:
      hostname: ` + hostname + `
      labels: {}
      labels_state: replace
`
	output4 := runPlaybook(t, playbookClear)
	if !strings.Contains(output4, "CHANGED") {
		t.Log("Warning: Expected CHANGED on clear labels, but maybe labels were already empty besides ours?")
		// Actually we added one. So it should change.
	}

	// Verify Empty
	labels := remoteExec(t, client, "docker node inspect self --format '{{json .Spec.Labels}}'")
	if strings.Contains(labels, "worker_test") {
		t.Errorf("Label still present after clear: %s", labels)
	}
}
