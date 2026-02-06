//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerStackLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	stackName := "test-stack-ansible"
	composeFile := "/tmp/stack-compose.yml"

	// Ensure Swarm is active
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm for Stack test...")
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}

	// Cleanup potential leftovers
	remoteExec(t, client, "docker stack rm "+stackName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// Create compose file on remote
	composeContent := `version: "3.8"
services:
  web:
    image: alpine:latest
    command: ["sleep", "3000"]
    deploy:
      replicas: 1
`
	remoteExec(t, client, "cat > "+composeFile+" << 'EOF'\n"+composeContent+"EOF")

	// 1. Deploy Stack
	t.Log("Step 1: Deploy Stack")
	playbookDeploy := playbookHeader + `
  - name: Deploy Stack
    docker_stack:
      name: ` + stackName + `
      compose_file: ` + composeFile + `
      state: present
`
	output1 := runPlaybook(t, playbookDeploy)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Deploy Stack failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on deploy stack")
	}

	// Verify
	stackInfo := remoteExec(t, client, "docker stack ls --format '{{.Name}}'")
	if !strings.Contains(stackInfo, stackName) {
		t.Errorf("Stack not found. Have: %s", stackInfo)
	}

	// 2. Idempotency (stack deploy is not perfectly idempotent but should not fail)
	t.Log("Step 2: Idempotency (should not fail)")
	output2 := runPlaybook(t, playbookDeploy)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Re-deploy Stack failed: %s", output2)
	}

	// 3. Remove Stack
	t.Log("Step 3: Remove Stack")
	playbookRemove := playbookHeader + `
  - name: Remove Stack
    docker_stack:
      name: ` + stackName + `
      state: absent
`
	output3 := runPlaybook(t, playbookRemove)
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on remove stack")
	}

	// Verify Gone
	gone := remoteExec(t, client, "docker stack ls --format '{{.Name}}'")
	if strings.Contains(gone, stackName) {
		t.Errorf("Stack still exists: %s", gone)
	}
}
