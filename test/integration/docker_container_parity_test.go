//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerCanonicalParity ports the canonical-option,
// idempotency, check-mode, and diff-mode patterns from the pinned upstream
// docker_container options/comparisons scenarios.
func TestPlaybook_DockerContainerCanonicalParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-container-canonical-parity"
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create canonical parity container
    community.docker.docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      capabilities:
        - NET_RAW
      security_opts:
        - no-new-privileges:true
      published_ports:
        - "127.0.0.1:48200:80"
      ulimits:
        - "nofile:1024:2048"
      read_only: true
      comparisons:
        env: strict
`
	first := runPlaybookWithArgs(t, playbook, "--diff")
	if strings.Contains(first, "FAILED") || !strings.Contains(first, "CHANGED") {
		t.Fatalf("canonical create failed: %s", first)
	}
	second := runPlaybookWithArgs(t, playbook, "--diff")
	if strings.Contains(second, "FAILED") || strings.Contains(second, "CHANGED") {
		t.Fatalf("canonical options were not idempotent: %s", second)
	}

	checkPlaybook := playbookHeader + `
  - name: Predict environment recreation
    community.docker.docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      env:
        PARITY_CHECK: "predicted-only"
      comparisons:
        env: strict
`
	check := runPlaybookWithArgs(t, checkPlaybook, "--check", "--diff")
	if strings.Contains(check, "FAILED") || strings.Contains(check, "SKIPPED") || !strings.Contains(check, "CHANGED") {
		t.Fatalf("container check mode did not predict the recreation: %s", check)
	}
	environment := remoteExec(t, client, "docker inspect --format '{{json .Config.Env}}' "+containerName)
	if strings.Contains(environment, "PARITY_CHECK") {
		t.Fatalf("check mode mutated the container: %s", environment)
	}
}

// TestPlaybook_DockerContainerPinnedUpstreamOptions ports the pinned upstream
// options, mounts, image-platform, healthcheck, env-file, link, and MAC-address
// idempotency scenarios to the current Engine baseline.
func TestPlaybook_DockerContainerPinnedUpstreamOptions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-container-upstream-options"
	linkedName := "test-container-upstream-link"
	envFile := "/tmp/dibra-container-parity.env"
	remoteExec(t, client, "docker rm -f "+containerName+" "+linkedName+" || true")
	remoteExec(t, client, "printf 'FROM_FILE=present\\nOVERRIDE=file\\n' > "+envFile)
	defer remoteExec(t, client, "docker rm -f "+containerName+" "+linkedName+" || true")

	remoteExec(t, client, "docker run -d --name "+linkedName+" alpine:latest sleep 300")
	platform := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}' alpine:latest"))
	playbook := playbookHeader + `
  - name: Exercise pinned upstream container options
    community.docker.docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      env_file: ` + envFile + `
      env:
        OVERRIDE: module
      hostname: parity-host
      domainname: example.test
      platform: ` + platform + `
      mac_address: "02:42:ac:11:00:42"
      links:
        - "` + linkedName + `:linked"
      healthcheck:
        test: "true"
        test_cli_compatible: true
        interval: 5s
        timeout: 2s
        retries: 2
      mounts:
        - type: tmpfs
          target: /cache
          tmpfs_size: 16m
          tmpfs_mode: "1770"
          tmpfs_options:
            - noexec:
      restart_policy: on-failure
      restart_retries: 3
`
	first := runPlaybookWithArgs(t, playbook, "--diff")
	if strings.Contains(first, "FAILED") || !strings.Contains(first, "CHANGED") {
		t.Fatalf("upstream option create failed: %s", first)
	}
	second := runPlaybookWithArgs(t, playbook, "--diff")
	if strings.Contains(second, "FAILED") || strings.Contains(second, "CHANGED") {
		t.Fatalf("upstream options were not idempotent: %s", second)
	}

	inspect := remoteExec(t, client, "docker inspect --format '{{json .Config.Env}}|{{json .Config.Healthcheck.Test}}|{{json .HostConfig.Mounts}}|{{json .HostConfig.Links}}|{{json .NetworkSettings.Networks.bridge.MacAddress}}' "+containerName)
	for _, expected := range []string{"FROM_FILE=present", "OVERRIDE=module", "CMD-SHELL", "noexec", linkedName, "02:42:ac:11:00:42"} {
		if !strings.Contains(inspect, expected) {
			t.Fatalf("container inspection is missing %q: %s", expected, inspect)
		}
	}
}

// TestPlaybook_DockerContainerHealthyPausedAndCleanup ports the pinned
// healthy-state, pause/unpause, and foreground-cleanup lifecycle scenarios.
func TestPlaybook_DockerContainerHealthyPausedAndCleanup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	healthyName := "test-container-upstream-healthy"
	cleanupName := "test-container-upstream-cleanup"
	remoteExec(t, client, "docker rm -f "+healthyName+" "+cleanupName+" || true")
	defer remoteExec(t, client, "docker rm -f "+healthyName+" "+cleanupName+" || true")

	healthyPlaybook := playbookHeader + `
  - name: Wait for a healthy container
    community.docker.docker_container:
      name: ` + healthyName + `
      image: alpine:latest
      state: healthy
      command: ["sh", "-c", "touch /tmp/ready && sleep 300"]
      healthcheck:
        test: ["CMD-SHELL", "test -f /tmp/ready"]
        interval: 1s
        timeout: 1s
        retries: 3
      healthy_wait_timeout: 15
`
	first := runPlaybookWithArgs(t, healthyPlaybook, "--diff")
	if strings.Contains(first, "FAILED") || !strings.Contains(first, "CHANGED") {
		t.Fatalf("healthy/paused create failed: %s", first)
	}
	second := runPlaybookWithArgs(t, healthyPlaybook, "--diff")
	if strings.Contains(second, "FAILED") || strings.Contains(second, "CHANGED") {
		t.Fatalf("healthy/paused state was not idempotent: %s", second)
	}
	pausePlaybook := playbookHeader + `
  - name: Pause the healthy container
    community.docker.docker_container:
      name: ` + healthyName + `
      image: alpine:latest
      state: started
      command: ["sh", "-c", "touch /tmp/ready && sleep 300"]
      healthcheck:
        test: ["CMD-SHELL", "test -f /tmp/ready"]
        interval: 1s
        timeout: 1s
        retries: 3
      paused: true
`
	pauseFirst := runPlaybookWithArgs(t, pausePlaybook, "--diff")
	pauseSecond := runPlaybookWithArgs(t, pausePlaybook, "--diff")
	if strings.Contains(pauseFirst, "FAILED") || !strings.Contains(pauseFirst, "CHANGED") || strings.Contains(pauseSecond, "FAILED") || strings.Contains(pauseSecond, "CHANGED") {
		t.Fatalf("pause lifecycle mismatch: first=%s second=%s", pauseFirst, pauseSecond)
	}
	if paused := remoteExec(t, client, "docker inspect --format '{{.State.Paused}}' "+healthyName); !strings.Contains(paused, "true") {
		t.Fatalf("container was not paused: %s", paused)
	}

	cleanupPlaybook := playbookHeader + `
  - name: Run and clean up a foreground container
    community.docker.docker_container:
      name: ` + cleanupName + `
      image: alpine:latest
      state: started
      command: ["sh", "-c", "printf parity-output"]
      detach: false
      cleanup: true
      output_logs: true
`
	cleanup := runPlaybook(t, cleanupPlaybook)
	if strings.Contains(cleanup, "FAILED") || !strings.Contains(cleanup, "CHANGED") || !strings.Contains(cleanup, "parity-output") {
		t.Fatalf("foreground cleanup failed: %s", cleanup)
	}
	if exists := remoteExec(t, client, "docker inspect "+cleanupName+" >/dev/null 2>&1 && echo present || echo absent"); !strings.Contains(exists, "absent") {
		t.Fatalf("cleanup left the foreground container behind: %s", exists)
	}
}

// TestPlaybook_DockerContainerPinnedRegressionMatrix exercises behavior that
// is easy to miss in a surface-only option audit: alias comparison keys,
// allow-more port semantics, ignored network comparisons during creation,
// absent-state parsing, and foreground output with a non-readable log driver.
func TestPlaybook_DockerContainerPinnedRegressionMatrix(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	portName := "test-container-parity-ports"
	networkName := "test-container-parity-ignored-network"
	networkContainer := "test-container-parity-network-create"
	foregroundName := "test-container-parity-log-none"
	autoRemoveName := "test-container-parity-auto-remove"
	remoteExec(t, client, "docker rm -f "+portName+" "+networkContainer+" "+foregroundName+" "+autoRemoveName+" || true")
	remoteExec(t, client, "docker network rm "+networkName+" || true")
	defer remoteExec(t, client, "docker rm -f "+portName+" "+networkContainer+" "+foregroundName+" "+autoRemoveName+" || true")
	defer remoteExec(t, client, "docker network rm "+networkName+" || true")
	remoteExec(t, client, "docker network create "+networkName)

	twoPorts := playbookHeader + `
  - name: Create a container with two published ports
    docker_container:
      name: ` + portName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      published_ports:
        - "127.0.0.1:48201:80"
        - "127.0.0.1:48202:81"
`
	if output := runPlaybookWithArgs(t, twoPorts, "--diff"); strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
		t.Fatalf("two-port setup failed: %s", output)
	}
	onePort := playbookHeader + `
  - name: Keep additional published ports by default
    docker_container:
      name: ` + portName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      published_ports:
        - "127.0.0.1:48201:80"
`
	if output := runPlaybookWithArgs(t, onePort, "--diff"); strings.Contains(output, "FAILED") || strings.Contains(output, "CHANGED") {
		t.Fatalf("default published_ports comparison was not allow-more-present: %s", output)
	}
	presentWithoutImage := playbookHeader + `
  - name: Keep an existing present container without repeating its image
    docker_container:
      name: ` + portName + `
      state: present
`
	if output := runPlaybookWithArgs(t, presentWithoutImage, "--diff"); strings.Contains(output, "FAILED") || strings.Contains(output, "CHANGED") {
		t.Fatalf("existing state=present required an image: %s", output)
	}
	strictAlias := playbookHeader + `
  - name: Purge an additional published port through the compatibility alias
    docker_container:
      name: ` + portName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      published_ports:
        - "127.0.0.1:48201:80"
      comparisons:
        ports: strict
`
	strictFirst := runPlaybookWithArgs(t, strictAlias, "--diff")
	strictSecond := runPlaybookWithArgs(t, strictAlias, "--diff")
	if strings.Contains(strictFirst, "FAILED") || !strings.Contains(strictFirst, "CHANGED") || strings.Contains(strictSecond, "FAILED") || strings.Contains(strictSecond, "CHANGED") {
		t.Fatalf("strict ports alias mismatch: first=%s second=%s", strictFirst, strictSecond)
	}

	networkCreate := playbookHeader + `
  - name: Attach requested networks while ignoring later network comparison
    docker_container:
      name: ` + networkContainer + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      networks:
        - name: ` + networkName + `
      comparisons:
        networks: ignore
`
	if output := runPlaybookWithArgs(t, networkCreate, "--diff"); strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
		t.Fatalf("ignored-network creation failed: %s", output)
	}
	if networks := remoteExec(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+networkContainer); !strings.Contains(networks, networkName) {
		t.Fatalf("creation skipped a network whose later comparison is ignored: %s", networks)
	}

	absent := playbookHeader + `
  - name: Remove without parsing irrelevant create-only settings
    docker_container:
      name: ` + networkContainer + `
      state: absent
      platform: invalid/platform/with/too/many/parts
      published_ports:
        - definitely-invalid
`
	if output := runPlaybookWithArgs(t, absent, "--diff"); strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
		t.Fatalf("absent state parsed irrelevant options: %s", output)
	}

	foreground := playbookHeader + `
  - name: Run in the foreground with a non-readable log driver
    docker_container:
      name: ` + foregroundName + `
      image: alpine:latest
      state: started
      command: ["true"]
      detach: false
      cleanup: true
      output_logs: true
      log_driver: none
`
	if output := runPlaybook(t, foreground); strings.Contains(output, "FAILED") || !strings.Contains(output, "CHANGED") {
		t.Fatalf("foreground non-readable logging driver mismatch: %s", output)
	}
	if exists := remoteExec(t, client, "docker inspect "+foregroundName+" >/dev/null 2>&1 && echo present || echo absent"); !strings.Contains(exists, "absent") {
		t.Fatalf("foreground non-readable logging cleanup failed: %s", exists)
	}

	autoRemoveFailure := playbookHeader + `
  - name: Preserve foreground failure status when auto-remove hides logs
    docker_container:
      name: ` + autoRemoveName + `
      image: alpine:latest
      state: started
      command: ["sh", "-c", "exit 7"]
      detach: false
      auto_remove: true
      output_logs: true
`
	if output := runPlaybook(t, autoRemoveFailure); !strings.Contains(output, "FAILED") || !strings.Contains(output, "Cannot retrieve result as auto_remove is enabled") {
		t.Fatalf("foreground auto-remove failure mismatch: %s", output)
	}
}
