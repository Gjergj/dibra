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
	mustRemote(t, client, "docker rm -f dibra-prune-exited dibra-prune-filtered dibra-prune-kept >/dev/null 2>&1 || true")
	mustRemote(t, client, "docker network rm dibra-prune-network dibra-prune-check-network >/dev/null 2>&1 || true")
	mustRemote(t, client, "rm -f /tmp/dibra-prune-*.json /tmp/.dibra-agent")
	defer mustRemote(t, client, "docker volume rm -f dibra-prune-named >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker rm -f dibra-prune-exited dibra-prune-filtered dibra-prune-kept >/dev/null 2>&1 || true")
	defer mustRemote(t, client, "docker network rm dibra-prune-network dibra-prune-check-network >/dev/null 2>&1 || true")

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

	t.Run("check mode skips before prune API calls", func(t *testing.T) {
		mustRemote(t, client, "docker network create dibra-prune-check-network")
		output := runPlaybookWithArgs(t, playbookHeader+`
  - name: Do not prune resources in check mode
    community.docker.docker_prune:
      networks: true
`, "--check")
		if strings.Contains(output, "FAILED") || !strings.Contains(output, "SKIPPED") {
			t.Fatalf("check-mode prune output = %s", output)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker network inspect dibra-prune-check-network >/dev/null 2>&1; echo $?")) != "0" {
			t.Fatal("check-mode prune removed a network")
		}
		mustRemote(t, client, "docker network rm dibra-prune-check-network")
	})

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

	t.Run("container filters remove only matching resources", func(t *testing.T) {
		mustRemote(t, client, "docker run --name dibra-prune-filtered --label dibra.prune=target alpine true")
		mustRemote(t, client, "docker run --name dibra-prune-kept --label dibra.prune=keep alpine true")
		result := runPrune("container-filter", "      containers: true\n      containers_filters:\n        label: dibra.prune=target\n")
		if result["changed"] != true || result["containers"] == nil {
			t.Fatalf("filtered container prune = %#v", result)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker inspect dibra-prune-filtered >/dev/null 2>&1; echo $?")) == "0" {
			t.Fatal("matching container was not pruned")
		}
		if strings.TrimSpace(remoteExec(t, client, "docker inspect dibra-prune-kept >/dev/null 2>&1; echo $?")) != "0" {
			t.Fatal("nonmatching container was pruned")
		}
		mustRemote(t, client, "docker rm -f dibra-prune-kept")
	})

	t.Run("network prune and second run idempotency", func(t *testing.T) {
		mustRemote(t, client, "docker network create dibra-prune-network")
		first := runPrune("network-first", "      networks: true\n")
		if first["changed"] != true || first["networks"] == nil {
			t.Fatalf("network prune = %#v", first)
		}
		if strings.TrimSpace(remoteExec(t, client, "docker network inspect dibra-prune-network >/dev/null 2>&1; echo $?")) == "0" {
			t.Fatal("unused network was not pruned")
		}
		second := runPrune("network-second", "      networks: true\n")
		if second["changed"] != false {
			t.Fatalf("second network prune = %#v", second)
		}
	})

	t.Run("image filters return the requested image group", func(t *testing.T) {
		first := runPrune("images-first", "      images: true\n      images_filters:\n        dangling: true\n")
		images, found := first["images"]
		if !found || images == nil || first["images_space_reclaimed"] == nil {
			t.Fatalf("image group missing: %#v", first)
		}
		second := runPrune("images-second", "      images: true\n      images_filters:\n        dangling: true\n")
		if second["changed"] != false {
			t.Fatalf("second image prune = %#v", second)
		}
	})

	t.Run("builder cache options and alias are accepted", func(t *testing.T) {
		result := runPrune("builder", `      builder_cache: true
      builder_cache_all: true
      builder_cache_filters:
        until: 0s
      builder_cache_keep_storage: 1GB
`)
		if result["builder_cache_space_reclaimed"] == nil || result["builder_cache_caches_deleted"] == nil {
			t.Fatalf("builder cache group missing: %#v", result)
		}
	})
}
