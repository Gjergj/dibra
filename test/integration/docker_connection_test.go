//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// Upstream behavioral references:
// - plugins/module_utils/_util.py (DOCKER_COMMON_ARGS)
// - plugins/module_utils/_common_api.py (auth_params precedence)
// at community.docker 5.2.2, commit 44812d46a5072eec78175a41a1100ee77218c8a2.
func TestDockerConnectionEnvironmentFallbackAndArgumentPrecedence(t *testing.T) {
	// Upload a fresh development agent before invoking it directly with a
	// controlled remote environment.
	bootstrap := playbookHeader + `
  - name: Bootstrap Docker agent
    docker_host_info:
      containers: false
      docker_host: unix:///var/run/docker.sock
`
	if output := runPlaybook(t, bootstrap); strings.Contains(output, "FAILED") {
		t.Fatalf("failed to bootstrap Docker agent: %s", output)
	}

	sshClient := getClient(t)
	defer sshClient.Close()

	tests := []struct {
		name        string
		environment string
		arguments   string
	}{
		{
			name:        "environment fallback",
			environment: "DOCKER_HOST=unix:///var/run/docker.sock",
			arguments:   `{"containers":false}`,
		},
		{
			name:        "explicit arguments override hostile environment",
			environment: "DOCKER_HOST=tcp://127.0.0.1:1 DOCKER_TLS=true DOCKER_TLS_VERIFY=true",
			arguments:   `{"containers":false,"docker_host":"unix:///var/run/docker.sock","tls":false,"validate_certs":false}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := `{"module":"community.docker.docker_host_info","args":` + test.arguments + `,"check_mode":false,"diff":false}`
			command := "printf '%s' '" + request + "' | env " + test.environment + " /tmp/.dibra-agent"
			stdout, stderr, err := sshClient.Run(command)
			if err != nil {
				t.Fatalf("agent invocation failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			var response struct {
				Failed bool   `json:"failed"`
				Msg    string `json:"msg"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &response); err != nil {
				t.Fatalf("decode agent response %q: %v", stdout, err)
			}
			if response.Failed {
				t.Fatalf("Docker request failed: %s", response.Msg)
			}
		})
	}
}
