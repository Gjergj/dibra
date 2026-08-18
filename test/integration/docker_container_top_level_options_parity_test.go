//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerTopLevelOptionsParity ports the pinned
// create/reorder/less/more matrices for top-level set and dictionary options.
func TestPlaybook_DockerContainerTopLevelOptionsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	setCases := []struct {
		name      string
		initial   string
		reordered string
		less      string
		more      string
		strict    bool
	}{
		{
			name: "capabilities",
			initial: `
      capabilities:
        - sys_time
      cap_drop:
        - all
`,
			reordered: `
      cap_drop:
        - all
      capabilities:
        - sys_time
`,
			less: `
      capabilities: []
      cap_drop:
        - all
`,
			more: `
      capabilities:
        - setgid
      cap_drop:
        - all
`,
		},
		{
			name:   "device_cgroup_rules",
			strict: true,
			initial: `
      device_cgroup_rules:
        - c 1:3 rwm
        - c 1:5 r
`,
			reordered: `
      device_cgroup_rules:
        - c 1:5 r
        - c 1:3 rwm
`,
			less: `
      device_cgroup_rules:
        - c 1:3 rwm
`,
			more: `
      device_cgroup_rules:
        - c 1:3 rwm
        - c 1:7 rw
`,
		},
		{
			name: "dns_opts",
			initial: `
      dns_opts:
        - timeout:10
        - rotate
`,
			reordered: `
      dns_opts:
        - rotate
        - timeout:10
`,
			less: `
      dns_opts:
        - timeout:10
`,
			more: `
      dns_opts:
        - timeout:10
        - no-check-names
`,
		},
		{
			name:   "dns_search_domains",
			strict: true,
			initial: `
      dns_search_domains:
        - example.test
        - parity.test
`,
			reordered: `
      dns_search_domains:
        - parity.test
        - example.test
`,
			less: `
      dns_search_domains:
        - example.test
`,
			more: `
      dns_search_domains:
        - example.test
        - added.test
`,
		},
		{
			name: "env",
			initial: `
      env:
        FIRST: one
        SECOND: two
`,
			reordered: `
      env:
        SECOND: two
        FIRST: one
`,
			less: `
      env:
        FIRST: one
`,
			more: `
      env:
        FIRST: one
        THIRD: three
`,
		},
		{
			name: "etc_hosts",
			initial: `
      etc_hosts:
        one.example: 192.0.2.10
        two.example: 192.0.2.20
`,
			reordered: `
      etc_hosts:
        two.example: 192.0.2.20
        one.example: 192.0.2.10
`,
			less: `
      etc_hosts:
        one.example: 192.0.2.10
`,
			more: `
      etc_hosts:
        one.example: 192.0.2.10
        three.example: 192.0.2.30
`,
		},
		{
			name: "log_options",
			initial: `
      log_driver: json-file
      log_options:
        labels: production_status
        env: os,customer
        max-file: "5"
`,
			reordered: `
      log_driver: json-file
      log_options:
        max-file: "5"
        labels: production_status
        env: os,customer
`,
			less: `
      log_driver: json-file
      log_options:
        labels: production_status
`,
			more: `
      log_driver: json-file
      log_options:
        labels: production_status
        max-size: 10m
`,
		},
		{
			name: "security_opts",
			initial: `
      security_opts:
        - no-new-privileges:true
        - seccomp:unconfined
`,
			reordered: `
      security_opts:
        - seccomp:unconfined
        - no-new-privileges:true
`,
			less: `
      security_opts:
        - no-new-privileges:true
`,
			more: `
      security_opts:
        - no-new-privileges:true
        - label:disable
`,
		},
		{
			name: "sysctls",
			initial: `
      sysctls:
        net.ipv4.icmp_echo_ignore_all: "1"
        net.ipv4.ip_forward: "1"
`,
			reordered: `
      sysctls:
        net.ipv4.ip_forward: "1"
        net.ipv4.icmp_echo_ignore_all: "1"
`,
			less: `
      sysctls:
        net.ipv4.icmp_echo_ignore_all: "1"
`,
			more: `
      sysctls:
        net.ipv4.icmp_echo_ignore_all: "1"
        net.core.somaxconn: "1024"
`,
		},
		{
			name: "tmpfs",
			initial: `
      tmpfs:
        - /test1:rw,noexec,nosuid,size=65536k
        - /test2:rw,noexec,nosuid,size=65536k
`,
			reordered: `
      tmpfs:
        - /test2:rw,noexec,nosuid,size=65536k
        - /test1:rw,noexec,nosuid,size=65536k
`,
			less: `
      tmpfs:
        - /test1:rw,noexec,nosuid,size=65536k
`,
			more: `
      tmpfs:
        - /test1:rw,noexec,nosuid,size=65536k
        - /test3:rw,noexec,nosuid,size=65536k
`,
		},
		{
			name: "ulimits",
			initial: `
      ulimits:
        - nofile:1234:1234
        - nproc:3:6
`,
			reordered: `
      ulimits:
        - nproc:3:6
        - nofile:1234:1234
`,
			less: `
      ulimits:
        - nofile:1234:1234
`,
			more: `
      ulimits:
        - nofile:1234:1234
        - sigpending:100:200
`,
		},
	}
	for _, test := range setCases {
		t.Run(test.name, func(t *testing.T) {
			name := "dibra-container-top-level-" + strings.ReplaceAll(test.name, "_", "-")
			remoteExec(t, client, "docker rm -f "+name+" || true")
			defer remoteExec(t, client, "docker rm -f "+name+" || true")

			initial := runTopLevelContainerOption(t, client, test.name+"-initial", name, test.initial)
			assertChanged(t, initial, true)
			initialID := containerResultID(t, initial)

			reordered := runTopLevelContainerOption(t, client, test.name+"-reordered", name, test.reordered)
			assertChanged(t, reordered, test.strict)
			reorderedID := containerResultID(t, reordered)
			if test.strict && reorderedID == initialID {
				t.Fatalf("strict list reordering did not recreate container: %s", initialID)
			}
			if !test.strict && reorderedID != initialID {
				t.Fatalf("set reordering recreated container: before=%s after=%s", initialID, reorderedID)
			}

			less := runTopLevelContainerOption(t, client, test.name+"-less", name, test.less)
			assertChanged(t, less, test.strict)
			lessID := containerResultID(t, less)
			if test.strict && lessID == reorderedID {
				t.Fatalf("strict list reduction did not recreate container: %s", reorderedID)
			}
			if !test.strict && lessID != initialID {
				t.Fatalf("allow-more-present reduction recreated container: before=%s after=%s", initialID, lessID)
			}

			more := runTopLevelContainerOption(t, client, test.name+"-more", name, test.more)
			assertChanged(t, more, true)
			moreID := containerResultID(t, more)
			if moreID == lessID {
				t.Fatalf("new requested value did not recreate container: %s", lessID)
			}
			assertChanged(t, runTopLevelContainerOption(t, client, test.name+"-final", name, test.more), false)
		})
	}

	scalarCases := []struct {
		name    string
		initial string
		changed string
	}{
		{name: "cgroup_parent", initial: "      cgroup_parent: dibra.slice\n", changed: "      cgroup_parent: dibra-changed.slice\n"},
		{name: "cgroupns_mode", initial: "      cgroupns_mode: private\n", changed: "      cgroupns_mode: host\n"},
		{name: "domainname", initial: "      domainname: example.test\n", changed: "      domainname: changed.test\n"},
		{name: "init", initial: "      init: true\n", changed: "      init: false\n"},
		{name: "interactive", initial: "      interactive: false\n", changed: "      interactive: true\n"},
		{name: "ipc_mode", initial: "      ipc_mode: private\n", changed: "      ipc_mode: shareable\n"},
		{name: "log_driver", initial: "      log_driver: json-file\n", changed: "      log_driver: syslog\n"},
		{name: "oom_score_adj", initial: "      oom_score_adj: -100\n", changed: "      oom_score_adj: 100\n"},
		{name: "pid_mode", initial: "      pid_mode: host\n", changed: "      pid_mode: \"\"\n"},
		{name: "privileged", initial: "      privileged: false\n", changed: "      privileged: true\n"},
		{name: "read_only", initial: "      read_only: false\n", changed: "      read_only: true\n"},
		{name: "runtime", initial: "      runtime: runc\n", changed: "      runtime: io.containerd.runc.v2\n"},
		{name: "shm_size", initial: "      shm_size: 96M\n", changed: "      shm_size: 75M\n"},
		{name: "stop_signal", initial: "      stop_signal: SIGTERM\n", changed: "      stop_signal: SIGKILL\n"},
		{
			name:    "storage_opts",
			initial: "      storage_opts:\n        size: 12m\n",
			changed: "      storage_opts:\n        size: 20m\n",
		},
		{name: "tty", initial: "      tty: false\n", changed: "      tty: true\n"},
		{name: "user", initial: "      user: root\n", changed: "      user: nobody\n"},
		{name: "userns_mode", initial: "      userns_mode: host\n", changed: "      userns_mode: \"\"\n"},
		{name: "uts", initial: "      uts: host\n", changed: "      uts: \"\"\n"},
		{name: "working_dir", initial: "      working_dir: /tmp\n", changed: "      working_dir: /\n"},
	}
	for _, test := range scalarCases {
		t.Run(test.name, func(t *testing.T) {
			name := "dibra-container-top-level-" + strings.ReplaceAll(test.name, "_", "-")
			remoteExec(t, client, "docker rm -f "+name+" || true")
			defer remoteExec(t, client, "docker rm -f "+name+" || true")

			initial := runTopLevelContainerOption(t, client, test.name+"-initial", name, test.initial)
			assertChanged(t, initial, true)
			initialID := containerResultID(t, initial)
			assertChanged(t, runTopLevelContainerOption(t, client, test.name+"-idempotent", name, test.initial), false)
			changed := runTopLevelContainerOption(t, client, test.name+"-changed", name, test.changed)
			assertChanged(t, changed, true)
			if got := containerResultID(t, changed); got == initialID {
				t.Fatalf("scalar change did not recreate container: %s", initialID)
			}
			assertChanged(t, runTopLevelContainerOption(t, client, test.name+"-final", name, test.changed), false)
		})
	}

	t.Run("oom_killer", func(t *testing.T) {
		const name = "dibra-container-top-level-oom-killer"
		remoteExec(t, client, "docker rm -f "+name+" || true")
		defer remoteExec(t, client, "docker rm -f "+name+" || true")

		disabled := runTopLevelContainerOption(t, client, "oom-killer-disabled", name, "      oom_killer: false\n")
		assertChanged(t, disabled, true)
		assertChanged(t, runTopLevelContainerOption(t, client, "oom-killer-disabled-idempotent", name, "      oom_killer: false\n"), false)

		unsupported := runTopLevelContainerOption(t, client, "oom-killer-host-capability-warning", name, "      oom_killer: true\n")
		assertChanged(t, unsupported, true)
		assertContainerWarningContains(t, unsupported, "OomKillDisable")
	})

	t.Run("blkio_weight", func(t *testing.T) {
		const name = "dibra-container-top-level-blkio-weight"
		remoteExec(t, client, "docker rm -f "+name+" || true")
		defer remoteExec(t, client, "docker rm -f "+name+" || true")

		run := func(suffix, value string) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "top-level-blkio-weight-"+suffix, `
      name: `+name+`
      image: alpine:latest
      state: present
      blkio_weight: `+value+`
`, "--diff")
		}
		created := run("create", "500")
		assertChanged(t, created, true)
		initialID := containerResultID(t, created)
		assertChanged(t, run("idempotent", "500"), false)
		changed := run("changed", "600")
		assertChanged(t, changed, true)
		if got := containerResultID(t, changed); got != initialID {
			t.Fatalf("blkio_weight live update recreated container: before=%s after=%s", initialID, got)
		}
		if got := remoteExec(t, client, "docker inspect --format '{{.HostConfig.BlkioWeight}}' "+name); got != "600" {
			t.Fatalf("blkio_weight = %q, want 600", got)
		}
		assertChanged(t, run("changed-idempotent", "600"), false)
	})

	t.Run("memory_swappiness", func(t *testing.T) {
		const name = "dibra-container-top-level-memory-swappiness"
		remoteExec(t, client, "docker rm -f "+name+" || true")
		defer remoteExec(t, client, "docker rm -f "+name+" || true")

		run := func(suffix string) map[string]any {
			t.Helper()
			return runContainerStateTask(t, client, "top-level-memory-swappiness-"+suffix, `
      name: `+name+`
      image: alpine:latest
      state: present
      memory_swappiness: 50
`, "--diff")
		}
		unsupported := run("host-capability-warning")
		assertChanged(t, unsupported, true)
		assertContainerWarningContains(t, unsupported, "memory swappiness")
		repeated := run("host-capability-warning-repeated")
		assertChanged(t, repeated, true)
		assertContainerWarningContains(t, repeated, "memory swappiness")
	})
}

func runTopLevelContainerOption(
	t *testing.T,
	client *ssh.Client,
	suffix, name, optionYAML string,
) map[string]any {
	t.Helper()
	return runContainerStateTask(t, client, "top-level-"+suffix, `
      name: `+name+`
      image: alpine:latest
      command: ["sleep", "300"]
      state: started
      force_kill: true
`+optionYAML, "--diff")
}

func assertContainerWarningContains(t *testing.T, result map[string]any, needle string) {
	t.Helper()
	warnings, ok := result["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("missing Engine warning containing %q: %#v", needle, result["warnings"])
	}
	for _, warning := range warnings {
		if strings.Contains(warning.(string), needle) {
			return
		}
	}
	t.Fatalf("Engine warnings %#v do not contain %q", warnings, needle)
}
