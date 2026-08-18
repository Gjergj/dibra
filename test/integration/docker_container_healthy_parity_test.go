//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerHealthyStateParity independently ports the
// timeout and successful healthy-state transitions from pinned healthy.yml.
func TestPlaybook_DockerContainerHealthyStateParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		name        = "dibra-container-healthy-state-parity"
		cliName     = "dibra-container-healthcheck-cli-compatible"
		classicName = "dibra-container-healthcheck-classic"
	)
	remoteExec(t, client, "docker rm -f "+name+" "+cliName+" "+classicName+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" "+cliName+" "+classicName+" || true")

	notHealthyArgs := `
      name: ` + name + `
      image: alpine:latest
      command: ["/bin/sh", "-c", "sleep 600"]
      state: stopped
      force_kill: true
      healthcheck:
        test: ["CMD-SHELL", "test -f /tmp/never-ready"]
        interval: 1s
        timeout: 1s
        start_interval: 500ms
        start_period: 30s
        retries: 1
`
	prepared := runContainerHealthyTask(t, client, "prepare-timeout", notHealthyArgs)
	assertChanged(t, prepared, true)
	assertContainerRunning(t, client, name, false)
	assertContainerHealthConfig(t, client, name, "test -f /tmp/never-ready", 1_000_000_000, 1_000_000_000, 500_000_000, 30_000_000_000, 1)

	healthyTimeoutArgs := `
      name: ` + name + `
      state: healthy
      healthy_wait_timeout: 1
`
	predicted := runContainerHealthyTask(t, client, "timeout-check", healthyTimeoutArgs, "--check", "--diff")
	assertChanged(t, predicted, true)
	assertContainerRunning(t, client, name, false)

	timedOut := runContainerAgentRequest(t, client, map[string]any{
		"name": name, "state": "healthy", "healthy_wait_timeout": 1.0,
	})
	if timedOut["failed"] != true {
		t.Fatalf("timeout result did not fail: %#v", timedOut)
	}
	msg, _ := timedOut["msg"].(string)
	if !strings.HasPrefix(msg, `Timeout of 1.0 seconds exceeded while waiting for container "`) {
		t.Fatalf("timeout msg = %q", msg)
	}
	if got := containerHealthStatus(t, timedOut); got != "starting" {
		t.Fatalf("timeout health status = %q, want starting", got)
	}
	assertContainerRunning(t, client, name, true)

	becomesHealthyArgs := `
      name: ` + name + `
      image: alpine:latest
      command: ["/bin/sh", "-c", "sleep 2; touch /tmp/ready; sleep 600"]
      state: stopped
      force_kill: true
      healthcheck:
        test: ["CMD-SHELL", "test -f /tmp/ready"]
        interval: 1s
        timeout: 1s
        start_interval: 500ms
        start_period: 10s
        retries: 3
`
	reprepared := runContainerHealthyTask(t, client, "prepare-success", becomesHealthyArgs)
	assertChanged(t, reprepared, true)
	assertContainerRunning(t, client, name, false)
	assertContainerHealthConfig(t, client, name, "test -f /tmp/ready", 1_000_000_000, 1_000_000_000, 500_000_000, 10_000_000_000, 3)

	healthyArgs := `
      name: ` + name + `
      state: healthy
      healthy_wait_timeout: 10
`
	healthy := runContainerHealthyTask(t, client, "success", healthyArgs, "--diff")
	assertChanged(t, healthy, true)
	if got := containerHealthStatus(t, healthy); got != "healthy" {
		t.Fatalf("successful health status = %q, want healthy", got)
	}
	assertContainerRunning(t, client, name, true)

	idempotent := runContainerHealthyTask(t, client, "success-idempotent", healthyArgs, "--diff")
	assertChanged(t, idempotent, false)
	if got := containerHealthStatus(t, idempotent); got != "healthy" {
		t.Fatalf("idempotent health status = %q, want healthy", got)
	}

	cliCompatibleArgs := `
      name: ` + cliName + `
      image: alpine:latest
      state: present
      healthcheck:
        interval: 5s
        test_cli_compatible: true
`
	assertChanged(t, runContainerHealthyTask(t, client, "cli-compatible", cliCompatibleArgs), true)
	assertChanged(t, runContainerHealthyTask(t, client, "cli-compatible-idempotent", cliCompatibleArgs), false)
	assertContainerHealthcheckTest(t, client, cliName, nil)

	classicArgs := `
      name: ` + classicName + `
      image: alpine:latest
      state: present
      healthcheck:
        interval: 5s
`
	assertChanged(t, runContainerHealthyTask(t, client, "classic", classicArgs), true)
	assertChanged(t, runContainerHealthyTask(t, client, "classic-idempotent", classicArgs), false)
	assertContainerHealthcheckTest(t, client, classicName, []string{"NONE"})
}

func runContainerHealthyTask(
	t *testing.T,
	client *ssh.Client,
	suffix string,
	arguments string,
	extra ...string,
) map[string]any {
	t.Helper()
	templatePath := writeResultTemplate(t, "healthy_result")
	remotePath := "/tmp/dibra-container-healthy-" + suffix + ".json"
	playbook := playbookHeader + `
  - name: Manage healthy container
    community.docker.docker_container:
` + arguments + `
    register: healthy_result

  - name: Persist healthy result
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

func runContainerAgentRequest(t *testing.T, client *ssh.Client, args map[string]any) map[string]any {
	return runContainerAgentRequestWithDiff(t, client, args, false)
}

func runContainerAgentRequestWithDiff(t *testing.T, client *ssh.Client, args map[string]any, diff bool) map[string]any {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"module":     "community.docker.docker_container",
		"args":       args,
		"check_mode": false,
		"diff":       diff,
	})
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := client.Run("printf '%s' '" + string(request) + "' | /tmp/.dibra-agent")
	if err != nil {
		t.Fatalf("healthy agent request failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode healthy result: %v\nstdout: %s", err, stdout)
	}
	return result
}

func containerHealthStatus(t *testing.T, result map[string]any) string {
	t.Helper()
	containerResult, ok := result["container"].(map[string]any)
	if !ok {
		t.Fatalf("container result = %#v", result["container"])
	}
	state, ok := containerResult["State"].(map[string]any)
	if !ok {
		t.Fatalf("container state = %#v", containerResult["State"])
	}
	health, ok := state["Health"].(map[string]any)
	if !ok {
		t.Fatalf("container health = %#v", state["Health"])
	}
	status, _ := health["Status"].(string)
	return status
}

func assertContainerHealthConfig(
	t *testing.T,
	client *ssh.Client,
	name, command string,
	interval, timeout, startInterval, startPeriod, retries int,
) {
	t.Helper()
	raw := mustRemote(t, client, "docker inspect --format '{{json .Config.Healthcheck}}' "+name)
	var health map[string]any
	if err := json.Unmarshal([]byte(raw), &health); err != nil {
		t.Fatalf("decode healthcheck: %v\n%s", err, raw)
	}
	test, _ := health["Test"].([]any)
	if len(test) != 2 || test[0] != "CMD-SHELL" || test[1] != command {
		t.Fatalf("healthcheck test = %#v", test)
	}
	for field, want := range map[string]int{
		"Interval": interval, "Timeout": timeout, "StartInterval": startInterval,
		"StartPeriod": startPeriod, "Retries": retries,
	} {
		if got := numberValue(health[field]); got != want {
			t.Fatalf("healthcheck %s = %v, want %v; health=%#v", field, got, want, health)
		}
	}
}

func assertContainerHealthcheckTest(t *testing.T, client *ssh.Client, name string, expected []string) {
	t.Helper()
	raw := mustRemote(t, client, "docker inspect --format '{{json .Config.Healthcheck}}' "+name)
	var health map[string]any
	if err := json.Unmarshal([]byte(raw), &health); err != nil {
		t.Fatalf("decode healthcheck: %v\n%s", err, raw)
	}
	actualValues, _ := health["Test"].([]any)
	actual := make([]string, len(actualValues))
	for index, value := range actualValues {
		actual[index], _ = value.(string)
	}
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("healthcheck test = %#v, want %#v; health=%#v", actual, expected, health)
	}
}
