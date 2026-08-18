//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerDeviceOptionsParity ports the pinned block-I/O
// ordering/idempotency matrix and proves the complete DeviceRequest API shape.
func TestPlaybook_DockerContainerDeviceOptionsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const name = "dibra-container-device-options-parity"
	remoteExec(t, client, "docker rm -f "+name+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")

	t.Run("block io paths rates ordering and changes", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		base := `
      name: ` + name + `
      image: alpine:latest
      state: present
`
		initialArgs := base + `
      device_read_bps:
        - path: /dev/random
          rate: 20M
        - path: /dev/urandom
          rate: 10K
      device_read_iops:
        - path: /dev/random
          rate: 10
        - path: /dev/urandom
          rate: 20
      device_write_bps:
        - path: /dev/random
          rate: 10M
      device_write_iops:
        - path: /dev/urandom
          rate: 30
`
		created := runContainerStateTask(t, client, "device-limits-create", initialArgs, "--diff")
		assertChanged(t, created, true)
		containerID := containerResultID(t, created)
		assertContainerDeviceLimits(t, client, name, map[string]map[string]int{
			"BlkioDeviceReadBps":   {"/dev/random": 20 * 1024 * 1024, "/dev/urandom": 10 * 1024},
			"BlkioDeviceReadIOps":  {"/dev/random": 10, "/dev/urandom": 20},
			"BlkioDeviceWriteBps":  {"/dev/random": 10 * 1024 * 1024},
			"BlkioDeviceWriteIOps": {"/dev/urandom": 30},
		})

		reorderedArgs := base + `
      device_read_bps:
        - path: /dev/urandom
          rate: 10K
        - path: /dev/random
          rate: 20M
      device_read_iops:
        - path: /dev/urandom
          rate: "20"
        - path: /dev/random
          rate: 10
      device_write_bps:
        - path: /dev/random
          rate: 10M
      device_write_iops:
        - path: /dev/urandom
          rate: 30
`
		reordered := runContainerStateTask(t, client, "device-limits-idempotent", reorderedArgs, "--diff")
		assertChanged(t, reordered, false)
		if got := containerResultID(t, reordered); got != containerID {
			t.Fatalf("device limit reordering recreated container: before=%s after=%s", containerID, got)
		}

		lessArgs := base + `
      device_read_bps:
        - path: /dev/random
          rate: 20M
      device_read_iops:
        - path: /dev/random
          rate: 10
      device_write_bps:
        - path: /dev/random
          rate: 10M
      device_write_iops:
        - path: /dev/urandom
          rate: 30
`
		assertChanged(t, runContainerStateTask(t, client, "device-limits-less", lessArgs, "--diff"), false)

		changedArgs := strings.Replace(initialArgs, "rate: 20M", "rate: 30M", 1)
		predicted := runContainerStateTask(t, client, "device-limits-check", changedArgs, "--check", "--diff")
		assertChanged(t, predicted, true)
		if got := mustRemote(t, client, "docker inspect --format '{{.Id}}' "+name); got != containerID {
			t.Fatalf("check mode changed container: before=%s after=%s", containerID, got)
		}
		changed := runContainerStateTask(t, client, "device-limits-change", changedArgs, "--diff")
		assertChanged(t, changed, true)
		if got := containerResultID(t, changed); got == containerID {
			t.Fatalf("changed device limit kept container ID %s", containerID)
		}
	})

	t.Run("device request nested fields", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		args := `
      name: ` + name + `
      image: alpine:latest
      state: present
      device_requests:
        - driver: nvidia
          count: 2
          capabilities:
            - [gpu]
          options:
            mode: test
        - driver: cdi
          device_ids:
            - vendor.com/device=test
          capabilities:
            - [gpu]
`
		assertChanged(t, runContainerStateTask(t, client, "device-requests-create", args, "--diff"), true)
		assertChanged(t, runContainerStateTask(t, client, "device-requests-idempotent", args, "--diff"), false)
		raw := mustRemote(t, client, "docker inspect --format '{{json .HostConfig.DeviceRequests}}' "+name)
		for _, expected := range []string{
			`"Driver":"nvidia"`, `"Count":2`, `"Capabilities":[["gpu"]]`,
			`"Options":{"mode":"test"}`, `"Driver":"cdi"`,
			`"DeviceIDs":["vendor.com/device=test"]`,
		} {
			if !strings.Contains(raw, expected) {
				t.Fatalf("device requests are missing %s: %s", expected, raw)
			}
		}
	})
}

func assertContainerDeviceLimits(t *testing.T, client *ssh.Client, name string, expected map[string]map[string]int) {
	t.Helper()
	raw := mustRemote(t, client, "docker inspect --format '{{json .HostConfig}}' "+name)
	var host map[string]any
	if err := json.Unmarshal([]byte(raw), &host); err != nil {
		t.Fatalf("decode host config: %v\n%s", err, raw)
	}
	for field, expectedDevices := range expected {
		devices, _ := host[field].([]any)
		actual := make(map[string]int, len(devices))
		for _, item := range devices {
			device, _ := item.(map[string]any)
			path, _ := device["Path"].(string)
			actual[path] = numberValue(device["Rate"])
		}
		for path, rate := range expectedDevices {
			if actual[path] != rate {
				t.Fatalf("%s[%s] = %d, want %d; host=%#v", field, path, actual[path], rate, host[field])
			}
		}
	}
}
