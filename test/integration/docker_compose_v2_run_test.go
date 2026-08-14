//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerComposeV2Run(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Setup: Create a temp dir with docker-compose.yml
	projectDir := "/tmp/dibra-compose-v2-run-test"
	remoteExec(t, client, "mkdir -p "+projectDir)

	// Simple compose file
	composeFile := `services:
  app:
    image: alpine:latest
    command: ["sleep", "3000"]
`
	remoteExec(t, client, "echo '"+composeFile+"' > "+projectDir+"/docker-compose.yml")
	defer remoteExec(t, client, "rm -rf "+projectDir)

	// 1. Run (cleanup=true / --rm)
	t.Log("Step 1: Run echo with --rm")
	playbookRun := playbookHeader + `
  - name: Compose Run Echo
    docker_compose_v2_run:
      project_src: ` + projectDir + `
      service: app
      command: echo "hello world"
      cleanup: true
`
	output1 := runPlaybook(t, playbookRun)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Compose Run failed: %s", output1)
	}
	if !strings.Contains(output1, "hello world") {
		t.Errorf("Expected 'hello world' in output: %s", output1)
	}

	// 2. Run with argv and env
	t.Log("Step 2: Run with argv and env")
	playbookRun2 := playbookHeader + `
  - name: Compose Run Env
    docker_compose_v2_run:
      project_src: ` + projectDir + `
      service: app
      argv:
        - env
      env:
        MY_VAR: my_value
      cleanup: true
`
	output2 := runPlaybook(t, playbookRun2)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Compose Run failed: %s", output2)
	}
	if !strings.Contains(output2, "MY_VAR=my_value") {
		t.Errorf("Expected 'MY_VAR=my_value' in output: %s", output2)
	}

	// 3. Detach
	t.Log("Step 3: Run Detached")
	playbookRun3 := playbookHeader + `
  - name: Compose Run Detached
    docker_compose_v2_run:
      project_src: ` + projectDir + `
      service: app
      command: sleep 300
      detach: true
      name: test-detached-run
`
	output3 := runPlaybook(t, playbookRun3)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Compose Run Detached failed: %s", output3)
	}
	// Upstream detach returns only container_id; AnsibleModule then defaults
	// changed to false. Synchronous runs still report changed.

	// Verify it's running
	ps := remoteExec(t, client, "docker ps --filter name=test-detached-run --format '{{.Status}}'")
	if !strings.Contains(ps, "Up") {
		t.Errorf("Detached container not running. Status: %s", ps)
	}

	// Cleanup
	remoteExec(t, client, "docker rm -f test-detached-run || true")
}
