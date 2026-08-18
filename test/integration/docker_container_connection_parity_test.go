//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerConnectionParity applies the pinned generic
// Docker API connection contract to docker_container itself.
func TestPlaybook_DockerContainerConnectionParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	startTLSFixture(t, client)
	defer stopTLSFixture(t, client)

	const (
		explicitName = "dibra-container-connection-explicit"
		envName      = "dibra-container-connection-environment"
		overrideName = "dibra-container-connection-override"
	)
	cleanup := func() {
		remoteExec(t, client, "docker rm -f "+explicitName+" "+envName+" "+overrideName+" || true")
	}
	cleanup()
	defer cleanup()

	explicitArgs := `
      name: ` + explicitName + `
      image: alpine:latest
      state: present
      docker_host: ` + tlsHTTPSHost + `
      api_version: auto
      timeout: 30
      tls: true
      validate_certs: true
      ca_path: ` + tlsCAPath + `
      client_cert: ` + tlsClientCertPath + `
      client_key: ` + tlsClientKeyPath + `
      tls_hostname: ` + tlsServerName + `
      debug: true
      use_ssh_client: true
`
	assertChanged(t, runContainerStateTask(t, client, "connection-explicit", explicitArgs, "--diff"), true)
	assertChanged(t, runContainerStateTask(t, client, "connection-explicit-idempotent", explicitArgs, "--diff"), false)

	t.Run("environment fallback", func(t *testing.T) {
		args := map[string]any{
			"name": envName, "image": "alpine:latest", "state": "present",
		}
		environment := strings.Join([]string{
			"DOCKER_HOST=" + tlsHTTPSHost,
			"DOCKER_API_VERSION=auto",
			"DOCKER_TIMEOUT=30",
			"DOCKER_TLS_VERIFY=true",
			"DOCKER_CERT_PATH=" + tlsFixtureDir + "/certs",
			"DOCKER_TLS_HOSTNAME=" + tlsServerName,
		}, " ")
		first := runContainerAgentRequestWithEnvironment(t, client, args, environment)
		if first["failed"] == true || first["changed"] != true {
			t.Fatalf("environment fallback create = %#v", first)
		}
		second := runContainerAgentRequestWithEnvironment(t, client, args, environment)
		if second["failed"] == true || second["changed"] != false {
			t.Fatalf("environment fallback idempotency = %#v", second)
		}
	})

	t.Run("explicit arguments override hostile environment", func(t *testing.T) {
		args := map[string]any{
			"name": overrideName, "image": "alpine:latest", "state": "present",
			"docker_host": "unix:///var/run/docker.sock",
			"tls":         false, "validate_certs": false, "timeout": 0,
		}
		environment := "DOCKER_HOST=tcp://127.0.0.1:1 DOCKER_TIMEOUT=1 DOCKER_TLS=true DOCKER_TLS_VERIFY=true"
		result := runContainerAgentRequestWithEnvironment(t, client, args, environment)
		if result["failed"] == true || result["changed"] != true {
			t.Fatalf("explicit connection override = %#v", result)
		}
	})
}

func runContainerAgentRequestWithEnvironment(
	t *testing.T,
	client *ssh.Client,
	args map[string]any,
	environment string,
) map[string]any {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"module": "community.docker.docker_container", "args": args,
		"check_mode": false, "diff": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := "printf '%s' '" + string(request) + "' | env " + environment + " /tmp/.dibra-agent"
	stdout, stderr, err := client.Run(command)
	if err != nil {
		t.Fatalf("container connection request failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		t.Fatalf("decode container connection response: %v\nstdout: %s", err, stdout)
	}
	return result
}
