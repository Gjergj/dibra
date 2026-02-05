//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerInfo tests the docker_container_info module
func TestPlaybook_DockerContainerInfo(t *testing.T) {
	// First create a container to inspect
	setupPlaybook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Create test container
    docker_container:
      name: info-test-container
      image: alpine:latest
      state: started
      command: ["sleep", "infinity"]
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
  - name: Remove test container
    docker_container:
      name: info-test-container
      state: absent
`
		runPlaybook(t, cleanupPlaybook)
	}()

	// Test inspecting the container
	t.Run("InspectExistingContainer", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get container info
    docker_container_info:
      name: info-test-container
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "failed") && strings.Contains(output, "true") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, `"exists":true`) && !strings.Contains(output, `"exists": true`) {
			t.Errorf("Expected exists: true in output: %s", output)
		}
	})

	t.Run("InspectNonExistentContainer", func(t *testing.T) {
		playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass

tasks:
  - name: Get container info for non-existent
    docker_container_info:
      name: non-existent-container-xyz
`
		output := runPlaybook(t, playbook)
		// Should succeed but with exists: false
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success (not failure) for non-existent container: %s", output)
		}
		if !strings.Contains(output, `"exists":false`) && !strings.Contains(output, `"exists": false`) {
			t.Errorf("Expected exists: false in output: %s", output)
		}
	})
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
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, `"exists":true`) && !strings.Contains(output, `"exists": true`) {
			t.Errorf("Expected exists: true in output: %s", output)
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
		// Should succeed but with exists: false
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success (not failure) for non-existent image: %s", output)
		}
		if !strings.Contains(output, `"exists":false`) && !strings.Contains(output, `"exists": false`) {
			t.Errorf("Expected exists: false in output: %s", output)
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
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, `"exists":true`) && !strings.Contains(output, `"exists": true`) {
			t.Errorf("Expected exists: true in output: %s", output)
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
		// Should succeed but with exists: false
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success (not failure) for non-existent network: %s", output)
		}
		if !strings.Contains(output, `"exists":false`) && !strings.Contains(output, `"exists": false`) {
			t.Errorf("Expected exists: false in output: %s", output)
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
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, `"exists":true`) && !strings.Contains(output, `"exists": true`) {
			t.Errorf("Expected exists: true in output: %s", output)
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
		// Should succeed but with exists: false
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success (not failure) for non-existent volume: %s", output)
		}
		if !strings.Contains(output, `"exists":false`) && !strings.Contains(output, `"exists": false`) {
			t.Errorf("Expected exists: false in output: %s", output)
		}
	})
}

// TestPlaybook_DockerHostInfo tests the docker_host_info module
func TestPlaybook_DockerHostInfo(t *testing.T) {
	t.Run("GetBasicHostInfo", func(t *testing.T) {
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
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success, got failure: %s", output)
		}
		// Check for expected fields
		if !strings.Contains(output, "server_version") {
			t.Errorf("Expected server_version in output: %s", output)
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
		if strings.Contains(output, `"failed":true`) || strings.Contains(output, `"failed": true`) {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "disk_usage") {
			t.Errorf("Expected disk_usage in output: %s", output)
		}
	})
}

// TestPlaybook_DockerSwarmInfo tests the docker_swarm_info module
func TestPlaybook_DockerSwarmInfo(t *testing.T) {
	t.Run("GetSwarmInfo_NotInSwarm", func(t *testing.T) {
		// Most test environments won't be in swarm mode
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
