//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerImageLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	ctx := "busybox"
	tag := "latest"
	imageName := ctx + ":" + tag

	// Ensure clean state
	remoteExec(t, client, "docker rmi "+imageName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent") // Force update agent

	// 1. Pull Image
	t.Log("Step 1: Pull Image")
	playbookPull := playbookHeader + `
  - name: Pull busybox
    docker_image:
      name: ` + ctx + `
      tag: ` + tag + `
      source: pull
      state: present
`
	output1 := runPlaybook(t, playbookPull)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Pull failed: %s", output1)
	}
	if !strings.Contains(output1, "CHANGED") {
		t.Error("Expected CHANGED on first pull")
	}

	// Verify
	images := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if !strings.Contains(images, imageName) {
		t.Errorf("Image %s not found in docker images. Got: %s", imageName, images)
	}

	// 2. Idempotency (Pull again)
	t.Log("Step 2: Pull Idempotency")
	output2 := runPlaybook(t, playbookPull)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second pull")
	}

	// 3. Remove Image
	t.Log("Step 3: Remove Image")
	playbookRemove := playbookHeader + `
  - name: Remove busybox
    docker_image:
      name: ` + ctx + `
      tag: ` + tag + `
      state: absent
`
	output3 := runPlaybook(t, playbookRemove)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Remove failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED on remove")
	}

	// Verify removal
	imagesAfter := remoteExec(t, client, "docker images --format '{{.Repository}}:{{.Tag}}'")
	if strings.Contains(imagesAfter, imageName) {
		t.Errorf("Image %s still exists after removal", imageName)
	}

	// 4. Remove Idempotency
	t.Log("Step 4: Remove Idempotency")
	output4 := runPlaybook(t, playbookRemove)
	if strings.Contains(output4, "CHANGED") {
		t.Error("Expected no changes on second remove")
	}
}
