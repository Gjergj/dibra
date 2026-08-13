//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlaybook_DockerContainerInfo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const containerName = "info-test-container"
	const presentResult = "/tmp/dibra-container-info-present.json"
	const missingResult = "/tmp/dibra-container-info-missing.json"
	resultTemplate := filepath.Join(t.TempDir(), "container-info-result.j2")
	if err := os.WriteFile(resultTemplate, []byte(`{{ container_info | to_json }}`), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "rm -f "+presentResult+" "+missingResult)
	remoteExec(t, client, "docker run -d --name "+containerName+" --label parity=container-info -e MODE=test alpine:latest sleep 3600")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	presentPlaybook := playbookHeader + `
  - name: Inspect the existing container
    community.docker.docker_container_info:
      name: ` + containerName + `
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
    register: container_info

  - name: Save the registered result for comparison
    template:
      src: ` + resultTemplate + `
      dest: ` + presentResult + `
`
	presentOutput := runPlaybook(t, presentPlaybook)
	if strings.Contains(presentOutput, "FAILED") {
		t.Fatalf("existing-container inspection failed: %s", presentOutput)
	}

	var registered map[string]interface{}
	presentJSON := remoteExec(t, client, "cat "+presentResult)
	if presentJSON == "" {
		t.Fatalf("registered result was not written: %s", presentOutput)
	}
	if err := json.Unmarshal([]byte(presentJSON), &registered); err != nil {
		t.Fatal(err)
	}
	if registered["changed"] != false || registered["exists"] != true {
		t.Fatalf("registered result = %#v", registered)
	}
	container, ok := registered["container"].(map[string]interface{})
	if !ok {
		t.Fatalf("registered container = %T, want object", registered["container"])
	}

	var dockerInspect []map[string]interface{}
	if err := json.Unmarshal([]byte(remoteExec(t, client, "docker inspect "+containerName)), &dockerInspect); err != nil {
		t.Fatal(err)
	}
	if len(dockerInspect) != 1 || !reflect.DeepEqual(container, dockerInspect[0]) {
		t.Fatalf("module result does not match docker inspect\nmodule: %#v\ndocker: %#v", container, dockerInspect)
	}

	missingPlaybook := playbookHeader + `
  - name: Inspect a missing container
    docker_container_info:
      name: definitely-missing-container-info
    register: container_info

  - name: Save the missing result
    template:
      src: ` + resultTemplate + `
      dest: ` + missingResult + `
`
	missingOutput := runPlaybook(t, missingPlaybook)
	if strings.Contains(missingOutput, "FAILED") {
		t.Fatalf("missing-container inspection failed: %s", missingOutput)
	}
	var missing map[string]interface{}
	missingJSON := remoteExec(t, client, "cat "+missingResult)
	if missingJSON == "" {
		t.Fatalf("missing-container result was not written: %s", missingOutput)
	}
	if err := json.Unmarshal([]byte(missingJSON), &missing); err != nil {
		t.Fatal(err)
	}
	containerValue, hasContainer := missing["container"]
	if missing["changed"] != false || missing["exists"] != false || !hasContainer || containerValue != nil {
		t.Fatalf("missing-container result = %#v", missing)
	}

	checkPlaybook := playbookHeader + `
  - name: Inspect in check and diff mode
    docker_container_info:
      name: ` + containerName + `
`
	for iteration := 0; iteration < 2; iteration++ {
		output := runPlaybookWithArgs(t, checkPlaybook, "--check", "--diff")
		if strings.Contains(output, "FAILED") || strings.Contains(output, "SKIPPED") || !strings.Contains(output, "OK") {
			t.Fatalf("read-only execution %d failed: %s", iteration+1, output)
		}
	}
}

// TestPlaybook_DockerImageInfo tests the docker_image_info module
func TestPlaybook_DockerImageInfo(t *testing.T) {
	t.Run("InspectExistingImage", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get alpine image info
    docker_image_info:
      name: alpine:latest
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("InspectNonExistentImage", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get non-existent image info
    docker_image_info:
      name: non-existent-image-xyz:v999
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success (not failure) for non-existent image: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK with an empty images result: %s", output)
		}
	})
}

// TestPlaybook_DockerNetworkInfo tests the docker_network_info module
func TestPlaybook_DockerNetworkInfo(t *testing.T) {
	// First create a network to inspect
	setupPlaybook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Create test network
    docker_network:
      name: info-test-network
      state: present
`
	runPlaybook(t, setupPlaybook)
	defer func() {
		cleanupPlaybook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Remove test network
    docker_network:
      name: info-test-network
      state: absent
`
		runPlaybook(t, cleanupPlaybook)
	}()

	t.Run("InspectExistingNetwork", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get network info
    docker_network_info:
      name: info-test-network
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("InspectNonExistentNetwork", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get non-existent network info
    docker_network_info:
      name: non-existent-network-xyz
`
		output := runPlaybook(t, playbook)
		// Should succeed with msg about not found
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success (not failure) for non-existent network: %s", output)
		}
		if !strings.Contains(output, "not found") {
			t.Errorf("Expected 'not found' message in output: %s", output)
		}
	})
}

// TestPlaybook_DockerVolumeInfo tests the docker_volume_info module
func TestPlaybook_DockerVolumeInfo(t *testing.T) {
	// First create a volume to inspect
	setupPlaybook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Create test volume
    docker_volume:
      name: info-test-volume
      state: present
`
	runPlaybook(t, setupPlaybook)
	defer func() {
		cleanupPlaybook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Remove test volume
    docker_volume:
      name: info-test-volume
      state: absent
`
		runPlaybook(t, cleanupPlaybook)
	}()

	t.Run("InspectExistingVolume", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get volume info
    docker_volume_info:
      name: info-test-volume
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("InspectNonExistentVolume", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get non-existent volume info
    docker_volume_info:
      name: non-existent-volume-xyz
`
		output := runPlaybook(t, playbook)
		// Should succeed with msg about not found
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success (not failure) for non-existent volume: %s", output)
		}
		if !strings.Contains(output, "not found") {
			t.Errorf("Expected 'not found' message in output: %s", output)
		}
	})
}

// TestPlaybook_DockerHostInfo tests the docker_host_info module
func TestPlaybook_DockerHostInfo(t *testing.T) {
	t.Run("GetBasicHostInfo", func(t *testing.T) {
		// Note: Need at least one field set (even false) because YAML parses empty value as nil
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get Docker host info
    docker_host_info:
      containers: false
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		// Check for successful execution (OK or CHANGED)
		if !strings.Contains(output, "OK") && !strings.Contains(output, "CHANGED") {
			t.Errorf("Expected OK or CHANGED in output: %s", output)
		}
	})

	t.Run("ExplicitConnectionOptions", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get Docker host info through an explicit socket
    docker_host_info:
      containers: false
      docker_host: unix:///var/run/docker.sock
      api_version: auto
      timeout: 15
      debug: true
      use_ssh_client: false
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected explicit connection options to succeed: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("GetHostInfoWithDiskUsage", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get Docker host info with disk usage
    docker_host_info:
      disk_usage: true
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		// Check for successful execution (OK or CHANGED)
		if !strings.Contains(output, "OK") && !strings.Contains(output, "CHANGED") {
			t.Errorf("Expected OK or CHANGED in output: %s", output)
		}
	})
}

// TestPlaybook_DockerSwarmInfo tests the docker_swarm_info module
func TestPlaybook_DockerSwarmInfo(t *testing.T) {
	t.Run("GetSwarmInfo_NotInSwarm", func(t *testing.T) {
		// Most test environments won't be in swarm mode
		// Note: Need at least one field set (even false) because YAML parses empty value as nil
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get swarm info
    docker_swarm_info:
      nodes: false
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success (even if not in swarm), got failure: %s", output)
		}
		// Parse response to check in_swarm field
		var resp map[string]interface{}
		// Find JSON in output
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "{") {
				if err := json.Unmarshal([]byte(line), &resp); err == nil {
					break
				}
			}
		}
		// Either in_swarm is present, or we got a valid response
		if resp != nil {
			_, hasInSwarm := resp["in_swarm"]
			if !hasInSwarm {
				t.Logf("Response does not contain in_swarm field: %v", resp)
			}
		}
	})
}
