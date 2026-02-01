//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerVolumeLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	volName := "test-ansible-vol"

	// Ensure clean state
	remoteExec(t, client, "docker volume rm "+volName+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent") // Force update agent

	// 1. Create Volume
	t.Log("Step 1: Create Volume")
	playbookCreate := playbookHeader + `
  - name: Create volume
    docker_volume:
      name: ` + volName + `
      state: present
      driver: local
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
	vols := remoteExec(t, client, "docker volume ls --format '{{.Name}}'")
	if !strings.Contains(vols, volName) {
		t.Errorf("Volume %s not found in docker volume ls. Got: %s", volName, vols)
	}

	// 2. Idempotency (Create again)
	t.Log("Step 2: Idempotency")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second create")
	}

	// 3. Remove Volume
	t.Log("Step 3: Remove Volume")
	playbookRemove := playbookHeader + `
  - name: Remove volume
    docker_volume:
      name: ` + volName + `
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
	volsAfter := remoteExec(t, client, "docker volume ls --format '{{.Name}}'")
	if strings.Contains(volsAfter, volName) {
		t.Errorf("Volume %s still exists after removal", volName)
	}

	// 4. Remove Idempotency
	t.Log("Step 4: Remove Idempotency")
	output4 := runPlaybook(t, playbookRemove)
	if strings.Contains(output4, "CHANGED") {
		t.Error("Expected no changes on second remove")
	}
}
