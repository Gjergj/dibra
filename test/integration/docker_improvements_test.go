//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerPrivilegedExec(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "privileged-test-container"

	// Clean up and create test container
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "docker run -d --name "+containerName+" alpine:latest sleep 3600")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Execute sysctl with privileged: true")
	playbook := playbookHeader + `
  - name: Try to set sysctl with privileges
    docker_container_exec:
      container: ` + containerName + `
      command: sysctl -w net.ipv4.ip_forward=1
      privileged: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Privileged exec failed: %s", output)
	}

	// Verify it's set
	val := remoteExec(t, client, "docker exec "+containerName+" sysctl -n net.ipv4.ip_forward")
	if strings.TrimSpace(val) != "1" {
		t.Errorf("Expected sysctl net.ipv4.ip_forward to be 1, got: %q", val)
	}
}

func TestPlaybook_DockerCopyOwnership(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "ownership-test-container"

	// Clean up and create test container with a non-root user
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	// Use alpine and run as nobody (UID 65534)
	remoteExec(t, client, "docker run -d --name "+containerName+" --user nobody alpine:latest sleep 3600")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Copy file without specifying owner/group")
	playbook := playbookHeader + `
  - name: Copy file to container as non-root
    docker_container_copy_into:
      container: ` + containerName + `
      content: "Ownership test"
      container_path: /tmp/owned.txt
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Copy failed: %s", output)
	}

	// Verify ownership is 65534:65534
	stat := remoteExec(t, client, "docker exec "+containerName+" stat -c '%u:%g' /tmp/owned.txt")
	if !strings.Contains(stat, "65534:65534") {
		t.Errorf("Expected ownership 65534:65534, got: %s", stat)
	}
}
