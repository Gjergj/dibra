//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerComposeLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Setup: Create a temp dir with docker-compose.yml
	projectDir := "/tmp/dibra-compose-test"
	remoteExec(t, client, "mkdir -p "+projectDir)

	// Simple compose file
	composeFile := `version: "3"
services:
  web:
    image: alpine:latest
    command: ["sleep", "3000"]
    platform: linux/amd64
`
	remoteExec(t, client, "echo '"+composeFile+"' > "+projectDir+"/docker-compose.yml")

	// Ensure clean state
	remoteExec(t, client, "docker compose -f "+projectDir+"/docker-compose.yml down || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Up (Present)
	t.Log("Step 1: Compose Up")
	playbookUp := playbookHeader + `
  - name: Compose Up
    docker_compose:
      project_src: ` + projectDir + `
      state: present
      build: false
      pull: false
`
	output1 := runPlaybook(t, playbookUp)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Compose Up failed: %s", output1)
	}
	// Note: 'docker compose up' stdout/stderr handling determines if we detect change.
	// We expect "Started" or "Created" in output.
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on connect/up")
	}

	// Verify
	// docker compose ps -q
	ps := remoteExec(t, client, "cd "+projectDir+" && docker compose ps -q")
	if strings.TrimSpace(ps) == "" {
		t.Error("No containers running for compose project")
	}

	// 2. Idempotency (Up again)
	t.Log("Step 2: Idempotency")
	output2 := runPlaybook(t, playbookUp)
	if strings.Contains(output2, "CHANGED") {
		// docker compose might say "Container ... Running" which we might interpret as changed if not careful?
		// Our executor checks for "created", "started", "recreated". "Running" is not in our list.
		// So it should be idempotent.
		t.Error("Expected no changes on second up")
	}

	// 3. Down (Absent)
	t.Log("Step 3: Compose Down")
	playbookDown := playbookHeader + `
  - name: Compose Down
    docker_compose:
      project_src: ` + projectDir + `
      state: absent
`
	output3 := runPlaybook(t, playbookDown)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Compose Down failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on down")
	}

	// Verify Gone
	psGone := remoteExec(t, client, "cd "+projectDir+" && docker compose ps -q")
	if strings.TrimSpace(psGone) != "" {
		t.Errorf("Containers still exist: %s", psGone)
	}

	// Cleanup
	remoteExec(t, client, "rm -rf "+projectDir)
}
