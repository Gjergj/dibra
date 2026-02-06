//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// ensureSwarmActive initializes swarm if not active
func ensureSwarmActive(t *testing.T, client interface{ Close() error }) {
	c := getClient(t)
	defer c.Close()
	info := remoteExec(t, c, "docker info --format '{{.Swarm.LocalNodeState}}'")
	if strings.TrimSpace(info) != "active" {
		t.Log("Initializing Swarm...")
		remoteExec(t, c, "docker swarm init --advertise-addr 127.0.0.1")
	}
	// Force update agent
	remoteExec(t, c, "rm -f /tmp/.dibra-agent")
}

// TestPlaybook_DockerSwarmServiceHealthcheck tests healthcheck configuration (6.3.1)
func TestPlaybook_DockerSwarmServiceHealthcheck(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)
	svcName := "test-healthcheck-svc"

	// Cleanup
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	defer remoteExec(t, client, "docker service rm "+svcName+" || true")

	// Create service with healthcheck
	playbook := playbookHeader + `
  - name: Create service with healthcheck
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      command: ["sleep", "3000"]
      healthcheck:
        test: ["CMD", "true"]
        interval: "10s"
        timeout: "5s"
        retries: 3
        start_period: "5s"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with healthcheck failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify healthcheck is set
	hcInfo := remoteExec(t, client, "docker service inspect "+svcName+" --format '{{.Spec.TaskTemplate.ContainerSpec.Healthcheck.Interval}}'")
	if !strings.Contains(hcInfo, "10") {
		t.Errorf("Healthcheck interval not set correctly. Got: %s", hcInfo)
	}
}

// TestPlaybook_DockerSwarmServiceDNS tests DNS configuration (6.3.2)
func TestPlaybook_DockerSwarmServiceDNS(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)
	svcName := "test-dns-svc"

	// Cleanup
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	defer remoteExec(t, client, "docker service rm "+svcName+" || true")

	// Create service with DNS config
	playbook := playbookHeader + `
  - name: Create service with DNS
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      command: ["sleep", "3000"]
      dns:
        - "8.8.8.8"
        - "8.8.4.4"
      dns_search:
        - "example.com"
      dns_options:
        - "ndots:5"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with DNS failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify DNS is set
	dnsInfo := remoteExec(t, client, "docker service inspect "+svcName+" --format '{{.Spec.TaskTemplate.ContainerSpec.DNSConfig.Nameservers}}'")
	if !strings.Contains(dnsInfo, "8.8.8.8") {
		t.Errorf("DNS nameserver not set correctly. Got: %s", dnsInfo)
	}
}

// TestPlaybook_DockerSwarmServiceMounts tests mounts configuration (6.3.4)
func TestPlaybook_DockerSwarmServiceMounts(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)
	svcName := "test-mounts-svc"

	// Cleanup
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	defer remoteExec(t, client, "docker service rm "+svcName+" || true")

	// Create service with tmpfs mount
	playbook := playbookHeader + `
  - name: Create service with mounts
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      command: ["sleep", "3000"]
      mounts:
        - type: tmpfs
          target: /tmp/mydata
          tmpfs_size: 67108864
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with mounts failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify mount is set
	mountInfo := remoteExec(t, client, "docker service inspect "+svcName+" --format '{{range .Spec.TaskTemplate.ContainerSpec.Mounts}}{{.Target}}{{end}}'")
	if !strings.Contains(mountInfo, "/tmp/mydata") {
		t.Errorf("Mount target not set correctly. Got: %s", mountInfo)
	}
}

// TestPlaybook_DockerSwarmServiceUpdateConfig tests update configuration (6.2)
func TestPlaybook_DockerSwarmServiceUpdateConfig(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)
	svcName := "test-update-config-svc"

	// Cleanup
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	defer remoteExec(t, client, "docker service rm "+svcName+" || true")

	// Create service with update config
	playbook := playbookHeader + `
  - name: Create service with update config
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      command: ["sleep", "3000"]
      update_delay: "10s"
      update_parallelism: 2
      update_failure_action: rollback
      update_order: start-first
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with update config failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify update config
	updateInfo := remoteExec(t, client, "docker service inspect "+svcName+" --format '{{.Spec.UpdateConfig.Parallelism}}'")
	if strings.TrimSpace(updateInfo) != "2" {
		t.Errorf("Update parallelism not set correctly. Got: %s", updateInfo)
	}

	orderInfo := remoteExec(t, client, "docker service inspect "+svcName+" --format '{{.Spec.UpdateConfig.Order}}'")
	if strings.TrimSpace(orderInfo) != "start-first" {
		t.Errorf("Update order not set correctly. Got: %s", orderInfo)
	}
}

// TestPlaybook_DockerSwarmServiceIdempotency tests improved idempotency (6.4)
func TestPlaybook_DockerSwarmServiceIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)
	svcName := "test-idempotent-svc"

	// Cleanup
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	defer remoteExec(t, client, "docker service rm "+svcName+" || true")

	// Create service
	playbook := playbookHeader + `
  - name: Create service
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      replicas: 1
      command: ["sleep", "3000"]
      env:
        FOO: bar
        BAZ: qux
      labels:
        app: test
`
	output1 := runPlaybook(t, playbook)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first run")
	}

	time.Sleep(time.Second)

	// Run again - should be idempotent
	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Idempotency run failed: %s", output2)
	}
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second run (idempotency)")
	}

	// Change replicas - should detect change
	playbookScaled := playbookHeader + `
  - name: Scale service
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      replicas: 2
      command: ["sleep", "3000"]
      env:
        FOO: bar
        BAZ: qux
      labels:
        app: test
`
	output3 := runPlaybook(t, playbookScaled)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Scale failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED when scaling")
	}
}

// TestPlaybook_DockerNodeLabelsToRemove tests label removal (6.5.3)
func TestPlaybook_DockerNodeLabelsToRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)

	// Get hostname
	hostname := strings.TrimSpace(remoteExec(t, client, "docker node ls --format '{{.Hostname}}' | head -n 1"))

	// First add some labels
	playbookAdd := playbookHeader + `
  - name: Add Labels
    docker_node:
      hostname: ` + hostname + `
      labels:
        env: test
        role: worker
        team: devops
`
	output1 := runPlaybook(t, playbookAdd)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Add labels failed: %s", output1)
	}

	// Verify labels added
	labelsInfo := remoteExec(t, client, "docker node inspect self --format '{{.Spec.Labels}}'")
	if !strings.Contains(labelsInfo, "env:test") {
		t.Errorf("Labels not added correctly. Got: %s", labelsInfo)
	}

	// Now remove specific labels
	playbookRemove := playbookHeader + `
  - name: Remove specific labels
    docker_node:
      hostname: ` + hostname + `
      labels_to_remove:
        - env
        - team
`
	output2 := runPlaybook(t, playbookRemove)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Remove labels failed: %s", output2)
	}
	if !strings.Contains(output2, "CHANGED") {
		t.Error("Expected CHANGED when removing labels")
	}

	// Verify specific labels removed but 'role' remains
	labelsAfter := remoteExec(t, client, "docker node inspect self --format '{{.Spec.Labels}}'")
	if strings.Contains(labelsAfter, "env:test") {
		t.Errorf("Label 'env' should have been removed. Got: %s", labelsAfter)
	}
	if strings.Contains(labelsAfter, "team:devops") {
		t.Errorf("Label 'team' should have been removed. Got: %s", labelsAfter)
	}
	if !strings.Contains(labelsAfter, "role:worker") {
		t.Errorf("Label 'role' should still exist. Got: %s", labelsAfter)
	}

	// Cleanup: remove remaining label
	playbookCleanup := playbookHeader + `
  - name: Clear all labels
    docker_node:
      hostname: ` + hostname + `
      labels: {}
      labels_state: replace
`
	runPlaybook(t, playbookCleanup)
}

// TestPlaybook_DockerSwarmServiceInfo tests swarm service info module (6.6)
func TestPlaybook_DockerSwarmServiceInfo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)
	svcName := "test-info-svc"

	// Cleanup and create a service
	remoteExec(t, client, "docker service rm "+svcName+" || true")
	defer remoteExec(t, client, "docker service rm "+svcName+" || true")

	// Create service first
	setupPlaybook := playbookHeader + `
  - name: Create service
    docker_swarm_service:
      name: ` + svcName + `
      image: alpine:latest
      state: present
      command: ["sleep", "3000"]
`
	runPlaybook(t, setupPlaybook)
	time.Sleep(time.Second)

	// Test service info
	t.Run("InspectExistingService", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Get service info
    docker_swarm_service_info:
      name: ` + svcName + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("InspectNonExistentService", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Get non-existent service info
    docker_swarm_service_info:
      name: non-existent-service-xyz
`
		output := runPlaybook(t, playbook)
		// Should succeed with 'not found' message
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success (not failure) for non-existent service: %s", output)
		}
		if !strings.Contains(output, "not found") {
			t.Errorf("Expected 'not found' message in output: %s", output)
		}
	})
}

// TestPlaybook_DockerNodeInfo tests node info module (6.7)
func TestPlaybook_DockerNodeInfo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ensureSwarmActive(t, client)

	// Get hostname
	hostname := strings.TrimSpace(remoteExec(t, client, "docker node ls --format '{{.Hostname}}' | head -n 1"))

	t.Run("InspectSelfNode", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Get self node info
    docker_node_info:
      self: true
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("InspectNodeByHostname", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Get node info by hostname
    docker_node_info:
      name: ` + hostname + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success, got failure: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK in output: %s", output)
		}
	})

	t.Run("InspectNonExistentNode", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Get non-existent node info
    docker_node_info:
      name: non-existent-node-xyz
`
		output := runPlaybook(t, playbook)
		// Should succeed with 'not found' message
		if strings.Contains(output, "FAILED") {
			t.Errorf("Expected success (not failure) for non-existent node: %s", output)
		}
		if !strings.Contains(output, "not found") {
			t.Errorf("Expected 'not found' message in output: %s", output)
		}
	})
}
