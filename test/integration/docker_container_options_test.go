//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerContainerEntrypoint tests entrypoint and command options (2.3.6)
func TestPlaybook_DockerContainerEntrypoint(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-entrypoint-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	// Test entrypoint override
	t.Log("Step 1: Create container with custom entrypoint")
	playbook := playbookHeader + `
  - name: Create container with entrypoint
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      entrypoint: ["/bin/sh", "-c"]
      command: ["echo 'custom entrypoint' && sleep 60"]
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with entrypoint failed: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED on create")
	}

	// Verify entrypoint was set
	entrypoint := remoteExec(t, client, "docker inspect --format '{{json .Config.Entrypoint}}' "+containerName)
	if !strings.Contains(entrypoint, "/bin/sh") {
		t.Errorf("Entrypoint not set correctly: %s", entrypoint)
	}

	// Test idempotency
	t.Log("Step 2: Idempotency check")
	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

// TestPlaybook_DockerContainerLogging tests log_driver and log_options (2.3.6)
func TestPlaybook_DockerContainerLogging(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-logging-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with logging options")
	playbook := playbookHeader + `
  - name: Create container with logging
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      log_driver: json-file
      log_options:
        max-size: "10m"
        max-file: "3"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with logging failed: %s", output)
	}

	// Verify log driver
	logDriver := remoteExec(t, client, "docker inspect --format '{{.HostConfig.LogConfig.Type}}' "+containerName)
	if !strings.Contains(logDriver, "json-file") {
		t.Errorf("Log driver not set correctly: %s", logDriver)
	}

	// Verify log options
	logOpts := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.LogConfig.Config}}' "+containerName)
	if !strings.Contains(logOpts, "10m") {
		t.Errorf("Log options not set correctly: %s", logOpts)
	}
}

// TestPlaybook_DockerContainerCapabilities tests cap_add and cap_drop (2.7.8)
func TestPlaybook_DockerContainerCapabilities(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-caps-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with capabilities")
	playbook := playbookHeader + `
  - name: Create container with capabilities
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      cap_add:
        - NET_ADMIN
        - SYS_TIME
      cap_drop:
        - MKNOD
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with capabilities failed: %s", output)
	}

	// Verify cap_add
	capAdd := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.CapAdd}}' "+containerName)
	if !strings.Contains(capAdd, "NET_ADMIN") || !strings.Contains(capAdd, "SYS_TIME") {
		t.Errorf("CapAdd not set correctly: %s", capAdd)
	}

	// Verify cap_drop
	capDrop := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.CapDrop}}' "+containerName)
	if !strings.Contains(capDrop, "MKNOD") {
		t.Errorf("CapDrop not set correctly: %s", capDrop)
	}

	// Test idempotency
	t.Log("Step 2: Idempotency check")
	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

// TestPlaybook_DockerContainerHealthcheck tests healthcheck options (2.7.8)
func TestPlaybook_DockerContainerHealthcheck(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-healthcheck-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with healthcheck")
	playbook := playbookHeader + `
  - name: Create container with healthcheck
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      healthcheck:
        test: ["CMD", "true"]
        interval: "30s"
        timeout: "10s"
        start_period: "5s"
        retries: 3
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with healthcheck failed: %s", output)
	}

	// Verify healthcheck
	healthcheck := remoteExec(t, client, "docker inspect --format '{{json .Config.Healthcheck}}' "+containerName)
	if !strings.Contains(healthcheck, "true") {
		t.Errorf("Healthcheck not set correctly: %s", healthcheck)
	}
	if !strings.Contains(healthcheck, "30000000000") { // 30s in nanoseconds
		t.Errorf("Healthcheck interval not set correctly: %s", healthcheck)
	}
}

// TestPlaybook_DockerContainerInit tests init option (2.7.8)
func TestPlaybook_DockerContainerInit(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-init-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with init")
	playbook := playbookHeader + `
  - name: Create container with init
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      init: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with init failed: %s", output)
	}

	// Verify init is enabled
	initEnabled := remoteExec(t, client, "docker inspect --format '{{.HostConfig.Init}}' "+containerName)
	if !strings.Contains(initEnabled, "true") {
		t.Errorf("Init not enabled: %s", initEnabled)
	}

	// Test idempotency
	t.Log("Step 2: Idempotency check")
	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

// TestPlaybook_DockerContainerTmpfs tests tmpfs mounts (2.7.8)
func TestPlaybook_DockerContainerTmpfs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-tmpfs-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with tmpfs")
	playbook := playbookHeader + `
  - name: Create container with tmpfs
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      tmpfs:
        - "/run:rw,noexec,nosuid,size=65536k"
        - "/tmp:rw,size=100m"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with tmpfs failed: %s", output)
	}

	// Verify tmpfs mounts
	tmpfs := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.Tmpfs}}' "+containerName)
	if !strings.Contains(tmpfs, "/run") || !strings.Contains(tmpfs, "/tmp") {
		t.Errorf("Tmpfs not set correctly: %s", tmpfs)
	}
}

// TestPlaybook_DockerContainerShmSize tests shm_size option (2.7.8)
func TestPlaybook_DockerContainerShmSize(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-shmsize-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with shm_size")
	playbook := playbookHeader + `
  - name: Create container with shm_size
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      shm_size: "128m"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with shm_size failed: %s", output)
	}

	// Verify shm_size (128m = 134217728 bytes)
	shmSize := remoteExec(t, client, "docker inspect --format '{{.HostConfig.ShmSize}}' "+containerName)
	if !strings.Contains(shmSize, "134217728") {
		t.Errorf("ShmSize not set correctly: %s", shmSize)
	}
}

// TestPlaybook_DockerContainerResources verifies the pinned upstream split
// between live-update resource fields and fields that require recreation.
func TestPlaybook_DockerContainerResources(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-resources-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")
	runResourceTask := func(suffix, arguments string) map[string]any {
		t.Helper()
		templatePath := writeResultTemplate(t, "resource_result")
		remotePath := "/tmp/dibra-container-resource-" + suffix + ".json"
		playbook := playbookHeader + `
  - name: Manage container resources
    docker_container:
` + arguments + `
    register: resource_result

  - name: Persist resource result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
		output := runPlaybookWithArgs(t, playbook, "--diff")
		if strings.Contains(output, "FAILED") {
			t.Fatalf("%s resource task failed: %s", suffix, output)
		}
		return readRemoteJSONMap(t, client, remotePath)
	}

	t.Log("Step 1: Create container with resource limits")
	created := runResourceTask("create", `
      name: `+containerName+`
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      cpus: 0.5
      memory: "256m"
      memory_swap: "512m"
      pids_limit: 100
`)
	if created["changed"] != true {
		t.Fatalf("create result = %#v", created)
	}

	// Verify CPU limit (0.5 CPU = 500000000 NanoCPUs)
	nanoCpus := remoteExec(t, client, "docker inspect --format '{{.HostConfig.NanoCPUs}}' "+containerName)
	if !strings.Contains(nanoCpus, "500000000") {
		t.Errorf("NanoCPUs not set correctly: %s", nanoCpus)
	}

	// Verify memory (256m = 268435456 bytes)
	memory := remoteExec(t, client, "docker inspect --format '{{.HostConfig.Memory}}' "+containerName)
	if !strings.Contains(memory, "268435456") {
		t.Errorf("Memory not set correctly: %s", memory)
	}

	// Verify pids_limit
	pidsLimit := remoteExec(t, client, "docker inspect --format '{{.HostConfig.PidsLimit}}' "+containerName)
	if !strings.Contains(pidsLimit, "100") {
		t.Errorf("PidsLimit not set correctly: %s", pidsLimit)
	}
	originalID := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+containerName)

	// memory is one of the resource fields updated through ContainerUpdate.
	t.Log("Step 2: Update a live-mutable resource")
	updated := runResourceTask("update", `
      name: `+containerName+`
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      memory: "512m"
`)
	if updated["changed"] != true {
		t.Fatalf("update result = %#v", updated)
	}
	updateDiff, ok := updated["diff"].(map[string]any)
	if !ok {
		t.Fatalf("update diff = %#v", updated["diff"])
	}
	updateBefore, beforeOK := updateDiff["before"].(map[string]any)
	updateAfter, afterOK := updateDiff["after"].(map[string]any)
	if !beforeOK || !afterOK ||
		numberValue(updateBefore["memory"]) != 268435456 ||
		numberValue(updateAfter["memory"]) != 536870912 {
		t.Fatalf("memory update diff = %#v", updateDiff)
	}

	if currentID := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+containerName); currentID != originalID {
		t.Fatalf("memory update recreated the container: before=%s after=%s", originalID, currentID)
	}
	if updatedMemory := remoteExec(t, client, "docker inspect --format '{{.HostConfig.Memory}}' "+containerName); !strings.Contains(updatedMemory, "536870912") {
		t.Errorf("Updated memory not set correctly: %s", updatedMemory)
	}

	// cpus and pids_limit are intentionally recreation fields in the pinned
	// community.docker implementation even though the Engine update API can
	// accept related values.
	t.Log("Step 3: Recreate for non-live resource fields")
	recreated := runResourceTask("recreate", `
      name: `+containerName+`
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      cpus: 1.0
      pids_limit: 200
`)
	if recreated["changed"] != true {
		t.Fatalf("recreate result = %#v", recreated)
	}
	recreateDiff, ok := recreated["diff"].(map[string]any)
	if !ok {
		t.Fatalf("recreate diff = %#v", recreated["diff"])
	}
	recreateAfter, ok := recreateDiff["after"].(map[string]any)
	if !ok || numberValue(recreateAfter["cpus"]) != 1000000000 ||
		numberValue(recreateAfter["pids_limit"]) != 200 {
		t.Fatalf("recreate diff = %#v", recreateDiff)
	}
	if currentID := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+containerName); currentID == originalID {
		t.Fatalf("cpus/pids_limit did not recreate the container: %s", currentID)
	}
	nanoCpus2 := remoteExec(t, client, "docker inspect --format '{{.HostConfig.NanoCPUs}}' "+containerName)
	if !strings.Contains(nanoCpus2, "1000000000") {
		t.Errorf("Recreated NanoCPUs not set correctly: %s", nanoCpus2)
	}
	unchanged := runResourceTask("recreate-idempotent", `
      name: `+containerName+`
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      cpus: 1.0
      pids_limit: 200
`)
	if unchanged["changed"] != false {
		t.Fatalf("repeated recreate configuration = %#v", unchanged)
	}
	if _, found := unchanged["diff"]; found {
		t.Fatalf("unchanged result returned diff = %#v", unchanged["diff"])
	}
}

// TestPlaybook_DockerContainerUlimits tests ulimits option (2.8.8)
func TestPlaybook_DockerContainerUlimits(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-ulimits-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with ulimits")
	playbook := playbookHeader + `
  - name: Create container with ulimits
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      ulimits:
        - name: nofile
          soft: 1024
          hard: 2048
        - name: nproc
          soft: 512
          hard: 1024
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with ulimits failed: %s", output)
	}

	// Verify ulimits
	ulimits := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.Ulimits}}' "+containerName)
	if !strings.Contains(ulimits, "nofile") || !strings.Contains(ulimits, "1024") {
		t.Errorf("Ulimits not set correctly: %s", ulimits)
	}
}

// TestPlaybook_DockerContainerSysctls tests sysctls option (2.8.8)
func TestPlaybook_DockerContainerSysctls(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-sysctls-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with sysctls")
	playbook := playbookHeader + `
  - name: Create container with sysctls
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      sysctls:
        net.core.somaxconn: "1024"
        net.ipv4.tcp_syncookies: "1"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with sysctls failed: %s", output)
	}

	// Verify sysctls
	sysctls := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.Sysctls}}' "+containerName)
	if !strings.Contains(sysctls, "somaxconn") || !strings.Contains(sysctls, "1024") {
		t.Errorf("Sysctls not set correctly: %s", sysctls)
	}
}

// TestPlaybook_DockerContainerSecurityOpt tests security_opt option (2.8.8)
func TestPlaybook_DockerContainerSecurityOpt(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-secopt-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container with security_opt")
	playbook := playbookHeader + `
  - name: Create container with security_opt
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      security_opt:
        - no-new-privileges:true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Create with security_opt failed: %s", output)
	}

	// Verify security_opt
	secOpt := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.SecurityOpt}}' "+containerName)
	if !strings.Contains(secOpt, "no-new-privileges") {
		t.Errorf("SecurityOpt not set correctly: %s", secOpt)
	}
}

// TestPlaybook_DockerContainerNetworks tests network reconciliation (2.5.7)
func TestPlaybook_DockerContainerNetworks(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-networks-container"
	network1 := "test-net-1"
	network2 := "test-net-2"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	remoteExec(t, client, "docker network rm "+network1+" || true")
	remoteExec(t, client, "docker network rm "+network2+" || true")
	defer func() {
		remoteExec(t, client, "docker rm -f "+containerName+" || true")
		remoteExec(t, client, "docker network rm "+network1+" || true")
		remoteExec(t, client, "docker network rm "+network2+" || true")
	}()

	// Create networks
	remoteExec(t, client, "docker network create "+network1)
	remoteExec(t, client, "docker network create "+network2)

	t.Log("Step 1: Create container connected to network1")
	playbook1 := playbookHeader + `
  - name: Create container with network
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      networks:
        - name: ` + network1 + `
          aliases:
            - myalias
`
	output1 := runPlaybook(t, playbook1)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create with network failed: %s", output1)
	}

	// Verify container is connected to network1
	networks := remoteExec(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+containerName)
	if !strings.Contains(networks, network1) {
		t.Errorf("Container not connected to %s: %s", network1, networks)
	}

	t.Log("Step 2: Add container to network2")
	playbook2 := playbookHeader + `
  - name: Add to second network
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      networks:
        - name: ` + network1 + `
        - name: ` + network2 + `
`
	output2 := runPlaybook(t, playbook2)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Add to network2 failed: %s", output2)
	}
	if !strings.Contains(output2, "CHANGED") {
		t.Error("Expected CHANGED when adding to new network")
	}

	// Verify container is connected to both networks
	networks2 := remoteExec(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+containerName)
	if !strings.Contains(networks2, network1) || !strings.Contains(networks2, network2) {
		t.Errorf("Container not connected to both networks: %s", networks2)
	}

	t.Log("Step 3: Remove from network1 (disconnect)")
	playbook3 := playbookHeader + `
  - name: Remove from network1
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      networks:
        - name: ` + network2 + `
      comparisons:
        networks: strict
`
	output3 := runPlaybook(t, playbook3)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Remove from network1 failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED when disconnecting from network")
	}

	// Verify container is only connected to network2 (and bridge)
	networks3 := remoteExec(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+containerName)
	if strings.Contains(networks3, network1) {
		t.Errorf("Container still connected to %s after disconnect: %s", network1, networks3)
	}
	if !strings.Contains(networks3, network2) {
		t.Errorf("Container not connected to %s: %s", network2, networks3)
	}

	t.Log("Step 4: Test networks_append (don't disconnect)")
	playbook4 := playbookHeader + `
  - name: Add network with append
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      networks:
        - name: ` + network1 + `
      networks_append: true
`
	output4 := runPlaybook(t, playbook4)
	if strings.Contains(output4, "FAILED") {
		t.Fatalf("Add with networks_append failed: %s", output4)
	}

	// Verify container is connected to both (network2 was not disconnected)
	networks4 := remoteExec(t, client, "docker inspect --format '{{json .NetworkSettings.Networks}}' "+containerName)
	if !strings.Contains(networks4, network1) || !strings.Contains(networks4, network2) {
		t.Errorf("networks_append did not preserve existing connections: %s", networks4)
	}
}

// TestPlaybook_DockerContainerRecreatePolicy tests recreate policy (2.4.4)
func TestPlaybook_DockerContainerRecreatePolicy(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-recreate-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 1: Create container")
	playbook1 := playbookHeader + `
  - name: Create container
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
`
	output1 := runPlaybook(t, playbook1)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Create failed: %s", output1)
	}

	// Get container ID
	containerID1 := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+containerName)

	t.Log("Step 2: Run with recreate=never (should not recreate even with changes)")
	playbook2 := playbookHeader + `
  - name: Try to change with recreate never
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "600"]
      recreate: never
`
	output2 := runPlaybook(t, playbook2)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Recreate never failed: %s", output2)
	}

	// Container ID should be the same
	containerID2 := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+containerName)
	if containerID1 != containerID2 {
		t.Errorf("Container was recreated despite recreate=never. Old: %s, New: %s", containerID1, containerID2)
	}

	t.Log("Step 3: Run with recreate=always")
	playbook3 := playbookHeader + `
  - name: Force recreate
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      recreate: always
`
	output3 := runPlaybook(t, playbook3)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Recreate always failed: %s", output3)
	}
	if !strings.Contains(output3, "CHANGED") {
		t.Error("Expected CHANGED with recreate=always")
	}

	// Container ID should be different
	containerID3 := remoteExec(t, client, "docker inspect --format '{{.Id}}' "+containerName)
	if containerID2 == containerID3 {
		t.Errorf("Container was not recreated despite recreate=always")
	}
}

// TestPlaybook_DockerContainerPullPolicy tests pull policy options (2.2)
func TestPlaybook_DockerContainerPullPolicy(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-pull-container"

	// Clean up
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	// Ensure image exists locally
	remoteExec(t, client, "docker pull alpine:latest")

	t.Log("Step 1: Test pull=never (should fail if image doesn't exist)")
	playbook1 := playbookHeader + `
  - name: Create with pull never
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "60"]
      pull: never
`
	output1 := runPlaybook(t, playbook1)
	if strings.Contains(output1, "FAILED") {
		t.Fatalf("Pull never failed (image exists): %s", output1)
	}

	// Clean up for next test
	remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 2: Test pull=missing (default)")
	playbook2 := playbookHeader + `
  - name: Create with pull missing
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "60"]
      pull: missing
`
	output2 := runPlaybook(t, playbook2)
	if strings.Contains(output2, "FAILED") {
		t.Fatalf("Pull missing failed: %s", output2)
	}

	// Clean up for next test
	remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Log("Step 3: Test pull=always")
	playbook3 := playbookHeader + `
  - name: Create with pull always
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "60"]
      pull: always
`
	output3 := runPlaybook(t, playbook3)
	if strings.Contains(output3, "FAILED") {
		t.Fatalf("Pull always failed: %s", output3)
	}

	t.Log("Step 4: Test backward compat (pull: true)")
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	playbook4 := playbookHeader + `
  - name: Create with pull true (backward compat)
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "60"]
      pull: true
`
	output4 := runPlaybook(t, playbook4)
	if strings.Contains(output4, "FAILED") {
		t.Fatalf("Pull true (backward compat) failed: %s", output4)
	}
}

// Mirrors community.docker's host/container range validation and proves that
// Docker receives individual exposed-port keys for matching ranges.
func TestPlaybook_DockerContainerPortRangeExpansion(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "test-port-range-container"
	remoteExec(t, client, "docker rm -f "+containerName+" 2>/dev/null || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" 2>/dev/null || true")

	mismatched := playbookHeader + `
  - name: Reject differently sized port ranges
    community.docker.docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "60"]
      ports:
        - "127.0.0.1:48100-48102:80-81/tcp"
`
	output := runPlaybook(t, mismatched)
	if !strings.Contains(output, "FAILED") || !strings.Contains(output, "Port ranges don't match in length") {
		t.Fatalf("mismatched port ranges were not rejected: %s", output)
	}
	if exists := remoteExec(t, client, "docker inspect "+containerName+" >/dev/null 2>&1 && echo present || echo absent"); !strings.Contains(exists, "absent") {
		t.Fatalf("mismatched port ranges created a container: %s", exists)
	}

	playbook := playbookHeader + `
  - name: Publish matching port ranges
    community.docker.docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "60"]
      published_ports:
        - "127.0.0.1:48100-48101:80-81/tcp"
`
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("matching port-range create failed: %s", output)
	}
	bindings := remoteExec(t, client, "docker inspect --format '{{json .HostConfig.PortBindings}}' "+containerName)
	for _, expected := range []string{"80/tcp", "81/tcp", "48100", "48101"} {
		if !strings.Contains(bindings, expected) {
			t.Errorf("Port bindings %s do not contain %q", bindings, expected)
		}
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Port-range idempotency run failed: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Fatalf("Port-range idempotency run changed: %s", output)
	}
}
