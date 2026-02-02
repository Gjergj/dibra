//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerContainerLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Ensure clean state
	remoteExec(t, client, "docker rm -f test-lifecycle-container || true")

	// 1. Create and Start
	t.Log("Step 1: Create and Start container")
	playbookCreate := playbookHeader + `
  - name: Create hello-world
    docker_container:
      name: test-lifecycle-container
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      pull: true
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first create")
	}

	// Verify independenly
	checkCmd := "docker ps --filter name=test-lifecycle-container --format '{{.Status}}'"
	status := remoteExec(t, client, checkCmd)
	if !strings.Contains(status, "Up") {
		t.Errorf("Container not up. Status: %s", status)
	}

	// 2. Idempotency (Run again)
	t.Log("Step 2: Idempotency check")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Idempotency run failed: %s", output2)
	}
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// 3. Remove
	t.Log("Step 3: Remove container")
	playbookRemove := playbookHeader + `
  - name: Remove hello-world
    docker_container:
      name: test-lifecycle-container
      state: absent
      force_kill: true
`
	output3 := runPlaybook(t, playbookRemove)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Remove failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify removal
	statusRemove := remoteExec(t, client, checkCmd)
	if statusRemove != "" {
		t.Errorf("Container still exists after removal. Status: %s", statusRemove)
	}

	// 4. Remove Idempotency
	t.Log("Step 4: Remove Idempotency check")
	output4 := runPlaybook(t, playbookRemove)
	if strings.Contains(output4, "CHANGED") {
		t.Error("Expected no changes on second remove")
	}
}
