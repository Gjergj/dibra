//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerContainerExec(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "exec-test-container"

	// Clean up and create test container
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "docker run -d --name "+containerName+" alpine:latest sleep 3600")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent")

	// 1. Execute command
	t.Log("Step 1: Execute simple command")
	playbook1 := playbookHeader + `
  - name: Run echo command
    docker_container_exec:
      container: ` + containerName + `
      command: echo hello world
`
	output1 := runPlaybook(t, playbook1)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Exec failed: %s", output1)
	}
	if !strings.Contains(output1, "hello world") {
		t.Errorf("Expected 'hello world' in output: %s", output1)
	}

	// 2. Execute with argv
	t.Log("Step 2: Execute with argv")
	playbook2 := playbookHeader + `
  - name: Create file in container
    docker_container_exec:
      container: ` + containerName + `
      argv:
        - /bin/sh
        - -c
        - "echo test > /tmp/testfile.txt && cat /tmp/testfile.txt"
`
	output2 := runPlaybook(t, playbook2)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Exec with argv failed: %s", output2)
	}
}

func TestPlaybook_DockerContainerCopyInto(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "copy-test-container"

	// Clean up and create test container
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "docker run -d --name "+containerName+" alpine:latest sleep 3600")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent")

	// 1. Copy content into container
	t.Log("Step 1: Copy content into container")
	playbook1 := playbookHeader + `
  - name: Copy content to container
    docker_container_copy_into:
      container: ` + containerName + `
      content: "Hello from GoAnsible!"
      container_path: /tmp/hello.txt
      mode: "0644"
`
	output1 := runPlaybook(t, playbook1)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Copy content failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on copy content")
	}

	// Verify file exists
	fileContent := remoteExec(t, client, "docker exec "+containerName+" cat /tmp/hello.txt")
	if !strings.Contains(fileContent, "Hello from GoAnsible!") {
		t.Errorf("File content mismatch, got: %s", fileContent)
	}

	// 2. Copy base64 content
	t.Log("Step 2: Copy base64 content")
	playbook2 := playbookHeader + `
  - name: Copy base64 content to container
    docker_container_copy_into:
      container: ` + containerName + `
      content: "QmluYXJ5IGRhdGEgdGVzdA=="
      content_is_b64: true
      container_path: /tmp/binary.txt
`
	output2 := runPlaybook(t, playbook2)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Copy base64 content failed: %s", output2)
	}

	// Verify
	b64Content := remoteExec(t, client, "docker exec "+containerName+" cat /tmp/binary.txt")
	if !strings.Contains(b64Content, "Binary data test") {
		t.Errorf("Base64 content mismatch, got: %s", b64Content)
	}
}
