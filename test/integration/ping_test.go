//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type PingResponse struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Ping    string `json:"ping,omitempty"`
}

func getPingResult(t *testing.T, client interface{ Run(string) (string, string, error) }, args string) PingResponse {
	t.Helper()
	cmd := `echo '{"module":"ping","args":` + args + `}' | /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Agent execution failed: %v, stderr: %s", err, stderr)
	}

	var resp PingResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func TestPlaybook_PingBasic(t *testing.T) {
	playbook := playbookHeader + `
  - name: Ping test
    ping:
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes (ping should never report changed)")
	}
	if !strings.Contains(output, "OK") {
		t.Errorf("Expected OK status, got: %s", output)
	}
}

func TestPlaybook_PingReturnsPong(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getPingResult(t, client, `{}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Ping != "pong" {
		t.Errorf("Expected ping=pong, got: %s", resp.Ping)
	}
	if resp.Changed {
		t.Error("ping should never report changed=true")
	}
}

func TestPlaybook_PingCustomData(t *testing.T) {
	playbook := playbookHeader + `
  - name: Ping with custom data
    ping:
      data: hello
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
}

func TestPlaybook_PingCustomDataReturned(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getPingResult(t, client, `{"data":"hello"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Ping != "hello" {
		t.Errorf("Expected ping=hello, got: %s", resp.Ping)
	}
}

func TestPlaybook_PingCrash(t *testing.T) {
	playbook := playbookHeader + `
  - name: Ping with crash
    ping:
      data: crash
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Fatalf("Expected failure when data=crash, got: %s", output)
	}
}

func TestPlaybook_PingCrashResponse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getPingResult(t, client, `{"data":"crash"}`)

	if !resp.Failed {
		t.Error("Expected failed=true when data=crash")
	}
	if resp.Msg != "boom" {
		t.Errorf("Expected msg=boom, got: %s", resp.Msg)
	}
}

func TestPlaybook_PingIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp1 := getPingResult(t, client, `{}`)
	resp2 := getPingResult(t, client, `{}`)

	if resp1.Changed || resp2.Changed {
		t.Error("ping should never report changed=true")
	}
	if resp1.Ping != resp2.Ping {
		t.Errorf("ping results differ: %s vs %s", resp1.Ping, resp2.Ping)
	}
}

func TestPlaybook_PingNeverChanges(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	for i := 0; i < 3; i++ {
		resp := getPingResult(t, client, `{}`)
		if resp.Changed {
			t.Errorf("Run %d: ping reported changed=true", i+1)
		}
	}
}

func TestPlaybook_PingEmptyData(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getPingResult(t, client, `{"data":""}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Ping != "pong" {
		t.Errorf("Expected ping=pong for empty data, got: %s", resp.Ping)
	}
}

func TestPlaybook_PingSpecialChars(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	testData := "test-123_special"
	resp := getPingResult(t, client, `{"data":"`+testData+`"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Ping != testData {
		t.Errorf("Expected ping=%s, got: %s", testData, resp.Ping)
	}
}

func TestPlaybook_PingSSHConnectivity(t *testing.T) {
	playbook := playbookHeader + `
  - name: Test SSH connectivity
    ping:
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("SSH connectivity test failed: %s", output)
	}
}

func TestPlaybook_PingMultipleRuns(t *testing.T) {
	for i := 0; i < 3; i++ {
		playbook := playbookHeader + `
  - name: Ping run ` + string(rune('1'+i)) + `
    ping:
`
		output := runPlaybook(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Run %d failed: %s", i+1, output)
		}
		if strings.Contains(output, "CHANGED") {
			t.Errorf("Run %d reported changed", i+1)
		}
	}
}
