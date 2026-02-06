//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerConfigLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	configName := "test-config-ansible"

	// Ensure Swarm is active
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm for Config test...")
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}

	// Cleanup potential leftovers
	remoteExec(t, client, "docker config rm "+configName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Create Config
	t.Log("Step 1: Create Config")
	playbookCreate := playbookHeader + `
  - name: Create Config
    docker_config:
      name: ` + configName + `
      data: "server { listen 80; }"
      state: present
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create Config failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on create config")
	}

	// Verify
	cfgInfo := remoteExec(t, client, "docker config ls --filter name="+configName+" -q")
	if strings.TrimSpace(cfgInfo) == "" {
		t.Error("Config not found")
	}

	// 2. Idempotency
	t.Log("Step 2: Idempotency")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on idempotency run")
	}

	// 3. Remove Config
	t.Log("Step 3: Remove Config")
	playbookRemove := playbookHeader + `
  - name: Remove Config
    docker_config:
      name: ` + configName + `
      state: absent
`
	output3 := runPlaybook(t, playbookRemove)
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify Gone
	gone := remoteExec(t, client, "docker config ls --filter name="+configName+" -q")
	if strings.TrimSpace(gone) != "" {
		t.Errorf("Config still exists: %s", gone)
	}
}
