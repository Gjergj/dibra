//go:build integration

package integration

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerImagePullParity independently ports the pinned
// community.docker options.yml pull and pull_check_mode_behavior matrix.
func TestPlaybook_DockerContainerImagePullParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		name  = "dibra-container-pull-parity"
		image = "quay.io/ansible/docker-test-containers:hello-world"
	)
	cleanupContainerPullFixture(t, client, name, image)
	defer cleanupContainerPullFixture(t, client, name, image)

	t.Run("never fails when image is missing", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Refuse a missing image
    community.docker.docker_container:
      name: ` + name + `
      image: ` + image + `
      state: present
      pull: never
      debug: true
`
		output := runPlaybook(t, playbook)
		want := "Cannot find image with name " + image + ", and pull=never"
		if !strings.Contains(output, "FAILED") || !strings.Contains(output, want) {
			t.Fatalf("pull=never output = %s", output)
		}
		assertContainerImageExists(t, client, image, false)
		assertContainerExists(t, client, name, false)
	})

	baseArgs := `
      name: ` + name + `
      image: ` + image + `
      state: present
      debug: true
`
	pulledChanged := map[string]any{"pulled_image": image, "changed": true}
	pulledUnchanged := map[string]any{"pulled_image": image, "changed": false}
	pulledPrediction := map[string]any{"pulled_image": image}

	t.Run("missing policy check predicts without mutation", func(t *testing.T) {
		result := runContainerStateTask(t, client, "pull-missing-check", baseArgs+"      pull: missing\n", "--check")
		assertChanged(t, result, true)
		assertExactContainerAction(t, result, pulledChanged, true)
		assertExactContainerAction(t, result, pulledUnchanged, false)
		assertExactContainerAction(t, result, pulledPrediction, false)
		assertContainerImageExists(t, client, image, false)
		assertContainerExists(t, client, name, false)
	})

	var containerID string
	t.Run("missing policy pulls and creates", func(t *testing.T) {
		result := runContainerStateTask(t, client, "pull-missing", baseArgs+"      pull: missing\n")
		assertChanged(t, result, true)
		assertExactContainerAction(t, result, pulledChanged, true)
		assertExactContainerAction(t, result, pulledUnchanged, false)
		assertExactContainerAction(t, result, pulledPrediction, false)
		assertContainerImageExists(t, client, image, true)
		assertContainerExists(t, client, name, true)
		containerID = containerResultID(t, result)
	})

	t.Run("missing policy is idempotent in check and real modes", func(t *testing.T) {
		check := runContainerStateTask(t, client, "pull-present-check", baseArgs+"      pull: missing\n", "--check")
		assertChanged(t, check, false)
		assertNoContainerPullAction(t, check, image)
		if got := containerResultID(t, check); got != containerID {
			t.Fatalf("check-mode container ID = %s, want %s", got, containerID)
		}

		real := runContainerStateTask(t, client, "pull-present", baseArgs+"      pull: missing\n")
		assertChanged(t, real, false)
		assertNoContainerPullAction(t, real, image)
		if got := containerResultID(t, real); got != containerID {
			t.Fatalf("idempotent container ID = %s, want %s", got, containerID)
		}
	})

	t.Run("always policy check behavior matches upstream", func(t *testing.T) {
		notPresentOnly := runContainerStateTask(t, client, "pull-always-not-present-check", baseArgs+`
      pull: always
      pull_check_mode_behavior: image_not_present
`, "--check")
		assertChanged(t, notPresentOnly, false)
		assertNoContainerPullAction(t, notPresentOnly, image)

		always := runContainerStateTask(t, client, "pull-always-check", baseArgs+`
      pull: always
      pull_check_mode_behavior: always
`, "--check")
		assertChanged(t, always, true)
		assertExactContainerAction(t, always, pulledPrediction, true)
		assertExactContainerAction(t, always, pulledChanged, false)
		assertExactContainerAction(t, always, pulledUnchanged, false)
		if got := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+name); got != containerID {
			t.Fatalf("check-mode always pull changed container ID: before=%s after=%s", containerID, got)
		}
	})

	t.Run("always policy reports unchanged when image ID is unchanged", func(t *testing.T) {
		result := runContainerStateTask(t, client, "pull-always", baseArgs+"      pull: always\n")
		assertChanged(t, result, false)
		assertExactContainerAction(t, result, pulledUnchanged, true)
		assertExactContainerAction(t, result, pulledChanged, false)
		assertExactContainerAction(t, result, pulledPrediction, false)
		if got := containerResultID(t, result); got != containerID {
			t.Fatalf("always pull recreated container: before=%s after=%s", containerID, got)
		}
	})
}

func cleanupContainerPullFixture(t *testing.T, client *ssh.Client, name, image string) {
	t.Helper()
	remoteExec(t, client, "docker rm -f "+name+" || true")
	remoteExec(t, client, "docker image rm -f "+image+" >/dev/null 2>&1 || true")
}

func assertContainerImageExists(t *testing.T, client *ssh.Client, image string, expected bool) {
	t.Helper()
	got := remoteExec(t, client, "if docker image inspect "+image+" >/dev/null 2>&1; then echo true; else echo false; fi")
	if got != map[bool]string{true: "true", false: "false"}[expected] {
		t.Fatalf("image %s exists=%s, want %t", image, got, expected)
	}
}

func assertExactContainerAction(t *testing.T, result map[string]any, expected map[string]any, present bool) {
	t.Helper()
	found := false
	if values, ok := result["actions"].([]any); ok {
		for _, value := range values {
			action, ok := value.(map[string]any)
			if ok && reflect.DeepEqual(action, expected) {
				found = true
				break
			}
		}
	}
	if found != present {
		t.Fatalf("action %#v present=%t in %#v, want %t", expected, found, result["actions"], present)
	}
}

func assertNoContainerPullAction(t *testing.T, result map[string]any, image string) {
	t.Helper()
	for _, action := range []map[string]any{
		{"pulled_image": image},
		{"pulled_image": image, "changed": true},
		{"pulled_image": image, "changed": false},
	} {
		assertExactContainerAction(t, result, action, false)
	}
}
