//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

type containerComparisonStep struct {
	name       string
	arguments  string
	changed    bool
	inspect    string
	inspectOut string
}

// TestPlaybook_DockerContainerComparisonsParity independently ports the
// value/list/set/set(dict)/dict/wildcard matrix from the pinned upstream
// comparisons.yml target.
func TestPlaybook_DockerContainerComparisonsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		alpine = "quay.io/ansible/docker-test-containers:alpine3.8"
		hello  = "quay.io/ansible/docker-test-containers:hello-world"
	)
	mustRemote(t, client, "docker pull "+alpine)
	mustRemote(t, client, "docker pull "+hello)

	t.Run("value comparison", func(t *testing.T) {
		runContainerComparisonScenario(t, client, "value", []containerComparisonStep{
			{name: "initial", changed: true, arguments: `
      hostname: example.com
`, inspect: "{{.Config.Hostname}}", inspectOut: "example.com"},
			{name: "ignore", changed: false, arguments: `
      hostname: example.org
      comparisons:
        hostname: ignore
`, inspect: "{{.Config.Hostname}}", inspectOut: "example.com"},
			{name: "strict", changed: true, arguments: `
      hostname: example.org
      comparisons:
        hostname: strict
`, inspect: "{{.Config.Hostname}}", inspectOut: "example.org"},
		}, alpine)
	})

	t.Run("list comparison", func(t *testing.T) {
		runContainerComparisonScenario(t, client, "list", []containerComparisonStep{
			{name: "initial", changed: true, arguments: `
      dns_servers: [1.1.1.1, 8.8.8.8]
`, inspect: "{{json .HostConfig.Dns}}", inspectOut: `["1.1.1.1","8.8.8.8"]`},
			{name: "ignore", changed: false, arguments: `
      dns_servers: [9.9.9.9]
      comparisons:
        dns_servers: ignore
`, inspect: "{{json .HostConfig.Dns}}", inspectOut: `["1.1.1.1","8.8.8.8"]`},
			{name: "strict", changed: true, arguments: `
      dns_servers: [9.9.9.9]
      comparisons:
        dns_servers: strict
`, inspect: "{{json .HostConfig.Dns}}", inspectOut: `["9.9.9.9"]`},
		}, alpine)
	})

	t.Run("set comparison", func(t *testing.T) {
		runContainerComparisonScenario(t, client, "set", []containerComparisonStep{
			{name: "initial", changed: true, arguments: `
      groups: ["1010", "1011"]
`},
			{name: "ignore", changed: false, arguments: `
      groups: ["1010", "1011", "1012"]
      comparisons:
        groups: ignore
`},
			{name: "allow more add", changed: true, arguments: `
      groups: ["1010", "1011", "1012"]
      comparisons:
        groups: allow_more_present
`},
			{name: "allow more subset", changed: false, arguments: `
      groups: ["1010", "1012"]
      comparisons:
        groups: allow_more_present
`},
			{name: "strict subset", changed: true, arguments: `
      groups: ["1010", "1012"]
      comparisons:
        groups: strict
`},
		}, alpine)
	})

	t.Run("set dictionary comparison", func(t *testing.T) {
		runContainerComparisonScenario(t, client, "set-dict", []containerComparisonStep{
			{name: "initial", changed: true, arguments: `
      devices:
        - /dev/random:/dev/virt-random:rwm
        - /dev/urandom:/dev/virt-urandom:rwm
`},
			{name: "ignore", changed: false, arguments: `
      devices:
        - /dev/random:/dev/virt-random:rwm
        - /dev/urandom:/dev/virt-urandom:rwm
        - /dev/null:/dev/virt-null:rwm
      comparisons:
        devices: ignore
`},
			{name: "allow more add", changed: true, arguments: `
      devices:
        - /dev/random:/dev/virt-random:rwm
        - /dev/urandom:/dev/virt-urandom:rwm
        - /dev/null:/dev/virt-null:rwm
      comparisons:
        devices: allow_more_present
`},
			{name: "allow more subset", changed: false, arguments: `
      devices:
        - /dev/random:/dev/virt-random:rwm
        - /dev/null:/dev/virt-null:rwm
      comparisons:
        devices: allow_more_present
`},
			{name: "strict subset", changed: true, arguments: `
      devices:
        - /dev/random:/dev/virt-random:rwm
        - /dev/null:/dev/virt-null:rwm
      comparisons:
        devices: strict
`},
		}, alpine)
	})

	t.Run("dictionary comparison", func(t *testing.T) {
		runContainerComparisonScenario(t, client, "dict", []containerComparisonStep{
			{name: "initial", changed: true, arguments: `
      labels:
        ansible.test.1: hello
        ansible.test.2: world
`},
			{name: "ignore", changed: false, arguments: `
      labels:
        ansible.test.1: hello
        ansible.test.2: world
        ansible.test.3: ansible
      comparisons:
        labels: ignore
`},
			{name: "allow more add", changed: true, arguments: `
      labels:
        ansible.test.1: hello
        ansible.test.2: world
        ansible.test.3: ansible
      comparisons:
        labels: allow_more_present
`},
			{name: "allow more subset", changed: false, arguments: `
      labels:
        ansible.test.1: hello
        ansible.test.3: ansible
      comparisons:
        labels: allow_more_present
`},
			{name: "strict subset", changed: true, arguments: `
      labels:
        ansible.test.1: hello
        ansible.test.3: ansible
      comparisons:
        labels: strict
`},
		}, alpine)
	})

	t.Run("wildcard comparison", func(t *testing.T) {
		name := "dibra-container-comparison-wildcard"
		remoteExec(t, client, "docker rm -f "+name+" || true")
		defer remoteExec(t, client, "docker rm -f "+name+" || true")
		base := func(image string) string {
			return `
      name: ` + name + `
      image: ` + image + `
      command: ["/bin/sh", "-c", "sleep 600"]
      state: started
      force_kill: true
`
		}
		first := runContainerStateTask(t, client, "comparison-wildcard-initial", base(alpine)+`
      hostname: example.com
      stop_timeout: 1
      labels:
        ansible.test.1: hello
        ansible.test.2: world
        ansible.test.3: ansible
`, "--diff")
		assertChanged(t, first, true)
		firstID := containerResultID(t, first)

		ignored := runContainerStateTask(t, client, "comparison-wildcard-ignore", base(hello)+`
      hostname: example.org
      stop_timeout: 2
      labels:
        ansible.test.1: hello
        ansible.test.4: ignore
      comparisons:
        '*': ignore
`, "--diff")
		assertChanged(t, ignored, false)
		if got := containerResultID(t, ignored); got != firstID {
			t.Fatalf("wildcard ignore recreated container: before=%s after=%s", firstID, got)
		}
		if got := remoteExec(t, client, "docker inspect --format '{{.Config.Image}}' "+name); got != alpine {
			t.Fatalf("wildcard ignore image = %s, want %s", got, alpine)
		}

		strictArgs := base(alpine) + `
      hostname: example.org
      stop_timeout: 1
      labels:
        ansible.test.1: hello
        ansible.test.2: world
        ansible.test.3: ansible
      comparisons:
        '*': strict
`
		strict := runContainerStateTask(t, client, "comparison-wildcard-strict", strictArgs, "--diff")
		assertChanged(t, strict, true)
		strictID := containerResultID(t, strict)
		if strictID == firstID {
			t.Fatalf("wildcard strict kept container ID %s", firstID)
		}
		assertChanged(t, runContainerStateTask(t, client, "comparison-wildcard-idempotent", strictArgs, "--diff"), false)
	})
}

func runContainerComparisonScenario(
	t *testing.T,
	client *ssh.Client,
	suffix string,
	steps []containerComparisonStep,
	image string,
) {
	t.Helper()
	name := "dibra-container-comparison-" + suffix
	remoteExec(t, client, "docker rm -f "+name+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")
	base := `
      name: ` + name + `
      image: ` + image + `
      command: ["/bin/sh", "-c", "sleep 600"]
      state: started
      force_kill: true
`
	var previousID string
	for _, step := range steps {
		resultSuffix := strings.ReplaceAll(step.name, " ", "-")
		result := runContainerStateTask(t, client, "comparison-"+suffix+"-"+resultSuffix, base+step.arguments, "--diff")
		assertChanged(t, result, step.changed)
		currentID := containerResultID(t, result)
		if previousID != "" {
			if step.changed && currentID == previousID {
				t.Fatalf("%s/%s changed without recreation: %s", suffix, step.name, currentID)
			}
			if !step.changed && currentID != previousID {
				t.Fatalf("%s/%s recreated while ignored/allow-more: before=%s after=%s", suffix, step.name, previousID, currentID)
			}
		}
		if step.inspect != "" {
			if got := remoteExec(t, client, "docker inspect --format '"+step.inspect+"' "+name); got != step.inspectOut {
				t.Fatalf("%s/%s inspect = %q, want %q", suffix, step.name, got, step.inspectOut)
			}
		}
		previousID = currentID
	}
}
