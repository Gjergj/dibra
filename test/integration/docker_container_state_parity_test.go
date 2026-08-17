//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerStateParity independently ports the lifecycle,
// check-mode, diff, recreation, and restart contracts from the pinned
// community.docker start-stop.yml target.
func TestPlaybook_DockerContainerStateParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const name = "dibra-container-state-parity"
	remoteExec(t, client, "docker pull alpine:latest")
	remoteExec(t, client, "docker rm -f "+name+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")

	t.Run("present start stop remove check diff and idempotency", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		presentArgs := `
      name: ` + name + `
      image: alpine:latest
      command: [sleep, "600"]
      state: present
`
		createCheck := runContainerStateTask(t, client, "create-check", presentArgs, "--check", "--diff")
		assertChanged(t, createCheck, true)
		assertContainerExists(t, client, name, false)

		created := runContainerStateTask(t, client, "create", presentArgs, "--diff")
		assertChanged(t, created, true)
		createdID := containerResultID(t, created)
		assertDiffField(t, created, "exists", false, true)

		assertChanged(t, runContainerStateTask(t, client, "create-idempotent", presentArgs, "--diff"), false)
		assertChanged(t, runContainerStateTask(t, client, "create-idempotent-check", presentArgs, "--check", "--diff"), false)

		startArgs := `
      name: ` + name + `
      state: started
`
		startCheck := runContainerStateTask(t, client, "start-check", startArgs, "--check", "--diff")
		assertChanged(t, startCheck, true)
		assertContainerRunning(t, client, name, false)

		started := runContainerStateTask(t, client, "start", startArgs, "--diff")
		assertChanged(t, started, true)
		if got := containerResultID(t, started); got != createdID {
			t.Fatalf("start recreated container: before=%s after=%s", createdID, got)
		}
		assertDiffField(t, started, "running", false, true)
		assertContainerRunning(t, client, name, true)
		assertChanged(t, runContainerStateTask(t, client, "start-idempotent", startArgs, "--diff"), false)
		assertChanged(t, runContainerStateTask(t, client, "start-idempotent-check", startArgs, "--check", "--diff"), false)

		runningPresent := runContainerStateTask(t, client, "present-running", presentArgs, "--check", "--diff")
		assertChanged(t, runningPresent, false)
		assertContainerRunning(t, client, name, true)

		stopArgs := `
      name: ` + name + `
      state: stopped
      stop_timeout: 1
`
		stopCheck := runContainerStateTask(t, client, "stop-check", stopArgs, "--check", "--diff")
		assertChanged(t, stopCheck, true)
		assertContainerRunning(t, client, name, true)

		stopped := runContainerStateTask(t, client, "stop", stopArgs, "--diff")
		assertChanged(t, stopped, true)
		if got := containerResultID(t, stopped); got != createdID {
			t.Fatalf("stop recreated container: before=%s after=%s", createdID, got)
		}
		assertDiffField(t, stopped, "running", true, false)
		assertContainerRunning(t, client, name, false)
		assertChanged(t, runContainerStateTask(t, client, "stop-idempotent", stopArgs, "--diff"), false)
		assertChanged(t, runContainerStateTask(t, client, "stop-idempotent-check", stopArgs, "--check", "--diff"), false)

		removeArgs := `
      name: ` + name + `
      state: absent
`
		removeCheck := runContainerStateTask(t, client, "remove-check", removeArgs, "--check", "--diff")
		assertChanged(t, removeCheck, true)
		assertContainerExists(t, client, name, true)

		removed := runContainerStateTask(t, client, "remove", removeArgs, "--diff")
		assertChanged(t, removed, true)
		assertDiffField(t, removed, "exists", true, false)
		assertContainerExists(t, client, name, false)
		assertChanged(t, runContainerStateTask(t, client, "remove-idempotent", removeArgs, "--diff"), false)
		assertChanged(t, runContainerStateTask(t, client, "remove-idempotent-check", removeArgs, "--check", "--diff"), false)
	})

	t.Run("recreate and restart preserve expected identity", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		baseArgs := `
      name: ` + name + `
      image: alpine:latest
      command: [sleep, "600"]
      state: started
      volumes:
        - /tmp/tmp
`
		created := runContainerStateTask(t, client, "recreate-base", baseArgs, "--diff")
		originalID := containerResultID(t, created)

		recreateArgs := baseArgs + "      recreate: always\n      force_kill: true\n"
		predicted := runContainerStateTask(t, client, "recreate-check", recreateArgs, "--check", "--diff")
		assertChanged(t, predicted, true)
		if got := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+name); got != originalID {
			t.Fatalf("check-mode recreate changed ID: before=%s after=%s", originalID, got)
		}

		recreated := runContainerStateTask(t, client, "recreate", recreateArgs, "--diff")
		recreatedID := containerResultID(t, recreated)
		if recreatedID == originalID {
			t.Fatalf("recreate kept container ID %s", originalID)
		}
		assertDiffField(t, recreated, "recreate", false, true)

		startedAt := remoteExec(t, client, "docker inspect --format '{{.State.StartedAt}}' "+name)
		time.Sleep(1100 * time.Millisecond)
		restartArgs := baseArgs + "      restart: true\n      force_kill: true\n"
		restartCheck := runContainerStateTask(t, client, "restart-check", restartArgs, "--check", "--diff")
		assertChanged(t, restartCheck, true)
		if got := remoteExec(t, client, "docker inspect --format '{{.State.StartedAt}}' "+name); got != startedAt {
			t.Fatalf("check-mode restart mutated StartedAt: before=%s after=%s", startedAt, got)
		}

		restarted := runContainerStateTask(t, client, "restart", restartArgs, "--diff")
		assertChanged(t, restarted, true)
		if got := containerResultID(t, restarted); got != recreatedID {
			t.Fatalf("restart recreated container: before=%s after=%s", recreatedID, got)
		}
		if got := remoteExec(t, client, "docker inspect --format '{{.State.StartedAt}}' "+name); got == startedAt {
			t.Fatalf("restart did not change StartedAt: %s", got)
		}
		assertChanged(t, runContainerStateTask(t, client, "restart-verify-volumes", baseArgs, "--diff"), false)
	})
}

func runContainerStateTask(
	t *testing.T,
	client *ssh.Client,
	suffix string,
	arguments string,
	extra ...string,
) map[string]any {
	t.Helper()
	templatePath := writeResultTemplate(t, "container_result")
	remotePath := "/tmp/dibra-container-state-" + suffix + ".json"
	playbook := playbookHeader + `
  - name: Manage container lifecycle
    community.docker.docker_container:
` + arguments + `
    register: container_result

  - name: Persist container result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extra...)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("%s failed: %s", suffix, output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}

func assertChanged(t *testing.T, result map[string]any, expected bool) {
	t.Helper()
	if result["changed"] != expected || result["failed"] == true {
		t.Fatalf("changed=%v result=%#v", expected, result)
	}
	if !expected {
		if _, found := result["diff"]; found {
			t.Fatalf("unchanged result returned diff: %#v", result["diff"])
		}
	}
}

func assertContainerExists(t *testing.T, client *ssh.Client, name string, expected bool) {
	t.Helper()
	got := remoteExec(t, client, "if docker inspect "+name+" >/dev/null 2>&1; then echo true; else echo false; fi")
	if got != fmt.Sprint(expected) {
		t.Fatalf("container %s exists=%s, want %t", name, got, expected)
	}
}

func assertContainerRunning(t *testing.T, client *ssh.Client, name string, expected bool) {
	t.Helper()
	got := remoteExec(t, client, "docker inspect --format '{{.State.Running}}' "+name)
	if got != fmt.Sprint(expected) {
		t.Fatalf("container %s running=%s, want %t", name, got, expected)
	}
}

func containerResultID(t *testing.T, result map[string]any) string {
	t.Helper()
	container, ok := result["container"].(map[string]any)
	if !ok {
		t.Fatalf("container result = %#v", result["container"])
	}
	id, _ := container["Id"].(string)
	if id == "" {
		t.Fatalf("container ID missing: %#v", container)
	}
	return id
}

func assertDiffField(t *testing.T, result map[string]any, field string, before, after any) {
	t.Helper()
	diff, ok := result["diff"].(map[string]any)
	if !ok {
		t.Fatalf("diff missing: %#v", result)
	}
	beforeMap, beforeOK := diff["before"].(map[string]any)
	afterMap, afterOK := diff["after"].(map[string]any)
	if !beforeOK || !afterOK || beforeMap[field] != before || afterMap[field] != after {
		t.Fatalf("diff field %s = %#v, want before=%#v after=%#v", field, diff, before, after)
	}
}
