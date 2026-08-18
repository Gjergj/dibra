//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerVolumeInheritanceParity ports the pinned
// volume_driver and volumes_from create/change/idempotency scenarios.
func TestPlaybook_DockerContainerVolumeInheritanceParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		name    = "dibra-container-volume-inheritance"
		helper1 = "dibra-container-volume-helper-one"
		helper2 = "dibra-container-volume-helper-two"
	)
	cleanup := func() {
		remoteExec(t, client, "docker rm -f "+name+" "+helper1+" "+helper2+" || true")
	}
	cleanup()
	defer cleanup()

	t.Run("volume_driver", func(t *testing.T) {
		run := func(suffix, driver string) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "volume-driver-"+suffix, `
      name: `+name+`
      image: alpine:latest
      command: ["sleep", "300"]
      state: started
      force_kill: true
      volume_driver: "`+driver+`"
`, "--diff")
		}

		created := run("create", "local")
		assertChanged(t, created, true)
		initialID := containerResultID(t, created)
		assertChanged(t, run("idempotent", "local"), false)

		changed := run("change", "/")
		assertChanged(t, changed, true)
		if got := containerResultID(t, changed); got == initialID {
			t.Fatalf("volume_driver change did not recreate container: %s", initialID)
		}
		assertChanged(t, run("changed-idempotent", "/"), false)
	})

	t.Run("volumes_from", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		mustRemote(t, client, "docker run -d --name "+helper1+" -v /helper-one alpine:latest sleep 300")
		mustRemote(t, client, "docker run -d --name "+helper2+" -v /helper-two alpine:latest sleep 300")

		run := func(suffix, helper string) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "volumes-from-"+suffix, `
      name: `+name+`
      image: alpine:latest
      command: ["sleep", "300"]
      state: started
      force_kill: true
      volumes_from:
        - `+helper+`
`, "--diff")
		}

		created := run("create", helper1)
		assertChanged(t, created, true)
		initialID := containerResultID(t, created)
		assertChanged(t, run("idempotent", helper1), false)
		if got := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.VolumesFrom}}' "+name); !strings.Contains(got, helper1) {
			t.Fatalf("volumes_from inspect = %q, want %s", got, helper1)
		}

		changed := run("change", helper2)
		assertChanged(t, changed, true)
		if got := containerResultID(t, changed); got == initialID {
			t.Fatalf("volumes_from change did not recreate container: %s", initialID)
		}
		assertChanged(t, run("changed-idempotent", helper2), false)
	})
}
