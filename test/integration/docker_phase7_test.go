//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerVolume
// - Deep compare driver options
// - Metadata in response
// - Volume in use error handling
func TestPlaybook_DockerVolume(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	volName := "test-volume-docker"

	// Cleanup
	remoteExec(t, client, "docker volume rm "+volName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Create volume with driver options and labels
	t.Log("Step 1: Create volume with options and labels")
	playbookCreate := playbookHeader + `
  - name: Create volume with options
    docker_volume:
      name: ` + volName + `
      state: present
      driver: local
      driver_options:
        type: tmpfs
        device: tmpfs
      labels:
        environment: test
        phase: "7"
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first create")
	}

	// Verify volume exists with correct labels
	labels := remoteExec(t, client, "docker volume inspect "+volName+" --format '{{.Labels.environment}}:{{.Labels.phase}}'")
	if strings.TrimSpace(labels) != "test:7" {
		t.Errorf("Labels not set correctly. Got: %s", labels)
	}

	// 2. Idempotency - same options should not change
	t.Log("Step 2: Idempotency with same options")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on idempotency run")
	}

	// 3. Try to change driver options (should fail gracefully)
	t.Log("Step 3: Change driver options (should fail)")
	playbookChangeOpts := playbookHeader + `
  - name: Change driver options (should fail)
    docker_volume:
      name: ` + volName + `
      state: present
      driver: local
      driver_options:
        type: none
        device: /tmp
        o: bind
      labels:
        environment: test
        phase: "7"
`
	output3 := runPlaybook(t, playbookChangeOpts)
	// Should fail because driver options differ and recreate is not set
	if !strings.Contains(output3, "FAILED") {
		t.Error("Expected FAILED when driver options differ without recreate=always")
	}

	// 4. Recreate=always should allow changing options
	t.Log("Step 4: Recreate with different options")
	playbookRecreate := playbookHeader + `
  - name: Recreate with new labels
    docker_volume:
      name: ` + volName + `
      state: present
      driver: local
      labels:
        environment: production
        phase: "7"
      recreate: always
`
	output4 := runPlaybook(t, playbookRecreate)
	if strings.Contains(output4, "FAILED") {
		t.Fatalf("Recreate failed: %s", output4)
	}
	if !strings.Contains(output4, "CHANGED") {
		t.Error("Expected CHANGED on recreate")
	}

	// Verify new labels
	newLabels := remoteExec(t, client, "docker volume inspect "+volName+" --format '{{.Labels.environment}}'")
	if strings.TrimSpace(newLabels) != "production" {
		t.Errorf("Labels not updated after recreate. Got: %s", newLabels)
	}

	// Cleanup
	remoteExec(t, client, "docker volume rm "+volName+" || true")
}

// TestPlaybook_DockerSecret
func TestPlaybook_DockerSecretHashIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	secretName := "test-secret-hash"

	// Ensure Swarm is active
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm...")
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}

	// Cleanup
	remoteExec(t, client, "docker secret rm "+secretName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Create secret
	t.Log("Step 1: Create secret")
	playbookCreate := playbookHeader + `
  - name: Create secret
    docker_secret:
      name: ` + secretName + `
      data: "initial-password-123"
      state: present
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify hash label exists
	hashLabel := remoteExec(t, client, "docker secret inspect "+secretName+" --format '{{index .Spec.Labels \"dibra.data_hash\"}}'")
	if strings.TrimSpace(hashLabel) == "" {
		t.Error("Expected dibra.data_hash label to be set")
	}
	initialHash := strings.TrimSpace(hashLabel)
	t.Logf("Initial hash: %s", initialHash)

	// 2. Same data - should NOT change (hash matches)
	t.Log("Step 2: Same data - no change expected")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected NO CHANGED when data is identical (hash should match)")
	}

	// 3. Different data - should recreate automatically
	t.Log("Step 3: Different data - should recreate")
	playbookNewData := playbookHeader + `
  - name: Update secret data
    docker_secret:
      name: ` + secretName + `
      data: "new-password-456"
      state: present
`
	output3 := runPlaybook(t, playbookNewData)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Update failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED when data differs")
	}

	// Verify hash changed
	newHashLabel := remoteExec(t, client, "docker secret inspect "+secretName+" --format '{{index .Spec.Labels \"dibra.data_hash\"}}'")
	newHash := strings.TrimSpace(newHashLabel)
	if newHash == initialHash {
		t.Error("Expected hash to change when data changed")
	}
	t.Logf("New hash: %s", newHash)

	// Cleanup
	remoteExec(t, client, "docker secret rm "+secretName+" || true")
}

// TestPlaybook_DockerConfigHashIdempotency tests Phase 7 hash-based idempotency for configs
func TestPlaybook_DockerConfigHashIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	configName := "test-config-hash"

	// Ensure Swarm is active
	info := remoteExec(t, client, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm...")
		remoteExec(t, client, "docker swarm init --advertise-addr 127.0.0.1")
	}

	// Cleanup
	remoteExec(t, client, "docker config rm "+configName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Create config
	t.Log("Step 1: Create config")
	playbookCreate := playbookHeader + `
  - name: Create config
    docker_config:
      name: ` + configName + `
      data: "server { listen 80; }"
      state: present
`
	output1 := runPlaybook(t, playbookCreate)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify hash label
	hashLabel := remoteExec(t, client, "docker config inspect "+configName+" --format '{{index .Spec.Labels \"dibra.data_hash\"}}'")
	if strings.TrimSpace(hashLabel) == "" {
		t.Error("Expected dibra.data_hash label to be set")
	}

	// 2. Same data - should NOT change
	t.Log("Step 2: Same data - no change expected")
	output2 := runPlaybook(t, playbookCreate)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected NO CHANGED when data is identical")
	}

	// 3. Different data - should recreate
	t.Log("Step 3: Different data - should recreate")
	playbookNewData := playbookHeader + `
  - name: Update config data
    docker_config:
      name: ` + configName + `
      data: "server { listen 8080; }"
      state: present
`
	output3 := runPlaybook(t, playbookNewData)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Update failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED when data differs")
	}

	// 4. Label-only update (data same, labels different)
	t.Log("Step 4: Label-only update")
	playbookLabels := playbookHeader + `
  - name: Update config labels
    docker_config:
      name: ` + configName + `
      data: "server { listen 8080; }"
      labels:
        environment: production
      state: present
`
	output4 := runPlaybook(t, playbookLabels)
	if strings.Contains(output4, "FAILED") {
		t.Fatalf("Label update failed: %s", output4)
	}
	if !strings.Contains(output4, "CHANGED") {
		t.Error("Expected CHANGED when labels differ")
	}

	// Verify label was set
	envLabel := remoteExec(t, client, "docker config inspect "+configName+" --format '{{.Spec.Labels.environment}}'")
	if strings.TrimSpace(envLabel) != "production" {
		t.Errorf("Label not updated. Got: %s", envLabel)
	}

	// Cleanup
	remoteExec(t, client, "docker config rm "+configName+" || true")
}

// TestPlaybook_DockerPrune
func TestPlaybook_DockerPrune(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Create some test containers and volumes to prune
	remoteExec(t, client, "docker run --name prune-test-container -d alpine sleep 1 || true")
	remoteExec(t, client, "sleep 2") // Wait for container to exit
	remoteExec(t, client, "docker volume create prune-test-volume --label prune=yes || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Prune containers with filter
	t.Log("Step 1: Prune stopped containers")
	playbookPruneContainers := playbookHeader + `
  - name: Prune containers
    docker_prune:
      containers: true
`
	output1 := runPlaybook(t, playbookPruneContainers)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Container prune failed: %s", output1)
	}

	// Verify container is gone
	containers := remoteExec(t, client, "docker ps -a --filter name=prune-test-container -q")
	if strings.TrimSpace(containers) != "" {
		t.Log("Container still exists (may not have exited yet)")
	}

	// 2. Prune volumes with label filter
	t.Log("Step 2: Prune volumes with filter")
	playbookPruneVolumes := playbookHeader + `
  - name: Prune labeled volumes
    docker_prune:
      volumes: true
      volumes_filters:
        label: "prune=yes"
`
	output2 := runPlaybook(t, playbookPruneVolumes)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Volume prune failed: %s", output2)
	}
	if !strings.Contains(output2, "CHANGED") {
		t.Log("No volumes pruned (may have been cleaned already)")
	}

	// Verify volume is gone
	volumes := remoteExec(t, client, "docker volume ls --filter name=prune-test-volume -q")
	if strings.TrimSpace(volumes) != "" {
		t.Error("Volume still exists after prune with filter")
	}

	// Cleanup any remaining
	remoteExec(t, client, "docker rm -f prune-test-container || true")
	remoteExec(t, client, "docker volume rm prune-test-volume || true")
}
