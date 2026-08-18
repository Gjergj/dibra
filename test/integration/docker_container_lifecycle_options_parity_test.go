//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerLifecycleOptionsParity ports the pinned
// keep_volumes, kill_signal, and removal_wait_timeout lifecycle cases.
func TestPlaybook_DockerContainerLifecycleOptionsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	t.Run("keep_volumes_and_removal_wait_timeout", func(t *testing.T) {
		const name = "dibra-container-keep-volumes"
		remoteExec(t, client, "docker rm -f -v "+name+" || true")
		defer remoteExec(t, client, "docker rm -f -v "+name+" || true")

		create := func(suffix string) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "keep-volumes-"+suffix, `
      name: `+name+`
      image: alpine:latest
      state: present
      volumes:
        - /data
`, "--diff")
		}
		remove := func(suffix string, keep bool) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "keep-volumes-"+suffix, `
      name: `+name+`
      state: absent
      force_kill: true
      keep_volumes: `+boolString(keep)+`
      removal_wait_timeout: 5
`, "--diff")
		}

		assertChanged(t, create("remove-create"), true)
		removedVolume := remoteExec(t, client, "docker inspect --format '{{(index .Mounts 0).Name}}' "+name)
		assertChanged(t, remove("remove", false), true)
		if _, _, err := client.Run("docker volume inspect " + removedVolume); err == nil {
			t.Fatalf("keep_volumes=false retained anonymous volume %s", removedVolume)
		}

		assertChanged(t, create("retain-create"), true)
		retainedVolume := remoteExec(t, client, "docker inspect --format '{{(index .Mounts 0).Name}}' "+name)
		assertChanged(t, remove("retain", true), true)
		if _, stderr, err := client.Run("docker volume inspect " + retainedVolume); err != nil {
			t.Fatalf("keep_volumes=true removed anonymous volume %s: %v\n%s", retainedVolume, err, stderr)
		}
		remoteExec(t, client, "docker volume rm "+retainedVolume+" || true")

		assertChanged(t, remove("already-absent", true), false)
	})

	t.Run("kill_signal", func(t *testing.T) {
		const name = "dibra-container-kill-signal"
		const markerDir = "/tmp/dibra-container-kill-signal"
		remoteExec(t, client, "docker rm -f "+name+" || true")
		remoteExec(t, client, "rm -rf "+markerDir+" && mkdir -p "+markerDir)
		defer remoteExec(t, client, "docker rm -f "+name+" || true; rm -rf "+markerDir)

		options := `
      name: ` + name + `
      image: alpine:latest
      state: started
      command: ["sh", "-c", "trap 'exit' TERM; touch /signal/ready; while :; do sleep 1; done"]
      volumes:
        - ` + markerDir + `:/signal
      force_kill: true
      kill_signal: SIGTERM
      debug: true
`
		assertChanged(t, runContainerStateTask(t, client, "kill-signal-create", options, "--diff"), true)
		if _, stderr, err := client.Run("for i in 1 2 3 4 5; do test -f " + markerDir + "/ready && exit 0; sleep 1; done; exit 1"); err != nil {
			t.Fatalf("container did not install signal trap: %v\n%s", err, stderr)
		}
		stopped := runContainerStateTask(t, client, "kill-signal-stop", strings.Replace(options, "state: started", "state: stopped", 1), "--diff")
		assertChanged(t, stopped, true)
		actions, ok := stopped["actions"].([]any)
		if !ok || len(actions) == 0 {
			t.Fatalf("kill_signal actions = %#v", stopped["actions"])
		}
		action, ok := actions[0].(map[string]any)
		if !ok || action["signal"] != "SIGTERM" {
			t.Fatalf("kill_signal action = %#v", actions[0])
		}
		if _, stderr, err := client.Run("for i in 1 2 3 4 5; do test \"$(docker inspect --format '{{.State.Status}}' " + name + ")\" = exited && exit 0; sleep 1; done; exit 1"); err != nil {
			t.Fatalf("container did not exit after configured kill signal: %v\n%s", err, stderr)
		}
		if got := remoteExec(t, client, "docker inspect --format '{{.State.Status}}' "+name); got != "exited" {
			t.Fatalf("kill_signal state = %q, want exited", got)
		}
		assertChanged(t, runContainerStateTask(t, client, "kill-signal-stopped-idempotent", strings.Replace(options, "state: started", "state: stopped", 1), "--diff"), false)
	})
}
