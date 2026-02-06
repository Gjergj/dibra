//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerPrune(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// 1. Create a dangling image (build output of a Dockerfile)
	// We'll use a simple approach: pull an image, tag it, then remove the tag? No that removes the image.
	// We need to build an image without a tag or build -> tag -> remove tag.

	// Create a dummy Dockerfile
	remoteExec(t, client, "mkdir -p /tmp/prune_test")
	remoteExec(t, client, "echo 'FROM alpine:latest\nRUN echo prune_me > /test' > /tmp/prune_test/Dockerfile")

	// Build it
	remoteExec(t, client, "docker build -t prune-test-image /tmp/prune_test")

	// Now remove the tag so it becomes dangling?
	// Actually, if we remove the tag `docker rmi prune-test-image`, it removes the image if no other tags exist.
	// To make a dangling image, we usually build without a -t, or build a new image that replaces an old tag (making the old ID dangling).

	// Let's try: Build v1, tag it 'test:latest'. Build v2, tag it 'test:latest'. v1 becomes dangling.
	remoteExec(t, client, "echo 'FROM alpine:latest\nRUN echo v1 > /version' > /tmp/prune_test/Dockerfile")
	remoteExec(t, client, "docker build -t prune-test-target /tmp/prune_test")

	remoteExec(t, client, "echo 'FROM alpine:latest\nRUN echo v2 > /version' > /tmp/prune_test/Dockerfile")
	remoteExec(t, client, "docker build -t prune-test-target /tmp/prune_test")

	// Verify we have dangling images
	dangling := remoteExec(t, client, "docker images -f dangling=true -q")
	if strings.TrimSpace(dangling) == "" {
		t.Log("Warning: Failed to create dangling image for test. Skipping prune assertions.")
	} else {
		remoteExec(t, client, "rm -f /tmp/.dibra-agent") // Force update agent

		// 2. Run Prune Playbook
		t.Log("Step 2: Run Prune")
		playbookPrune := playbookHeader + `
  - name: Prune Dangling Images
    docker_prune:
      images: true
      images_filters:
        dangling: "true"
`
		output := runPlaybook(t, playbookPrune)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Prune failed: %s", output)
		}
		if !strings.Contains(output, "CHANGED") {
			t.Error("Expected CHANGED when pruning dangling images")
		}

		// Verify gone
		danglingAfter := remoteExec(t, client, "docker images -f dangling=true -q")
		if strings.TrimSpace(danglingAfter) != "" {
			t.Errorf("Dangling images still exist: %s", danglingAfter)
		}
	}

	// Cleanup
	remoteExec(t, client, "docker rmi prune-test-target || true")
	remoteExec(t, client, "rm -rf /tmp/prune_test")
}
