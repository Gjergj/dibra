//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_DockerPruneParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	mustRemote(t, client, "docker volume rm -f dibra-prune-named >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker rm -f dibra-prune-exited >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-prune-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker volume rm -f dibra-prune-named >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker rm -f dibra-prune-exited >/dev/null 2>&1 || true")

	templatePath := writeResultTemplate(t, "prune_result")
	runPrune := func(testName, arguments string) map[string]any {
		t.Helper()
		remotePath := "/tmp/dibra-prune-" + testName + ".json"
		playbook := playbookHeader + `
  - name: Prune resources
    community.docker.docker_prune:
` + arguments + `
    register: prune_result

  - name: Persist prune result
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s prune playbook failed: %s", testName, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Run("named volume is kept without all filter on Engine 29", func(t *testing.T) {
		mustRemote(t, client, "docker volume create dibra-prune-named")
		result := runPrune("named-kept", "      volumes: true\n")
		if _, found := result["volumes"]; !found || result["volumes_space_reclaimed"] == nil {
			t.Fatalf("volume group missing: %#v", result)
		}
		if _, found := result["containers"]; found {
			t.Fatalf("unrequested containers group present: %#v", result)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker volume inspect dibra-prune-named >/dev/null && echo yes")) != "yes" {
			t.Fatal("named volume was pruned without all=true")
		}
	})

	t.Run("all filter removes unused named volume", func(t *testing.T) {
		result := runPrune("named-all", "      volumes: true\n      volumes_filters:\n        all: true\n")
		volumes, _ := result["volumes"].([]any)
		if !result["changed"].(bool) && len(volumes) == 0 {
			t.Logf("no volumes reclaimed: %#v", result)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker volume inspect dibra-prune-named >/dev/null 2>&1; echo $?")) == "0" {
			t.Fatal("named volume still exists after prune with all=true")
		}
	})

	t.Run("container prune returns requested fields", func(t *testing.T) {
		mustRemote(t, client, "docker run --name dibra-prune-exited alpine true")
		result := runPrune("containers", "      containers: true\n")
		if result["containers"] == nil || result["containers_space_reclaimed"] == nil {
			t.Fatalf("container group missing: %#v", result)
		}
	})

	t.Run("builder_cache alias is accepted", func(t *testing.T) {
		result := runPrune("builder", "      builder_cache: true\n")
		if result["builder_cache_space_reclaimed"] == nil || result["builder_cache_caches_deleted"] == nil {
			t.Fatalf("builder cache group missing: %#v", result)
		}
	})
}
