//go:build integration

package integration

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPlaybook_DockerNetworkInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	name := "dibra-parity-net-info"
	remoteExec(t, client, "docker network rm "+name+" || true")
	defer remoteExec(t, client, "docker network rm "+name+" || true")
	remoteExec(t, client, "rm -f /tmp/dibra-network-info-*.json")

	templatePath := writeResultTemplate(t, "network_info")
	runInfo := func(resultName, networkName string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-network-info-" + resultName + ".json"
		playbook := playbookHeader + `
  - name: Inspect network
    community.docker.docker_network_info:
      name: ` + networkName + `
    register: network_info

  - name: Persist network info result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s network info playbook failed: %s", resultName, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("missing network returns exists false and network null", func(t *testing.T) {
		result := runInfo("missing", name)
		if result["changed"] != false || result["exists"] != false || result["network"] != nil {
			t.Fatalf("missing result = %#v", result)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"network":null`) {
			t.Fatalf("missing JSON = %s", encoded)
		}
	})

	create := playbookHeader + `
  - name: Create network for info
    community.docker.docker_network:
      name: ` + name + `
      state: present
`
	if output := runPlaybook(t, create); strings.Contains(output, "FAILED") {
		t.Fatalf("create network failed: %s", output)
	}

	t.Run("present network matches docker network inspect", func(t *testing.T) {
		result := runInfo("present", name)
		if result["changed"] != false || result["exists"] != true {
			t.Fatalf("present result = %#v", result)
		}
		actual, ok := result["network"].(map[string]any)
		if !ok {
			t.Fatalf("network = %T", result["network"])
		}
		if actual["Id"] == nil || actual["Name"] != name {
			t.Fatalf("raw keys missing: %#v", actual)
		}
		if _, found := actual["id"]; found {
			t.Fatalf("snake_case id leaked: %#v", actual)
		}

		raw, stderr, err := client.Run("docker network inspect " + name)
		if err != nil {
			t.Fatalf("docker network inspect: %v: %s", err, stderr)
		}
		var inspected []map[string]any
		if err := json.Unmarshal([]byte(raw), &inspected); err != nil || len(inspected) != 1 {
			t.Fatalf("decode docker inspect: %v\n%s", err, raw)
		}
		if !reflect.DeepEqual(actual, inspected[0]) {
			t.Fatalf("module inspection does not match docker inspect\nmodule: %#v\ndocker: %#v", actual, inspected[0])
		}
	})

	t.Run("check and diff modes stay read only", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Inspect network in check and diff mode
    docker_network_info:
      name: ` + name + `
`
		before := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" --format '{{.Id}}'"))
		for iteration := 0; iteration < 2; iteration++ {
			output := runPlaybookWithArgs(t, playbook, "--check", "--diff")
			if strings.Contains(output, "FAILED") || strings.Contains(output, "SKIPPED") ||
				strings.Contains(output, "CHANGED") || !strings.Contains(output, "OK") {
				t.Fatalf("read-only run %d failed: %s", iteration+1, output)
			}
		}
		after := strings.TrimSpace(remoteExec(t, client, "docker network inspect "+name+" --format '{{.Id}}'"))
		if before != after {
			t.Fatalf("check/diff mode mutated network id %s -> %s", before, after)
		}
	})
}
