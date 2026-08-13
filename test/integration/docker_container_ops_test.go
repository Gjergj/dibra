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
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "docker run -d --name "+containerName+" alpine:latest sleep 3600")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	t.Log("Execute quoted command through the canonical module name")
	commandPlaybook := playbookHeader + `
  - name: Run quoted command
    community.docker.docker_container_exec:
      container: ` + containerName + `
      command: /bin/sh -c "printf 'hello world'"
      docker_url: unix:///var/run/docker.sock
      docker_api_version: auto
`
	commandOutput := runPlaybook(t, commandPlaybook)
	if strings.Contains(commandOutput, "FAILED") || !strings.Contains(commandOutput, "hello world") {
		t.Fatalf("quoted exec failed: %s", commandOutput)
	}

	t.Log("Execute argv with stdin defaults and explicit newline controls")
	stdinPlaybook := playbookHeader + `
  - name: Write stdin with the default newline
    docker_container_exec:
      container: ` + containerName + `
      argv: [/bin/sh, -c, "cat > /tmp/stdin-default"]
      stdin: Hello world!

  - name: Write stdin without a newline
    docker_container_exec:
      container: ` + containerName + `
      argv: [/bin/sh, -c, "cat > /tmp/stdin-no-newline"]
      stdin: Hello world!
      stdin_add_newline: false

  - name: Preserve output newlines
    docker_container_exec:
      container: ` + containerName + `
      argv: [/bin/sh, -c, "printf 'line\n\n'"]
      strip_empty_ends: false
`
	stdinOutput := runPlaybook(t, stdinPlaybook)
	if strings.Contains(stdinOutput, "FAILED") {
		t.Fatalf("stdin exec failed: %s", stdinOutput)
	}
	if size := remoteExec(t, client, "docker exec "+containerName+" sh -c 'wc -c < /tmp/stdin-default'"); size != "13" {
		t.Fatalf("default stdin newline size = %q, want 13", size)
	}
	if size := remoteExec(t, client, "docker exec "+containerName+" sh -c 'wc -c < /tmp/stdin-no-newline'"); size != "12" {
		t.Fatalf("explicit no-newline stdin size = %q, want 12", size)
	}

	t.Log("Pass environment, working directory, user, and TTY options")
	optionPlaybook := playbookHeader + `
  - name: Exercise execution options
    docker_container_exec:
      container: ` + containerName + `
      argv:
        - /bin/sh
        - -c
        - 'printf "%s" "$FOO" > env-result; pwd > pwd-result; id -u > uid-result'
      chdir: /tmp
      user: "65534"
      tty: true
      env:
        FOO: |-
          bar
          baz
`
	optionOutput := runPlaybook(t, optionPlaybook)
	if strings.Contains(optionOutput, "FAILED") {
		t.Fatalf("exec options failed: %s", optionOutput)
	}
	if got := remoteExec(t, client, "docker exec "+containerName+" cat /tmp/env-result"); got != "bar\nbaz" {
		t.Fatalf("environment value = %q", got)
	}
	if got := remoteExec(t, client, "docker exec "+containerName+" cat /tmp/pwd-result"); got != "/tmp" {
		t.Fatalf("working directory = %q", got)
	}
	if got := remoteExec(t, client, "docker exec "+containerName+" cat /tmp/uid-result"); got != "65534" {
		t.Fatalf("exec user = %q", got)
	}

	t.Log("Run detached and return before command completion")
	detachPlaybook := playbookHeader + `
  - name: Run detached command
    docker_container_exec:
      container: ` + containerName + `
      argv: [/bin/sh, -c, "sleep 1; echo detached > /tmp/detached-result"]
      detach: true
`
	detachOutput := runPlaybook(t, detachPlaybook)
	if strings.Contains(detachOutput, "FAILED") || !strings.Contains(detachOutput, "CHANGED") {
		t.Fatalf("detached exec failed: %s", detachOutput)
	}
	result := remoteExec(t, client, "for i in $(seq 1 20); do docker exec "+containerName+" test -f /tmp/detached-result && docker exec "+containerName+" cat /tmp/detached-result && exit 0; sleep 0.1; done; exit 1")
	if result != "detached" {
		t.Fatalf("detached command result = %q", result)
	}

	t.Log("Report the pinned missing-container error")
	missingPlaybook := playbookHeader + `
  - name: Execute in missing container
    docker_container_exec:
      container: definitely-missing-exec-container
      argv: ["true"]
`
	missingOutput := runPlaybook(t, missingPlaybook)
	if !strings.Contains(missingOutput, `Could not find container`) {
		t.Fatalf("missing-container error did not match upstream: %s", missingOutput)
	}

	t.Log("Skip execution in unsupported check mode")
	checkPlaybook := playbookHeader + `
  - name: Do not execute in check mode
    docker_container_exec:
      container: ` + containerName + `
      argv: [touch, /tmp/check-mode-must-not-exist]
`
	checkOutput := runPlaybookWithArgs(t, checkPlaybook, "--check")
	if strings.Contains(checkOutput, "FAILED") || !strings.Contains(checkOutput, "SKIPPED") {
		t.Fatalf("check-mode handling failed: %s", checkOutput)
	}
	if got := remoteExec(t, client, "docker exec "+containerName+" test ! -e /tmp/check-mode-must-not-exist && echo absent"); got != "absent" {
		t.Fatalf("check mode executed the command: %q", got)
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
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")

	// 1. Copy content into container
	t.Log("Step 1: Copy content into container")
	playbook1 := playbookHeader + `
  - name: Copy content to container
    docker_container_copy_into:
      container: ` + containerName + `
      content: "Hello from dibra!"
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
	if !strings.Contains(fileContent, "Hello from dibra!") {
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
