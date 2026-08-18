//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerResourceUpdateParity ports the pinned update.yml
// live-resource bundle and proves updates happen in place with structured diffs.
func TestPlaybook_DockerContainerResourceUpdateParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const name = "dibra-container-resource-update-parity"
	remoteExec(t, client, "docker rm -f "+name+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")

	run := func(suffix, options string) map[string]any {
		t.Helper()
		return runContainerStateTask(t, client, "resource-update-"+suffix, `
      name: `+name+`
      image: alpine:latest
      command: ["sleep", "300"]
      state: started
      force_kill: true
`+options, "--diff")
	}

	initialOptions := `
      cpu_period: 100000
      cpu_quota: 50000
      cpu_shares: 512
      cpuset_cpus: "0"
      cpuset_mems: "0"
      memory: 256m
      memory_reservation: 128m
      memory_swap: 512m
      restart_policy: on-failure
      restart_retries: 2
`
	created := run("create", initialOptions)
	assertChanged(t, created, true)
	initialID := containerResultID(t, created)
	assertChanged(t, run("idempotent", initialOptions), false)

	updatedOptions := `
      cpu_period: 200000
      cpu_quota: 100000
      cpu_shares: 1024
      cpuset_cpus: "0"
      cpuset_mems: "0"
      memory: 512m
      memory_reservation: 256m
      memory_swap: 768m
      restart_policy: on-failure
      restart_retries: 3
`
	updated := run("update", updatedOptions)
	assertChanged(t, updated, true)
	if got := containerResultID(t, updated); got != initialID {
		t.Fatalf("live resource update recreated container: before=%s after=%s", initialID, got)
	}
	diff, ok := updated["diff"].(map[string]any)
	if !ok {
		t.Fatalf("resource update diff = %#v", updated["diff"])
	}
	before, beforeOK := diff["before"].(map[string]any)
	after, afterOK := diff["after"].(map[string]any)
	for _, key := range []string{
		"cpu_period", "cpu_quota", "cpu_shares", "memory",
		"memory_reservation", "memory_swap", "restart_retries",
	} {
		if !beforeOK || !afterOK || before[key] == nil || after[key] == nil {
			t.Fatalf("resource update diff missing %s: %#v", key, diff)
		}
	}
	assertChanged(t, run("updated-idempotent", updatedOptions), false)

	inspect := remoteExec(t, client, "docker inspect --format '{{.HostConfig.CpuPeriod}}|{{.HostConfig.CpuQuota}}|{{.HostConfig.CpuShares}}|{{.HostConfig.MemoryReservation}}|{{.HostConfig.MemorySwap}}|{{.HostConfig.RestartPolicy.Name}}|{{.HostConfig.RestartPolicy.MaximumRetryCount}}' "+name)
	for _, want := range []string{"200000", "100000", "1024", "268435456", "805306368", "on-failure", "3"} {
		if !strings.Contains(inspect, want) {
			t.Fatalf("updated resource inspect %q missing %q", inspect, want)
		}
	}
}
