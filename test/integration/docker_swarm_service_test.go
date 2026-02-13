//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

func TestPlaybook_DockerSwarmServiceLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	svcName := "test-dibra-svc"

	// Setup: Clean env && Init Swarm
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}
	// Force update agent
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Create Service
	t.Log("Step 1: Create Service")
	playbookCreate := playbookHeader + `
  - name: Create Service
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      replicas: 1
      command: ["sleep", "3000"]
      publish:
        - published_port: 8089
          target_port: 80
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify
	svcInfo := remoteExec(t, client, "docker service ls --filter name="+svcName+" --format '{{.Replicas}}'") // e.g., "1/1"
	if !strings.Contains(svcInfo, "/1") {
		t.Errorf("Service replicas expectation mismatch. Got: %s", svcInfo)
	}

	// 2. Scale Service
	t.Log("Step 2: Scale Service")
	playbookScale := playbookHeader + `
  - name: Scale Service
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      replicas: 2
      command: ["sleep", "3000"]
`
	output2 := runPlaybook(t, playbookScale)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Scale failed: %s", output2)
	}
	if !strings.Contains(output2, "CHANGED") {
		t.Error("Expected CHANGED on scale")
	}

	// Verify Scale
	time.Sleep(2 * time.Second) // Give it a moment to update spec
	svcInfoScale := remoteExec(t, client, "docker service inspect "+svcName+" --format '{{.Spec.Mode.Replicated.Replicas}}'")
	if strings.TrimSpace(svcInfoScale) != "2" {
		t.Errorf("Service not scaled to 2. Got: %s", svcInfoScale)
	}

	// 3. Idempotency
	t.Log("Step 3: Idempotency")
	output3 := runPlaybook(t, playbookScale)
	if strings.Contains(output3, "CHANGED") {
		t.Error("Expected no changes on idempotency run")
	}

	// 4. Remove Service
	t.Log("Step 4: Remove Service")
	playbookRemove := playbookHeader + `
  - name: Remove Service
    docker_swarm_service:
      name: ` + svcName + `
      state: absent
`
	output4 := runPlaybook(t, playbookRemove)
	if strings.Contains(output4, "FAILED") {
		t.Fatalf("Remove failed: %s", output4)
	}
	if !strings.Contains(output4, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify Gone
	svcGone := remoteExec(t, client, "docker service ls --filter name="+svcName+" -q")
	if strings.TrimSpace(svcGone) != "" {
		t.Errorf("Service %s still exists", svcName)
	}

	// Cleanup: Leave Swarm (optional, but keeps test env clean)
	// remoteExec(t, client, "docker swarm leave --force")
}
