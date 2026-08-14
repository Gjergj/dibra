//go:build integration

package integration

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func TestPlaybook_DockerVolumeInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	name := "dibra-volume-info-parity"
	mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-volume-info-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker volume rm -f "+name+" >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "volume_info_result")
	runInfo := func(testName, arguments string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-volume-info-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Inspect volume
    community.docker.docker_volume_info:
` + arguments + `
    register: volume_info_result

  - name: Persist volume info result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s volume info playbook failed: %s", testName, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("missing volume is exists false and volume null", func(t *testing.T) {
		result := runInfo("missing", "      name: "+name+"\n")
		if result["changed"] != false || result["exists"] != false {
			t.Fatalf("result = %#v", result)
		}
		if _, found := result["volume"]; !found || result["volume"] != nil {
			t.Fatalf("volume = %#v", result["volume"])
		}
	})

	t.Run("present volume matches docker volume inspect", func(t *testing.T) {
		create := playbookHeader + `
  - name: Create volume
    docker_volume:
      name: ` + name + `
`
		if output := runPlaybook(t, create); strings.Contains(output, "FAILED") {
			t.Fatalf("create failed: %s", output)
		}
		result := runInfo("present", "      name: "+name+"\n")
		if result["changed"] != false || result["exists"] != true {
			t.Fatalf("result = %#v", result)
		}
		actual, ok := result["volume"].(map[string]any)
		if !ok {
			t.Fatalf("volume = %#v", result["volume"])
		}
		expected := dockerInspectVolume(t, client, name)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("module inspection does not match docker volume inspect\nmodule: %#v\ndocker: %#v", actual, expected)
		}
		for _, key := range []string{"Name", "Driver", "Mountpoint", "CreatedAt", "Scope", "Labels", "Options"} {
			if _, found := actual[key]; !found {
				t.Fatalf("raw key %q missing from %#v", key, actual)
			}
		}
	})
}

func dockerInspectVolume(t *testing.T, client *ssh.Client, name string) map[string]any {
	t.Helper()
	raw, stderr, err := client.Run("docker volume inspect " + name)
	if err != nil {
		t.Fatalf("docker volume inspect %s: %v: %s", name, err, stderr)
	}
	var volumes []map[string]any
	if err := json.Unmarshal([]byte(raw), &volumes); err != nil || len(volumes) != 1 {
		t.Fatalf("decode docker volume inspect %s: %v\n%s", name, err, raw)
	}
	return volumes[0]
}
