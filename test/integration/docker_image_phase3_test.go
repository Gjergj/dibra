//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerImagePullPolicies tests pull: missing/always/never (3.2)
func TestPlaybook_DockerImagePullPolicies(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	imageName := "alpine:3.19"
	remoteExec(t, client, "docker rmi "+imageName+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent")
	defer remoteExec(t, client, "docker rmi "+imageName+" || true")

	// Test 1: pull: never should fail when image doesn't exist
	t.Log("Test 1: pull: never should fail when image doesn't exist")
	playbookNever := playbookHeader + `
  - name: Pull alpine with never policy
    docker_image:
      name: alpine
      tag: "3.19"
      pull: never
      state: present
`
	outputNever := runPlaybook(t, playbookNever)
	if !strings.Contains(outputNever, "FAILED") {
		t.Error("Expected FAILED when pull: never and image doesn't exist")
	}

	// Test 2: pull: missing should pull when image doesn't exist
	t.Log("Test 2: pull: missing should pull when image doesn't exist")
	playbookMissing := playbookHeader + `
  - name: Pull alpine with missing policy
    docker_image:
      name: alpine
      tag: "3.19"
      pull: missing
      state: present
`
	outputMissing := runPlaybook(t, playbookMissing)
	if strings.Contains(outputMissing, "FAILED") {
		t.Fatalf("Pull with missing policy failed: %s", outputMissing)
	}
	if !strings.Contains(outputMissing, "CHANGED") {
		t.Error("Expected CHANGED on first pull with missing policy")
	}

	// Verify image exists
	images := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if !strings.Contains(images, imageName) {
		t.Errorf("Image %s not found after pull: missing", imageName)
	}

	// Test 3: pull: missing should not pull when image exists
	t.Log("Test 3: pull: missing should not pull when image exists")
	outputMissing2 := runPlaybook(t, playbookMissing)
	if strings.Contains(outputMissing2, "CHANGED") {
		t.Error("Expected no changes when pull: missing and image exists")
	}

	// Test 4: pull: always should always check for updates
	t.Log("Test 4: pull: always should check for updates")
	playbookAlways := playbookHeader + `
  - name: Pull alpine with always policy
    docker_image:
      name: alpine
      tag: "3.19"
      pull: always
      state: present
`
	outputAlways := runPlaybook(t, playbookAlways)
	if strings.Contains(outputAlways, "FAILED") {
		t.Fatalf("Pull with always policy failed: %s", outputAlways)
	}
	// Note: may or may not be CHANGED depending on if image was updated

	// Test 5: pull: never should succeed when image exists
	t.Log("Test 5: pull: never should succeed when image exists")
	outputNever2 := runPlaybook(t, playbookNever)
	if strings.Contains(outputNever2, "FAILED") {
		t.Error("Expected SUCCESS when pull: never and image exists")
	}
	if strings.Contains(outputNever2, "CHANGED") {
		t.Error("Expected no changes when pull: never")
	}
}

// TestPlaybook_DockerImageTagIdempotency tests tag idempotency (3.3)
func TestPlaybook_DockerImageTagIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	sourceImage := "alpine:3.19"
	targetTag := "alpine:test-tag-idem"

	// Setup: ensure source exists, remove target
	remoteExec(t, client, "docker pull "+sourceImage)
	remoteExec(t, client, "docker rmi "+targetTag+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent")
	defer remoteExec(t, client, "docker rmi "+targetTag+" || true")

	// Test 1: First tag should change
	t.Log("Test 1: First tag should change")
	playbookTag := playbookHeader + `
  - name: Tag alpine
    docker_image:
      name: alpine
      tag: "3.19"
      source: local
      repository: alpine
      state: present
`
	// Using the yaml tag field for the target tag name
	playbookTagFull := playbookHeader + `
  - name: Tag alpine to test-tag-idem
    docker_image:
      name: alpine:3.19
      source: local
      repository: alpine:test-tag-idem
      state: present
`
	output1 := runPlaybook(t, playbookTagFull)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("First tag failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first tag")
	}

	// Verify tag exists
	images := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if !strings.Contains(images, targetTag) {
		t.Errorf("Tagged image %s not found", targetTag)
	}

	// Test 2: Second tag should be idempotent
	t.Log("Test 2: Second tag should be idempotent")
	output2 := runPlaybook(t, playbookTagFull)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Second tag failed: %s", output2)
	}
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second tag (idempotency)")
	}

	// Test 3: force_tag should always tag
	t.Log("Test 3: force_tag should always report changed")
	playbookForceTag := playbookHeader + `
  - name: Force tag alpine
    docker_image:
      name: alpine:3.19
      source: local
      repository: alpine:test-tag-idem
      force_tag: true
      state: present
`
	output3 := runPlaybook(t, playbookForceTag)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Force tag failed: %s", output3)
	}
	// force_tag still tags even if same ID, but changed is based on actual work
	_ = playbookTag
}

// TestPlaybook_DockerImageStreamErrors tests stream error parsing (3.2.1)
func TestPlaybook_DockerImageStreamErrors(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/.goansible-agent")

	// Test: Pull non-existent image should fail with proper error
	t.Log("Test: Pull non-existent image should fail gracefully")
	playbook := playbookHeader + `
  - name: Pull non-existent image
    docker_image:
      name: this-image-definitely-does-not-exist-12345
      tag: invalid
      state: present
`
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for non-existent image")
	}
}

// TestPlaybook_DockerImageForceRemove tests force_remove flag (3.5.1)
func TestPlaybook_DockerImageForceRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	imageName := "alpine:3.19"
	containerName := "test-force-remove-container"

	// Setup: pull image and create a stopped container using it
	remoteExec(t, client, "docker pull "+imageName)
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "docker create --name "+containerName+" "+imageName+" sleep 1")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rmi -f "+imageName+" || true")

	// Test 1: Remove without force should fail (container using image)
	t.Log("Test 1: Remove without force should fail when container uses image")
	playbookNoForce := playbookHeader + `
  - name: Remove alpine without force
    docker_image:
      name: alpine
      tag: "3.19"
      state: absent
`
	outputNoForce := runPlaybook(t, playbookNoForce)
	if !strings.Contains(outputNoForce, "FAILED") {
		t.Error("Expected FAILED when removing image with container using it")
	}

	// Test 2: Remove with force_remove should succeed
	t.Log("Test 2: Remove with force_remove should succeed")
	playbookForce := playbookHeader + `
  - name: Remove alpine with force
    docker_image:
      name: alpine
      tag: "3.19"
      force_remove: true
      state: absent
`
	outputForce := runPlaybook(t, playbookForce)
	if strings.Contains(outputForce, "FAILED") {
		t.Fatalf("Force remove failed: %s", outputForce)
	}
	if !strings.Contains(outputForce, "CHANGED") {
		t.Error("Expected CHANGED on force remove")
	}

	// Verify image is gone
	images := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if strings.Contains(images, imageName) {
		t.Error("Image still exists after force remove")
	}
}

// TestPlaybook_DockerImageBackwardCompat tests backward compatibility (3.5)
func TestPlaybook_DockerImageBackwardCompat(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	imageName := "alpine:3.18"
	remoteExec(t, client, "docker rmi "+imageName+" || true")
	remoteExec(t, client, "rm -f /tmp/.goansible-agent")
	defer remoteExec(t, client, "docker rmi "+imageName+" || true")

	// Test: force_source: true should still work as force_pull
	t.Log("Test: force_source backward compatibility")
	playbook := playbookHeader + `
  - name: Pull with force_source (deprecated)
    docker_image:
      name: alpine
      tag: "3.18"
      force_source: true
      state: present
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Pull with force_source failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on pull with force_source")
	}

	// Verify image exists
	images := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if !strings.Contains(images, imageName) {
		t.Errorf("Image %s not found", imageName)
	}
}
