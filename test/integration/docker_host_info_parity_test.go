//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerHostInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "rm -f /tmp/dibra-host-info-*.json /tmp/.dibra-agent")
	templatePath := writeResultTemplate(t, "host_info_result")
	runInfo := func(testName, arguments string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-host-info-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Inspect docker host
    community.docker.docker_host_info:
` + arguments + `
    register: host_info_result

  - name: Persist host info result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s host info playbook failed: %s", testName, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("raw host_info and can_talk_to_docker", func(t *testing.T) {
		result := runInfo("basic", "      containers: false\n")
		if result["changed"] != false || result["can_talk_to_docker"] != true {
			t.Fatalf("result = %#v", result)
		}
		hostInfo, _ := result["host_info"].(map[string]any)
		if hostInfo["Name"] == nil || hostInfo["ServerVersion"] == nil {
			t.Fatalf("host_info = %#v", hostInfo)
		}
		if _, found := result["containers"]; found {
			t.Fatalf("unrequested containers present: %#v", result)
		}
	})

	t.Run("lists are projected unless verbose", func(t *testing.T) {
		mustRemote(t, client, "docker rm -f dibra-host-info-probe >/dev/null 2>&1 || true")
		mustRemote(t, client, "docker pull alpine:latest")
		mustRemote(t, client, "docker run -d --name dibra-host-info-probe alpine:latest sleep 60")
		defer mustRemote(t, client, "docker rm -f dibra-host-info-probe >/dev/null 2>&1 || true")

		result := runInfo("lists", "      containers: true\n      containers_all: true\n      images: true\n      networks: true\n      volumes: true\n")
		containers, _ := result["containers"].([]any)
		if len(containers) == 0 {
			t.Fatalf("expected containers: %#v", result)
		}
		first, _ := containers[0].(map[string]any)
		for _, key := range []string{"Id", "Image", "Command", "Created", "Status", "Ports", "Names"} {
			if _, found := first[key]; !found {
				t.Fatalf("missing container key %s in %#v", key, first)
			}
		}
		if _, found := first["HostConfig"]; found {
			t.Fatalf("non-verbose container leaked HostConfig: %#v", first)
		}
		images, _ := result["images"].([]any)
		if len(images) == 0 {
			t.Fatalf("expected images: %#v", result)
		}
		image, _ := images[0].(map[string]any)
		if _, found := image["ParentId"]; found {
			t.Fatalf("non-verbose image leaked ParentId: %#v", image)
		}

		verbose := runInfo("verbose-images", "      images: true\n      verbose_output: true\n")
		verboseImages, _ := verbose["images"].([]any)
		verboseImage, _ := verboseImages[0].(map[string]any)
		if _, found := verboseImage["ParentId"]; !found {
			t.Fatalf("verbose image missing ParentId: %#v", verboseImage)
		}
	})

	t.Run("disk usage non-verbose is LayersSize only", func(t *testing.T) {
		result := runInfo("df", "      disk_usage: true\n")
		usage, _ := result["disk_usage"].(map[string]any)
		if _, found := usage["LayersSize"]; !found {
			t.Fatalf("disk_usage = %#v", usage)
		}
		if _, found := usage["Images"]; found {
			t.Fatalf("non-verbose disk usage leaked Images: %#v", usage)
		}
		verbose := runInfo("df-verbose", "      disk_usage: true\n      verbose_output: true\n")
		verboseUsage, _ := verbose["disk_usage"].(map[string]any)
		for _, key := range []string{"LayersSize", "Images", "Containers", "Volumes", "BuildCache"} {
			if _, found := verboseUsage[key]; !found {
				t.Fatalf("verbose disk usage missing %s: %#v", key, verboseUsage)
			}
		}
	})

	t.Run("bad docker host cannot talk to docker", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Inspect unreachable daemon
    community.docker.docker_host_info:
      docker_host: unix:///tmp/dibra-missing-docker.sock
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "FAILED") {
			t.Fatalf("expected failure for missing docker host: %s", output)
		}
	})
}
