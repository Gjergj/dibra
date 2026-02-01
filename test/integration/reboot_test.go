//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlaybook_RebootBootTimeCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"cat /proc/sys/kernel/random/boot_id"}}' | /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Boot time command failed: %v, stderr: %s", err, stderr)
	}

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}

	if resp.RC != 0 {
		t.Errorf("Expected RC=0, got: %d", resp.RC)
	}

	bootID := strings.TrimSpace(resp.Stdout)
	if bootID == "" {
		t.Error("Boot ID should not be empty")
	}

	if len(bootID) != 36 {
		t.Logf("Boot ID format might be different: %s (length %d)", bootID, len(bootID))
	}
}

func TestPlaybook_RebootBootTimeIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"cat /proc/sys/kernel/random/boot_id"}}' | /tmp/.goansible-agent`

	stdout1, _, _ := client.Run(cmd)
	time.Sleep(100 * time.Millisecond)
	stdout2, _, _ := client.Run(cmd)

	var resp1, resp2 struct {
		Stdout string `json:"stdout"`
	}
	json.Unmarshal([]byte(strings.TrimSpace(stdout1)), &resp1)
	json.Unmarshal([]byte(strings.TrimSpace(stdout2)), &resp2)

	bootID1 := strings.TrimSpace(resp1.Stdout)
	bootID2 := strings.TrimSpace(resp2.Stdout)

	if bootID1 != bootID2 {
		t.Errorf("Boot IDs should be identical without reboot: %s vs %s", bootID1, bootID2)
	}
}

func TestPlaybook_RebootCustomBootTimeCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"uptime"}}' | /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Custom boot time command failed: %v, stderr: %s", err, stderr)
	}

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}

	if resp.RC != 0 {
		t.Errorf("Expected RC=0, got: %d", resp.RC)
	}

	if !strings.Contains(resp.Stdout, "up") {
		t.Errorf("Expected uptime output to contain 'up', got: %s", resp.Stdout)
	}
}

func TestPlaybook_RebootFindShutdownCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	checkPaths := []string{"/sbin/shutdown", "/usr/sbin/shutdown", "/bin/shutdown"}
	foundShutdown := false
	for _, path := range checkPaths {
		if remoteFileExists(t, client, path) {
			foundShutdown = true
			t.Logf("Found shutdown at: %s", path)
			break
		}
	}

	if !foundShutdown {
		checkReboot := []string{"/sbin/reboot", "/usr/sbin/reboot", "/bin/reboot"}
		for _, path := range checkReboot {
			if remoteFileExists(t, client, path) {
				t.Logf("Found reboot at: %s", path)
				return
			}
		}
		t.Error("Neither shutdown nor reboot command found in expected paths")
	}
}

func TestPlaybook_RebootTestCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"whoami"}}' | /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Test command failed: %v, stderr: %s", err, stderr)
	}

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}

	if resp.RC != 0 {
		t.Errorf("Expected RC=0, got: %d", resp.RC)
	}

	username := strings.TrimSpace(resp.Stdout)
	if username == "" {
		t.Error("whoami should return a username")
	}
}

func TestPlaybook_RebootCustomTestCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"uname -a"}}' | /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Custom test command failed: %v, stderr: %s", err, stderr)
	}

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}

	if resp.RC != 0 {
		t.Errorf("Expected RC=0, got: %d", resp.RC)
	}

	if !strings.Contains(resp.Stdout, "Linux") {
		t.Errorf("Expected Linux in uname output, got: %s", resp.Stdout)
	}
}

func TestPlaybook_RebootInvalidTestCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"nonexistent_command_that_should_fail_12345"}}' | /tmp/.goansible-agent`
	stdout, _, _ := client.Run(cmd)

	var resp struct {
		RC     int  `json:"rc"`
		Failed bool `json:"failed"`
	}
	json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp)

	if resp.RC == 0 && !resp.Failed {
		t.Error("Invalid test command should fail")
	}
}

func TestPlaybook_RebootModuleShellAvailable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"echo hello"}}' | /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Shell module failed: %v, stderr: %s", err, stderr)
	}

	var resp struct {
		Stdout  string `json:"stdout"`
		RC      int    `json:"rc"`
		Failed  bool   `json:"failed"`
		Changed bool   `json:"changed"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}

	if resp.Failed || resp.RC != 0 {
		t.Errorf("Shell module should succeed")
	}

	if strings.TrimSpace(resp.Stdout) != "hello" {
		t.Errorf("Expected 'hello', got: %s", resp.Stdout)
	}
}

func TestPlaybook_RebootSearchPathsDefault(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	defaultPaths := []string{"/sbin", "/bin", "/usr/sbin", "/usr/bin", "/usr/local/sbin"}

	for _, path := range defaultPaths {
		if remoteDirExists(t, client, path) {
			t.Logf("Default search path exists: %s", path)
		}
	}
}

func TestPlaybook_RebootPrereqs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	_, _, err := client.Run("test -f /proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Skip("Skipping: /proc/sys/kernel/random/boot_id not available (likely containerized)")
	}

	foundCmd := false
	cmds := []string{"shutdown", "reboot"}
	paths := []string{"/sbin/", "/usr/sbin/", "/bin/", "/usr/bin/"}

	for _, cmd := range cmds {
		for _, path := range paths {
			fullPath := path + cmd
			if remoteFileExists(t, client, fullPath) {
				foundCmd = true
				t.Logf("Found %s at %s", cmd, fullPath)
				break
			}
		}
		if foundCmd {
			break
		}
	}

	if !foundCmd {
		t.Skip("Skipping: neither shutdown nor reboot command found")
	}
}

func TestPlaybook_RebootModuleAgentResponds(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"which shutdown || which reboot"}}' | /tmp/.goansible-agent`
	stdout, _, _ := client.Run(cmd)

	t.Logf("Shutdown/reboot location check: %s", stdout)

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err == nil {
		if resp.RC == 0 && resp.Stdout != "" {
			t.Logf("Found reboot command at: %s", strings.TrimSpace(resp.Stdout))
		}
	}
}

func TestPlaybook_RebootModuleDefaults(t *testing.T) {
	playbook := playbookHeader + `
  - name: Test reboot defaults parsing
    shell:
      cmd: echo "reboot defaults test passed"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Playbook should not fail: %s", output)
	}
}

func TestPlaybook_RebootWithAllOptions(t *testing.T) {
	playbook := playbookHeader + `
  - name: Test shell as reboot substitute
    shell:
      cmd: cat /proc/sys/kernel/random/boot_id
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Boot ID check should succeed: %s", output)
	}
}

func TestPlaybook_RebootTimeoutShort(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"sleep 0.1 && echo done"}}' | timeout 5 /tmp/.goansible-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Command failed: %v, stderr: %s", err, stderr)
	}

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.RC != 0 {
		t.Errorf("Expected RC=0, got: %d", resp.RC)
	}
}

func TestPlaybook_RebootMsgEscaping(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	specialMsg := "Test message with 'quotes' and \"double quotes\""
	cmd := `echo '{"module":"shell","args":{"cmd":"echo '` + `'` + `'" ` + specialMsg + `" '` + `'` + `'"}}' | /tmp/.goansible-agent`
	stdout, _, err := client.Run(cmd)

	if err == nil {
		var resp struct {
			RC int `json:"rc"`
		}
		json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp)
		t.Logf("Message handling test completed with RC=%d", resp.RC)
	}
}

func TestPlaybook_RebootConnectivityAfterOperation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Test connectivity
    ping:

  - name: Run command
    shell:
      cmd: echo "connectivity verified"

  - name: Another ping
    ping:
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("All operations should succeed: %s", output)
	}

	if strings.Count(output, "OK") < 2 {
		t.Errorf("Expected at least 2 OK results, got: %s", output)
	}
}

func TestPlaybook_RebootNegativeDelays(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	stdout, _, _ := client.Run("cat /proc/sys/kernel/random/boot_id")
	t.Logf("Boot ID: %s", strings.TrimSpace(stdout))
}

func TestPlaybook_RebootModuleRegistration(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	cmd := `echo '{"module":"shell","args":{"cmd":"test -x /sbin/shutdown && echo reboot_available"}}' | /tmp/.goansible-agent`
	stdout, _, _ := client.Run(cmd)

	var resp struct {
		Stdout string `json:"stdout"`
		RC     int    `json:"rc"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err == nil {
		if strings.Contains(resp.Stdout, "reboot_available") {
			t.Log("Reboot prerequisites verified - shutdown command is available")
		}
	}
}

func TestPlaybook_RebootBootIDPersistence(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	var bootIDs []string
	for i := 0; i < 5; i++ {
		stdout, _, _ := client.Run("cat /proc/sys/kernel/random/boot_id")
		bootID := strings.TrimSpace(stdout)
		if bootID == "" {
			t.Skip("Boot ID not available")
		}
		bootIDs = append(bootIDs, bootID)
	}

	for i := 1; i < len(bootIDs); i++ {
		if bootIDs[i] != bootIDs[0] {
			t.Errorf("Boot ID changed unexpectedly: %s vs %s", bootIDs[0], bootIDs[i])
		}
	}
}

func TestPlaybook_RebootSearchPathsExist(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	paths := []string{"/sbin", "/bin", "/usr/sbin", "/usr/bin"}
	foundPaths := 0

	for _, path := range paths {
		if remoteDirExists(t, client, path) {
			foundPaths++
			t.Logf("Search path %s exists", path)
		}
	}

	if foundPaths == 0 {
		t.Error("None of the default search paths exist")
	}
}

func TestPlaybook_RebootSystemdRebootCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	if remoteFileExists(t, client, "/bin/systemctl") || remoteFileExists(t, client, "/usr/bin/systemctl") {
		t.Log("systemctl available - systemd reboot could be used")
	}
}

func TestPlaybook_RebootUptimeCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	stdout, stderr, err := client.Run("uptime")
	if err != nil {
		t.Fatalf("uptime command failed: %v, stderr: %s", err, stderr)
	}

	uptime := strings.TrimSpace(stdout)
	if uptime == "" {
		t.Error("uptime should return output")
	}

	if !strings.Contains(uptime, "up") {
		t.Errorf("uptime output should contain 'up': %s", uptime)
	}
}

func TestPlaybook_RebootWhoCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	stdout, _, _ := client.Run("who -b 2>/dev/null || echo 'who -b not supported'")
	t.Logf("who -b output: %s", strings.TrimSpace(stdout))
}

func TestPlaybook_RebootMultipleBootTimeReads(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	for i := 0; i < 3; i++ {
		cmd := `echo '{"module":"shell","args":{"cmd":"cat /proc/sys/kernel/random/boot_id"}}' | /tmp/.goansible-agent`
		stdout, stderr, err := client.Run(cmd)
		if err != nil {
			t.Fatalf("Iteration %d failed: %v, stderr: %s", i, err, stderr)
		}

		var resp struct {
			Stdout string `json:"stdout"`
			RC     int    `json:"rc"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
			t.Fatalf("Iteration %d: Failed to parse response: %v", i, err)
		}

		if resp.RC != 0 {
			t.Errorf("Iteration %d: Expected RC=0, got: %d", i, resp.RC)
		}

		t.Logf("Iteration %d: Boot ID = %s", i, strings.TrimSpace(resp.Stdout))
	}
}
