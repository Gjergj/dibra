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

// TestPlaybook_DockerContainerImageIDsParity independently ports the pinned
// community.docker image-ids.yml name, full-ID, and digest lifecycle.
func TestPlaybook_DockerContainerImageIDsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		name       = "dibra-container-image-ids-parity"
		helloImage = "quay.io/ansible/docker-test-containers:hello-world"
		alpine     = "quay.io/ansible/docker-test-containers:alpine3.8"
		digestBase = "quay.io/ansible/docker-test-containers"
		digestV1   = "e004c2cc521c95383aebb1fb5893719aa7a8eae2e7a71f316a4410784edb00a9"
		digestV2   = "ee44b399df993016003bf5466bd3eeb221305e9d0fa831606bc7902d149c775b"
	)
	digestRefV1 := digestBase + "@sha256:" + digestV1
	digestRefV2 := digestBase + "@sha256:" + digestV2
	cleanup := func() {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		remoteExec(t, client, "docker image rm -f "+helloImage+" "+alpine+" "+digestRefV1+" "+digestRefV2+" >/dev/null 2>&1 || true")
	}
	cleanup()
	defer cleanup()

	mustRemote(t, client, "docker pull "+helloImage)
	mustRemote(t, client, "docker pull "+alpine)
	helloID := mustRemote(t, client, "docker image inspect --format '{{.Id}}' "+helloImage)
	alpineID := mustRemote(t, client, "docker image inspect --format '{{.Id}}' "+alpine)
	if helloID == "" || alpineID == "" || helloID == alpineID {
		t.Fatalf("fixture image IDs: hello=%q alpine=%q", helloID, alpineID)
	}

	t.Run("full image IDs create switch and stay idempotent", func(t *testing.T) {
		helloArgs := `
      name: ` + name + `
      image: ` + helloID + `
      state: present
      force_kill: true
`
		created := runContainerStateTask(t, client, "image-id-hello", helloArgs, "--diff")
		assertChanged(t, created, true)
		helloContainerID := containerResultID(t, created)
		if got := containerResultImageID(t, created); got != helloID {
			t.Fatalf("hello container image = %s, want %s", got, helloID)
		}
		assertChanged(t, runContainerStateTask(t, client, "image-id-hello-idempotent", helloArgs, "--diff"), false)

		alpineArgs := `
      name: ` + name + `
      image: ` + alpineID + `
      state: present
      force_kill: true
`
		changed := runContainerStateTask(t, client, "image-id-alpine", alpineArgs, "--diff")
		assertChanged(t, changed, true)
		alpineContainerID := containerResultID(t, changed)
		if alpineContainerID == helloContainerID {
			t.Fatalf("switching image IDs kept container ID %s", helloContainerID)
		}
		if got := containerResultImageID(t, changed); got != alpineID {
			t.Fatalf("alpine container image = %s, want %s", got, alpineID)
		}
		assertChanged(t, runContainerStateTask(t, client, "image-id-alpine-idempotent", alpineArgs, "--diff"), false)

		mustRemote(t, client, "docker image rm -f "+alpine)
		assertContainerImageExists(t, client, alpine, false)
		nameArgs := `
      name: ` + name + `
      image: ` + alpine + `
      state: present
      image_name_mismatch: ignore
      debug: true
`
		predicted := runContainerStateTask(t, client, "image-name-check", nameArgs, "--check")
		assertChanged(t, predicted, true)
		assertExactContainerAction(t, predicted, map[string]any{"pulled_image": alpine, "changed": true}, true)
		assertContainerImageExists(t, client, alpine, false)
		if got := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+name); got != alpineContainerID {
			t.Fatalf("check-mode name pull changed container: before=%s after=%s", alpineContainerID, got)
		}

		pulled := runContainerStateTask(t, client, "image-name", nameArgs)
		assertChanged(t, pulled, true)
		assertExactContainerAction(t, pulled, map[string]any{"pulled_image": alpine, "changed": true}, true)
		if got := containerResultID(t, pulled); got != alpineContainerID {
			t.Fatalf("same-image name pull recreated container: before=%s after=%s", alpineContainerID, got)
		}
		if got := containerResultImageID(t, pulled); got != alpineID {
			t.Fatalf("same-image name pull image = %s, want %s", got, alpineID)
		}
	})

	t.Run("digest references pull remain idempotent and switch", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		remoteExec(t, client, "docker image rm -f "+digestRefV1+" "+digestRefV2+" >/dev/null 2>&1 || true")
		v1Args := `
      name: ` + name + `
      image: ` + digestRefV1 + `
      state: present
      force_kill: true
      debug: true
`
		created := runContainerStateTask(t, client, "digest-v1", v1Args)
		assertChanged(t, created, true)
		assertExactContainerAction(t, created, map[string]any{"pulled_image": digestRefV1, "changed": true}, true)
		v1ContainerID := containerResultID(t, created)
		if got := containerResultImageID(t, created); got != "sha256:"+digestV1 {
			t.Fatalf("v1 container image = %s", got)
		}

		idempotent := runContainerStateTask(t, client, "digest-v1-idempotent", v1Args)
		assertChanged(t, idempotent, false)
		assertNoContainerPullAction(t, idempotent, digestRefV1)

		pullAlways := runContainerStateTask(t, client, "digest-v1-pull", v1Args+"      pull: always\n")
		assertChanged(t, pullAlways, false)
		assertExactContainerAction(t, pullAlways, map[string]any{"pulled_image": digestRefV1, "changed": false}, true)
		if got := containerResultID(t, pullAlways); got != v1ContainerID {
			t.Fatalf("digest pull recreated container: before=%s after=%s", v1ContainerID, got)
		}

		v2Args := `
      name: ` + name + `
      image: ` + digestRefV2 + `
      state: present
      force_kill: true
      debug: true
`
		changed := runContainerStateTask(t, client, "digest-v2", v2Args)
		assertChanged(t, changed, true)
		assertExactContainerAction(t, changed, map[string]any{"pulled_image": digestRefV2, "changed": true}, true)
		if got := containerResultID(t, changed); got == v1ContainerID {
			t.Fatalf("digest switch kept container ID %s", v1ContainerID)
		}
		if got := containerResultImageID(t, changed); got != "sha256:"+digestV2 {
			t.Fatalf("v2 container image = %s", got)
		}
	})
}

func cleanupContainerPullFixture(t *testing.T, client *ssh.Client, name, image string) {
	t.Helper()
	remoteExec(t, client, "docker rm -f "+name+" || true")
	remoteExec(t, client, "docker image rm -f "+image+" >/dev/null 2>&1 || true")
}

func containerResultImageID(t *testing.T, result map[string]any) string {
	t.Helper()
	container, ok := result["container"].(map[string]any)
	if !ok {
		t.Fatalf("container result = %#v", result["container"])
	}
	imageID, _ := container["Image"].(string)
	if imageID == "" {
		t.Fatalf("container image ID missing: %#v", container)
	}
	return imageID
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
