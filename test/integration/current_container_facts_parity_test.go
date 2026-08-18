//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_CurrentContainerFactsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "rm -f /tmp/dibra-facts-*.json /tmp/dibra-facts-injected.txt /tmp/.dibra-agent")
	defer mustRemote(t, client, "rm -f /tmp/dibra-facts-injected.txt")
	templatePath := writeResultTemplate(t, "facts_result")
	playbook := playbookHeader + `
  - name: Detect current container
    community.docker.current_container_facts:
    register: facts_result

  - name: Persist facts result
    template:
      src: ` + templatePath + `
      dest: /tmp/dibra-facts-result.json
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("facts playbook failed: %s", output)
	}
	result := readRemoteJSONMap(t, client, "/tmp/dibra-facts-result.json")
	if result["changed"] != false {
		t.Fatalf("result = %#v", result)
	}
	facts, _ := result["ansible_facts"].(map[string]any)
	if facts["ansible_module_running_in_container"] != true {
		t.Fatalf("expected to detect the integration container: %#v", facts)
	}
	containerType, _ := facts["ansible_module_container_type"].(string)
	if containerType != "docker" && containerType != "podman" {
		t.Fatalf("container type = %#v", facts)
	}
	containerID, _ := facts["ansible_module_container_id"].(string)
	if len(containerID) < 12 {
		t.Fatalf("container id = %#v", facts)
	}

	checkPlaybook := playbookHeader + `
  - name: Detect current container in check mode
    community.docker.current_container_facts:
    check_mode: true
    register: facts_result

  - name: Persist facts result
    template:
      src: ` + templatePath + `
      dest: /tmp/dibra-facts-check.json
`
	checkOutput := runPlaybook(t, checkPlaybook)
	if strings.Contains(checkOutput, "FAILED") {
		t.Fatalf("check-mode facts failed: %s", checkOutput)
	}
	checkResult := readRemoteJSONMap(t, client, "/tmp/dibra-facts-check.json")
	if checkResult["changed"] != false {
		t.Fatalf("check result = %#v", checkResult)
	}

	t.Run("facts are injected without register", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Detect current container without register
    community.docker.current_container_facts:

  - name: Consume injected container facts
    copy:
      content: "{{ ansible_module_running_in_container }}|{{ ansible_module_container_type }}|{{ ansible_facts.ansible_module_container_id }}"
      dest: /tmp/dibra-facts-injected.txt
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("injected facts playbook failed: %s", output)
		}
		fields := strings.Split(remoteFileContent(t, client, "/tmp/dibra-facts-injected.txt"), "|")
		if len(fields) != 3 || fields[0] != "true" ||
			(fields[1] != "docker" && fields[1] != "podman") ||
			len(fields[2]) < 12 {
			t.Fatalf("injected facts = %#v", fields)
		}
	})
}
