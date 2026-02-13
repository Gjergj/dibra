//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerSecretLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	secretName := "test-secret-dibra"

	// Ensure Swarm is active (init if needed)
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm for Secret test...")
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}

	// Cleanup potential leftovers
	remoteExec(t, client, "docker secret rm "+secretName+" || true")

	// Force update agent
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Create Secret
	t.Log("Step 1: Create Secret")
	playbookCreate := playbookHeader + `
  - name: Create Secret
    docker_secret:
      name: ` + secretName + `
      data: "supersecurepassword"
      state: present
      labels:
        env: test
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create Secret failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on create secret")
	}

	// Verify
	// Note: We can't view secret data, but we can check checking labels and existence
	secInfo := remoteExec(t, client, "docker secret inspect "+secretName+" --format '{{.Spec.Labels.env}}'")
	if strings.TrimSpace(secInfo) != "test" {
		t.Errorf("Secret label not set. Got: %s", secInfo)
	}

	// 2. Idempotency
	t.Log("Step 2: Idempotency")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on idempotency run (same data/labels)")
	}

	// 3. Update Labels
	t.Log("Step 3: Update Labels")
	playbookUpdate := playbookHeader + `
  - name: Update Secret Labels
    docker_secret:
      name: ` + secretName + `
      data: "supersecurepassword"
      state: present
      labels:
        env: prod
        owner: devops
`
	output3 := runPlaybook(t, playbookUpdate)
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on label update")
	}

	// Verify Labels
	newLabels := remoteExec(t, client, "docker secret inspect "+secretName+" --format '{{.Spec.Labels.env}}:{{.Spec.Labels.owner}}'")
	if strings.TrimSpace(newLabels) != "prod:devops" {
		t.Errorf("Labels not updated. Got: %s", newLabels)
	}

	// 4. Force Update (Change Data requiring replacement if we implement check, but here just Force=true)
	// Changing data without force usually does nothing if we stick to same name and it exists (state present logic).
	// But if we pass `force: true`, it should recreate.
	t.Log("Step 4: Force Recreate")
	playbookForce := playbookHeader + `
  - name: Force Recreate Secret
    docker_secret:
      name: ` + secretName + `
      data: "newpassword"
      force: true
      state: present
`
	output4 := runPlaybook(t, playbookForce)
	if !strings.Contains(output4, "CHANGED") {
		t.Error("Expected CHANGED on force recreate")
	}

	// 5. Remove Secret
	t.Log("Step 5: Remove Secret")
	playbookRemove := playbookHeader + `
  - name: Remove Secret
    docker_secret:
      name: ` + secretName + `
      state: absent
`
	output5 := runPlaybook(t, playbookRemove)
	if !strings.Contains(output5, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify Gone
	gone := remoteExec(t, client, "docker secret ls --filter name="+secretName+" -q")
	if strings.TrimSpace(gone) != "" {
		t.Errorf("Secret still exists: %s", gone)
	}
}
