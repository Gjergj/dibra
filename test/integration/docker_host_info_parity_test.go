//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerHostInfoParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker rm -f dibra-host-info-probe dibra-host-info-running dibra-host-info-stopped >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker network rm dibra-host-info-network >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker volume rm -f dibra-host-info-volume >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-host-info-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker rm -f dibra-host-info-probe dibra-host-info-running dibra-host-info-stopped >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker network rm dibra-host-info-network >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker volume rm -f dibra-host-info-volume >/dev/null 2>&1 || true")
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
		mustRemote(t, client, "docker network create dibra-host-info-network")
		mustRemote(t, client, "docker volume create dibra-host-info-volume")
		defer mustRemote(t, client, "docker rm -f dibra-host-info-probe >/dev/null 2>&1 || true")
		defer mustRemote(t, client, "docker network rm dibra-host-info-network >/dev/null 2>&1 || true")
		defer mustRemote(t, client, "docker volume rm -f dibra-host-info-volume >/dev/null 2>&1 || true")

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

		verbose := runInfo("verbose-lists", "      containers: true\n      containers_all: true\n      images: true\n      networks: true\n      volumes: true\n      verbose_output: true\n")
		verboseImages, _ := verbose["images"].([]any)
		verboseImage, _ := verboseImages[0].(map[string]any)
		if _, found := verboseImage["ParentId"]; !found {
			t.Fatalf("verbose image missing ParentId: %#v", verboseImage)
		}
		verboseContainers, _ := verbose["containers"].([]any)
		containerHasImageID := false
		for _, item := range verboseContainers {
			record, _ := item.(map[string]any)
			if names, _ := record["Names"].([]any); len(names) > 0 && names[0] == "/dibra-host-info-probe" {
				_, containerHasImageID = record["ImageID"]
			}
		}
		if !containerHasImageID {
			t.Fatalf("verbose container missing ImageID: %#v", verboseContainers)
		}
		verboseNetworks, _ := verbose["networks"].([]any)
		networkHasCreated := false
		for _, item := range verboseNetworks {
			record, _ := item.(map[string]any)
			if record["Name"] == "dibra-host-info-network" {
				_, networkHasCreated = record["Created"]
			}
		}
		if !networkHasCreated {
			t.Fatalf("verbose network missing Created: %#v", verboseNetworks)
		}
		verboseVolumes, _ := verbose["volumes"].([]any)
		volumeHasMountpoint := false
		for _, item := range verboseVolumes {
			record, _ := item.(map[string]any)
			if record["Name"] == "dibra-host-info-volume" {
				_, volumeHasMountpoint = record["Mountpoint"]
			}
		}
		if !volumeHasMountpoint {
			t.Fatalf("verbose volume missing Mountpoint: %#v", verboseVolumes)
		}
	})

	t.Run("container filters and containers_all", func(t *testing.T) {
		mustRemote(t, client, "docker run -d --name dibra-host-info-running --label dibra.host_info=target alpine:latest sleep 60")
		mustRemote(t, client, "docker run --name dibra-host-info-stopped --label dibra.host_info=target alpine:latest true")
		defer mustRemote(t, client, "docker rm -f dibra-host-info-running dibra-host-info-stopped >/dev/null 2>&1 || true")

		running := runInfo("filtered-running", `      containers: true
      containers_all: false
      containers_filters:
        label: dibra.host_info=target
`)
		runningContainers, _ := running["containers"].([]any)
		if len(runningContainers) != 1 {
			t.Fatalf("running filtered containers = %#v", runningContainers)
		}
		all := runInfo("filtered-all", `      containers: true
      containers_all: true
      containers_filters:
        label:
          - dibra.host_info=target
`)
		allContainers, _ := all["containers"].([]any)
		if len(allContainers) != 2 {
			t.Fatalf("all filtered containers = %#v", allContainers)
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
		result := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_host_info",
			map[string]any{"docker_host": "unix:///tmp/dibra-missing-docker.sock"}, "")
		if result["failed"] != true || result["can_talk_to_docker"] != false ||
			!strings.Contains(resultString(result, "msg"), "Error inspecting docker host:") {
			t.Fatalf("bad host result = %#v", result)
		}
	})

	t.Run("invalid API version cannot talk to docker", func(t *testing.T) {
		result := runDockerAgentRequestWithEnvironment(t, client,
			"community.docker.docker_host_info",
			map[string]any{"api_version": "1.999.999"}, "")
		if result["failed"] != true || result["can_talk_to_docker"] != false ||
			!strings.Contains(resultString(result, "msg"), "An unexpected Docker error occurred: invalid API version") {
			t.Fatalf("invalid API result = %#v", result)
		}
	})
}
