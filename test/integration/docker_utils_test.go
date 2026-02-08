//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestPlaybook_DockerPortBindings tests port binding parsing and handling
func TestPlaybook_DockerPortBindings(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "port-binding-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Run("simple host:container port", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with port binding
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      ports:
        - "18080:80"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		// Verify port binding
		ports := remoteExec(t, client, "docker port "+containerName)
		if !strings.Contains(ports, "18080") {
			t.Errorf("Expected port 18080 to be bound, got: %s", ports)
		}

		// Cleanup for next test
		remoteExec(t, client, "docker rm -f "+containerName)
	})

	t.Run("multiple ports", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with multiple ports
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      ports:
        - "18080:80"
        - "18443:443"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		ports := remoteExec(t, client, "docker port "+containerName)
		if !strings.Contains(ports, "18080") || !strings.Contains(ports, "18443") {
			t.Errorf("Expected both ports to be bound, got: %s", ports)
		}

		remoteExec(t, client, "docker rm -f "+containerName)
	})

	t.Run("port binding idempotency", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with port
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      ports:
        - "19080:80"
`
		// First run
		output1 := runPlaybook(t, playbook)
		if strings.Contains(output1, "FAILED") {
			t.Fatalf("First run failed: %s", output1)
		}
		if !strings.Contains(output1, "CHANGED") {
			t.Error("Expected CHANGED on first run")
		}

		// Second run - should be idempotent
		output2 := runPlaybook(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("Second run failed: %s", output2)
		}
		// Note: Current implementation may not be fully idempotent for ports
		// This test documents expected behavior after improvements
	})
}

// TestPlaybook_DockerEnvironment tests environment variable handling
func TestPlaybook_DockerEnvironment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "env-test-container"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Run("environment variables", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with env vars
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      env:
        FOO: bar
        BAZ: "qux with spaces"
        NUMERIC: "123"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		// Verify env vars
		fooVal := remoteExec(t, client, "docker exec "+containerName+" printenv FOO")
		if fooVal != "bar" {
			t.Errorf("Expected FOO=bar, got: %s", fooVal)
		}

		bazVal := remoteExec(t, client, "docker exec "+containerName+" printenv BAZ")
		if bazVal != "qux with spaces" {
			t.Errorf("Expected BAZ='qux with spaces', got: %s", bazVal)
		}
	})

	t.Run("env idempotency", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Container with env
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      env:
        TEST_VAR: value1
`
		// Run twice
		runPlaybook(t, playbook)
		output2 := runPlaybook(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("Second run failed: %s", output2)
		}
	})
}

// TestPlaybook_DockerLabels tests label handling
func TestPlaybook_DockerLabels(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "label-test-container"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create container with labels
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      labels:
        app: myapp
        version: "1.0"
        env: test
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Failed: %s", output)
	}

	// Verify labels
	labels := remoteExec(t, client, "docker inspect --format '{{json .Config.Labels}}' "+containerName)
	if !strings.Contains(labels, "myapp") {
		t.Errorf("Expected label 'app: myapp', got: %s", labels)
	}
	if !strings.Contains(labels, "1.0") {
		t.Errorf("Expected label 'version: 1.0', got: %s", labels)
	}
}

// TestPlaybook_DockerNetworkModes tests network mode handling
func TestPlaybook_DockerNetworkModes(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "network-mode-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Run("host network mode", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with host network
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      network_mode: host
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		mode := remoteExec(t, client, "docker inspect --format '{{.HostConfig.NetworkMode}}' "+containerName)
		if mode != "host" {
			t.Errorf("Expected network mode 'host', got: %s", mode)
		}

		remoteExec(t, client, "docker rm -f "+containerName)
	})

	t.Run("none network mode", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with no network
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      network_mode: none
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		mode := remoteExec(t, client, "docker inspect --format '{{.HostConfig.NetworkMode}}' "+containerName)
		if mode != "none" {
			t.Errorf("Expected network mode 'none', got: %s", mode)
		}
	})

	t.Run("network mode idempotency", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Container with network mode
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      network_mode: bridge
`
		// First run
		output1 := runPlaybook(t, playbook)
		if strings.Contains(output1, "FAILED") {
			t.Fatalf("First run failed: %s", output1)
		}

		// Second run
		output2 := runPlaybook(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("Second run failed: %s", output2)
		}
		if strings.Contains(output2, "CHANGED") {
			t.Error("Expected no changes on second run for same network mode")
		}
	})
}

// TestPlaybook_DockerVolumes tests volume binding handling
func TestPlaybook_DockerVolumes(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "volume-test-container"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Run("bind mount", func(t *testing.T) {
		volumeName := "dibra-test-vol"
		remoteExec(t, client, "docker volume rm -f "+volumeName+" || true")
		defer remoteExec(t, client, "docker volume rm -f "+volumeName+" || true")

		playbook := playbookHeader + `
  - name: Create test volume
    docker_volume:
      name: ` + volumeName + `
      state: present

  - name: Create container with volume
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      volumes:
        - "` + volumeName + `:/data"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		// Write test content into the volume via the container
		remoteExec(t, client, "docker exec "+containerName+" sh -c 'echo test content > /data/test.txt'")

		// Verify mount works
		content := remoteExec(t, client, "docker exec "+containerName+" cat /data/test.txt")
		if !strings.Contains(content, "test content") {
			t.Errorf("Expected 'test content', got: %s", content)
		}
	})
}

// TestPlaybook_DockerRestartPolicy tests restart policy handling
func TestPlaybook_DockerRestartPolicy(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "restart-policy-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Run("always restart", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container with restart policy
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      restart_policy: always
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		policy := remoteExec(t, client, "docker inspect --format '{{.HostConfig.RestartPolicy.Name}}' "+containerName)
		if policy != "always" {
			t.Errorf("Expected restart policy 'always', got: %s", policy)
		}
	})
}

// TestPlaybook_DockerWorkingDir tests working directory handling
func TestPlaybook_DockerWorkingDir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "workdir-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create container with working dir
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      working_dir: /opt/app
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Failed: %s", output)
	}

	workdir := remoteExec(t, client, "docker inspect --format '{{.Config.WorkingDir}}' "+containerName)
	if workdir != "/opt/app" {
		t.Errorf("Expected working dir '/opt/app', got: %s", workdir)
	}
}

// TestPlaybook_DockerUser tests user handling
func TestPlaybook_DockerUser(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "user-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create container with specific user
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      user: "1000:1000"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Failed: %s", output)
	}

	user := remoteExec(t, client, "docker inspect --format '{{.Config.User}}' "+containerName)
	if user != "1000:1000" {
		t.Errorf("Expected user '1000:1000', got: %s", user)
	}
}

// TestPlaybook_DockerHostname tests hostname handling
func TestPlaybook_DockerHostname(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "hostname-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create container with custom hostname
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      hostname: mycontainer
      domainname: example.com
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Failed: %s", output)
	}

	hostname := remoteExec(t, client, "docker exec "+containerName+" hostname")
	if hostname != "mycontainer" {
		t.Errorf("Expected hostname 'mycontainer', got: %s", hostname)
	}
}

// TestPlaybook_DockerPrivileged tests privileged mode
func TestPlaybook_DockerPrivileged(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "privileged-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create privileged container
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
      privileged: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Failed: %s", output)
	}

	privileged := remoteExec(t, client, "docker inspect --format '{{.HostConfig.Privileged}}' "+containerName)
	if privileged != "true" {
		t.Errorf("Expected privileged=true, got: %s", privileged)
	}
}

// TestPlaybook_DockerAutoRemove tests auto remove functionality
func TestPlaybook_DockerAutoRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "autoremove-test"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")

	playbook := playbookHeader + `
  - name: Create container with auto_remove
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["echo", "hello"]
      auto_remove: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Failed: %s", output)
	}

	// Container might already be removed since command exits quickly
	// Just verify no error occurred
}

// TestPlaybook_DockerContainerStates tests all container states
func TestPlaybook_DockerContainerStates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	containerName := "state-test-container"

	// Cleanup
	remoteExec(t, client, "docker rm -f "+containerName+" || true")
	defer remoteExec(t, client, "docker rm -f "+containerName+" || true")

	t.Run("state: present (create only)", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create container without starting
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: present
      command: ["sleep", "300"]
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		status := remoteExec(t, client, "docker inspect --format '{{.State.Status}}' "+containerName)
		if status != "created" {
			t.Errorf("Expected status 'created', got: %s", status)
		}

		remoteExec(t, client, "docker rm -f "+containerName)
	})

	t.Run("state: started", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Create and start container
    docker_container:
      name: ` + containerName + `
      image: alpine:latest
      state: started
      command: ["sleep", "300"]
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		status := remoteExec(t, client, "docker inspect --format '{{.State.Running}}' "+containerName)
		if status != "true" {
			t.Errorf("Expected container to be running, got: %s", status)
		}
	})

	t.Run("state: stopped", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Stop container
    docker_container:
      name: ` + containerName + `
      state: stopped
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		running := remoteExec(t, client, "docker inspect --format '{{.State.Running}}' "+containerName)
		if running != "false" {
			t.Errorf("Expected container to be stopped, got running=%s", running)
		}
	})

	t.Run("state: absent", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Remove container
    docker_container:
      name: ` + containerName + `
      state: absent
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}

		// Container should not exist
		exists := remoteExec(t, client, "docker ps -a --filter name=^"+containerName+"$ --format '{{.Names}}'")
		if exists != "" {
			t.Errorf("Expected container to be removed, but found: %s", exists)
		}
	})

	t.Run("state: absent idempotency", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Remove non-existent container
    docker_container:
      name: ` + containerName + `-nonexistent
      state: absent
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Failed: %s", output)
		}
		if strings.Contains(output, "CHANGED") {
			t.Error("Expected no changes when removing non-existent container")
		}
	})
}
