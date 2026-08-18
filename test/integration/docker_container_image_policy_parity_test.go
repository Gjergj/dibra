//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerImagePolicyParity ports the pinned
// image_comparison and image_label_mismatch matrices with real labeled images.
func TestPlaybook_DockerContainerImagePolicyParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		currentImage = "dibra-container-policy:current"
		desiredImage = "dibra-container-policy:desired"
	)
	remoteExec(t, client, "printf 'FROM alpine:latest\\nLABEL inherited=one\\nENV BASE=one\\n' | docker build -q -t "+currentImage+" -")
	remoteExec(t, client, "printf 'FROM alpine:latest\\nLABEL inherited=two\\nENV BASE=two\\n' | docker build -q -t "+desiredImage+" -")
	defer remoteExec(t, client, "docker image rm -f "+currentImage+" "+desiredImage+" || true")
	const bootstrapName = "dibra-container-policy-agent-bootstrap"
	remoteExec(t, client, "docker rm -f "+bootstrapName+" || true")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent")
	assertChanged(t, runContainerStateTask(t, client, "image-policy-agent-bootstrap", `
      name: `+bootstrapName+`
      image: alpine:latest
      state: present
`, "--diff"), true)
	defer remoteExec(t, client, "docker rm -f "+bootstrapName+" || true")

	run := func(name, image, imageComparison, labelMismatch string) map[string]any {
		t.Helper()
		args := map[string]any{
			"name": name, "image": image, "state": "started",
			"command": []string{"sleep", "300"}, "force_kill": true,
			"env": map[string]any{"EXPLICIT": "managed"},
			"labels": map[string]any{"managed": "true"},
			"comparisons": map[string]any{"image": "ignore", "env": "strict", "labels": "strict"},
		}
		if imageComparison != "" {
			args["image_comparison"] = imageComparison
		}
		if labelMismatch != "" {
			args["image_label_mismatch"] = labelMismatch
		}
		return runContainerAgentRequestWithDiff(t, client, args, true)
	}

	t.Run("image_comparison", func(t *testing.T) {
		currentName := "dibra-image-comparison-current"
		desiredName := "dibra-image-comparison-desired"
		remoteExec(t, client, "docker rm -f "+currentName+" "+desiredName+" || true")
		defer remoteExec(t, client, "docker rm -f "+currentName+" "+desiredName+" || true")

		currentCreated := run(currentName, currentImage, "", "ignore")
		desiredCreated := run(desiredName, currentImage, "", "ignore")
		assertChanged(t, currentCreated, true)
		assertChanged(t, desiredCreated, true)
		currentID := containerResultID(t, currentCreated)
		desiredID := containerResultID(t, desiredCreated)

		currentCompared := run(currentName, desiredImage, "current-image", "ignore")
		assertChanged(t, currentCompared, false)
		if got := containerResultID(t, currentCompared); got != currentID {
			t.Fatalf("current-image comparison recreated container: before=%s after=%s", currentID, got)
		}

		desiredCompared := run(desiredName, desiredImage, "desired-image", "ignore")
		assertChanged(t, desiredCompared, true)
		if got := containerResultID(t, desiredCompared); got == desiredID {
			t.Fatalf("desired-image comparison did not recreate container: %s", desiredID)
		}
		assertChanged(t, run(desiredName, desiredImage, "desired-image", "ignore"), false)
	})

	t.Run("image_label_mismatch", func(t *testing.T) {
		const name = "dibra-image-label-mismatch"
		remoteExec(t, client, "docker rm -f "+name+" || true")
		defer remoteExec(t, client, "docker rm -f "+name+" || true")

		ignored := run(name, currentImage, "desired-image", "ignore")
		assertChanged(t, ignored, true)
		assertChanged(t, run(name, currentImage, "desired-image", "ignore"), false)

		failed := run(name, currentImage, "desired-image", "fail")
		if failed["failed"] != true {
			t.Fatalf("image_label_mismatch=fail result = %#v", failed)
		}
		message, _ := failed["msg"].(string)
		if !strings.Contains(message, "some labels should be removed") ||
			!strings.Contains(message, `"inherited"`) {
			t.Fatalf("image_label_mismatch failure = %q", message)
		}
	})
}
