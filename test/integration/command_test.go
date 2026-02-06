//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type CommandResponse struct {
	Changed     bool     `json:"changed"`
	Failed      bool     `json:"failed"`
	Msg         string   `json:"msg,omitempty"`
	Cmd         []string `json:"cmd"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	StdoutLines []string `json:"stdout_lines"`
	StderrLines []string `json:"stderr_lines"`
	RC          int      `json:"rc"`
	Start       string   `json:"start,omitempty"`
	End         string   `json:"end,omitempty"`
	Delta       string   `json:"delta,omitempty"`
}

func getCommandResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args string) CommandResponse {
	t.Helper()
	cmd := `echo '{"module":"command","args":` + args + `}' | /tmp/.dibra-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Logf("Agent stderr: %s", stderr)
	}

	var resp CommandResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func TestPlaybook_CommandBasic(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run echo command
    command:
      cmd: echo hello
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for command execution")
	}
}

func TestPlaybook_CommandReturnsParsedArgs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"echo hello world"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.Cmd) != 3 {
		t.Errorf("Expected 3 args, got: %v", resp.Cmd)
	}
	if resp.Cmd[0] != "echo" || resp.Cmd[1] != "hello" || resp.Cmd[2] != "world" {
		t.Errorf("Expected [echo, hello, world], got: %v", resp.Cmd)
	}
}

func TestPlaybook_CommandEcho(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"echo hello"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello" {
		t.Errorf("Expected stdout=hello, got: %q", resp.Stdout)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for command execution")
	}
	if resp.RC != 0 {
		t.Errorf("Expected rc=0, got: %d", resp.RC)
	}
}

func TestPlaybook_CommandWithArgv(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["echo","hello","world"]}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello world" {
		t.Errorf("Expected stdout='hello world', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandArgvWithSpaces(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["echo","hello world with spaces"]}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello world with spaces" {
		t.Errorf("Expected stdout='hello world with spaces', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandWithChdir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"pwd","chdir":"/tmp"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "/tmp" {
		t.Errorf("Expected stdout=/tmp, got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandChdirNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"pwd","chdir":"/nonexistent/directory"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-existent directory")
	}
	if !strings.Contains(resp.Msg, "Unable to change directory") {
		t.Errorf("Expected directory error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_CommandWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-creates-file")
	defer remoteExec(t, client, "rm -f /tmp/test-creates-file")

	resp := getCommandResult(t, client, `{"cmd":"echo should not run","creates":"/tmp/test-creates-file"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when creates file exists")
	}
	if !strings.Contains(resp.Msg, "skipped") {
		t.Errorf("Expected skip message, got: %s", resp.Msg)
	}
}

func TestPlaybook_CommandCreatesNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-creates-not-exists")

	resp := getCommandResult(t, client, `{"cmd":"echo should run","creates":"/tmp/test-creates-not-exists"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true when creates file does not exist")
	}
	if resp.Stdout != "should run" {
		t.Errorf("Expected stdout='should run', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-removes-file")
	defer remoteExec(t, client, "rm -f /tmp/test-removes-file")

	resp := getCommandResult(t, client, `{"cmd":"echo should run","removes":"/tmp/test-removes-file"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true when removes file exists")
	}
	if resp.Stdout != "should run" {
		t.Errorf("Expected stdout='should run', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandRemovesNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-removes-not-exists")

	resp := getCommandResult(t, client, `{"cmd":"echo should not run","removes":"/tmp/test-removes-not-exists"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when removes file does not exist")
	}
	if !strings.Contains(resp.Msg, "skipped") {
		t.Errorf("Expected skip message, got: %s", resp.Msg)
	}
}

func TestPlaybook_CommandCreatesGlobPattern(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-glob-pattern.txt")
	defer remoteExec(t, client, "rm -f /tmp/test-glob-pattern.txt")

	resp := getCommandResult(t, client, `{"cmd":"echo should not run","creates":"/tmp/test-glob-*.txt"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when glob pattern matches")
	}
}

func TestPlaybook_CommandRemovesGlobPattern(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-glob-removes.log")
	defer remoteExec(t, client, "rm -f /tmp/test-glob-removes.log")

	resp := getCommandResult(t, client, `{"cmd":"echo should run","removes":"/tmp/test-glob-removes.*"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true when removes glob pattern matches")
	}
}

func TestPlaybook_CommandNonZeroRC(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"exit 42","argv":["sh","-c","exit 42"]}`)

	if !resp.Failed {
		t.Error("Expected failed=true for non-zero return code")
	}
	if resp.RC != 42 {
		t.Errorf("Expected rc=42, got: %d", resp.RC)
	}
	if !strings.Contains(resp.Msg, "non-zero return code") {
		t.Errorf("Expected non-zero return code message, got: %s", resp.Msg)
	}
}

func TestPlaybook_CommandStderr(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["sh","-c","echo error >&2"]}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stderr != "error" {
		t.Errorf("Expected stderr='error', got: %q", resp.Stderr)
	}
}

func TestPlaybook_CommandStdoutLines(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["sh","-c","echo line1; echo line2; echo line3"]}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.StdoutLines) != 3 {
		t.Errorf("Expected 3 stdout lines, got: %d (%v)", len(resp.StdoutLines), resp.StdoutLines)
	}
	if resp.StdoutLines[0] != "line1" || resp.StdoutLines[1] != "line2" || resp.StdoutLines[2] != "line3" {
		t.Errorf("Expected [line1, line2, line3], got: %v", resp.StdoutLines)
	}
}

func TestPlaybook_CommandStderrLines(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["sh","-c","echo err1 >&2; echo err2 >&2"]}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.StderrLines) != 2 {
		t.Errorf("Expected 2 stderr lines, got: %d (%v)", len(resp.StderrLines), resp.StderrLines)
	}
}

func TestPlaybook_CommandStripEmptyEnds(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["sh","-c","echo hello"],"strip_empty_ends":true}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if strings.HasSuffix(resp.Stdout, "\n") {
		t.Error("Expected trailing newline to be stripped")
	}
}

func TestPlaybook_CommandNoStripEmptyEnds(t *testing.T) {
	playbook := playbookHeader + `
  - name: Echo with trailing newline
    command:
      argv:
        - sh
        - -c
        - "printf 'hello\\n'"
      strip_empty_ends: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for command execution")
	}
}

func TestPlaybook_CommandWithStdin(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"argv":["cat"],"stdin":"hello from stdin"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "hello from stdin") {
		t.Errorf("Expected stdout to contain stdin content, got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandTiming(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"echo hello"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Start == "" {
		t.Error("Expected start time to be set")
	}
	if resp.End == "" {
		t.Error("Expected end time to be set")
	}
	if resp.Delta == "" {
		t.Error("Expected delta time to be set")
	}
}

func TestPlaybook_CommandNoCmdOrArgv(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{}`)

	if !resp.Failed {
		t.Error("Expected failure when neither cmd nor argv provided")
	}
	if !strings.Contains(resp.Msg, "required") {
		t.Errorf("Expected 'required' in error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_CommandLs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"ls /tmp"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for command execution")
	}
}

func TestPlaybook_CommandUptime(t *testing.T) {
	playbook := playbookHeader + `
  - name: Check system uptime
    command:
      cmd: uptime
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
}

func TestPlaybook_CommandHostname(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"hostname"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout == "" {
		t.Error("Expected hostname output")
	}
}

func TestPlaybook_CommandDate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"date +%Y"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.Stdout) != 4 {
		t.Errorf("Expected 4-digit year, got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandWhoami(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"whoami"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "root" {
		t.Errorf("Expected 'root', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandQuotedArgs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"echo 'hello world'"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello world" {
		t.Errorf("Expected 'hello world', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandDoubleQuotedArgs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"echo \"hello world\""}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello world" {
		t.Errorf("Expected 'hello world', got: %q", resp.Stdout)
	}
}

func TestPlaybook_CommandPlaybookWithChdir(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run pwd in /tmp
    command:
      cmd: pwd
      chdir: /tmp
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for command execution")
	}
}

func TestPlaybook_CommandPlaybookWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/command-creates-test")
	defer remoteExec(t, client, "rm -f /tmp/command-creates-test")

	playbook := playbookHeader + `
  - name: Skip if file exists
    command:
      cmd: echo this should be skipped
      creates: /tmp/command-creates-test
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when creates file exists")
	}
}

func TestPlaybook_CommandPlaybookWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/command-removes-nonexistent")

	playbook := playbookHeader + `
  - name: Skip if file does not exist
    command:
      cmd: echo this should be skipped
      removes: /tmp/command-removes-nonexistent
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when removes file does not exist")
	}
}

func TestPlaybook_CommandPlaybookWithArgv(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run command with argv
    command:
      argv:
        - echo
        - "hello from argv"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for command execution")
	}
}

func TestPlaybook_CommandMultipleCommands(t *testing.T) {
	playbook := playbookHeader + `
  - name: First command
    command:
      cmd: echo first

  - name: Second command
    command:
      cmd: echo second

  - name: Third command
    command:
      cmd: echo third
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	changedCount := strings.Count(output, "CHANGED")
	if changedCount != 3 {
		t.Errorf("Expected 3 CHANGED, got: %d", changedCount)
	}
}

func TestPlaybook_CommandIdempotencyWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/idempotent-creates-test")
	defer remoteExec(t, client, "rm -f /tmp/idempotent-creates-test")

	playbook := playbookHeader + `
  - name: Create file if not exists
    command:
      cmd: touch /tmp/idempotent-creates-test
      creates: /tmp/idempotent-creates-test
`

	output1 := runPlaybook(t, playbook)
	if !strings.Contains(output1, "CHANGED") {
		t.Error("First run should make changes")
	}

	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Second run should not make changes (creates file exists)")
	}
}

func TestPlaybook_CommandIdempotencyWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/idempotent-removes-test")
	defer remoteExec(t, client, "rm -f /tmp/idempotent-removes-test")

	playbook := playbookHeader + `
  - name: Remove file if exists
    command:
      cmd: rm /tmp/idempotent-removes-test
      removes: /tmp/idempotent-removes-test
`

	output1 := runPlaybook(t, playbook)
	if !strings.Contains(output1, "CHANGED") {
		t.Error("First run should make changes")
	}

	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Second run should not make changes (removes file no longer exists)")
	}
}

func TestPlaybook_CommandCommandNotFound(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"nonexistent_command_xyz123"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-existent command")
	}
}

func TestPlaybook_CommandBinaryExecutable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"/bin/true"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.RC != 0 {
		t.Errorf("Expected rc=0, got: %d", resp.RC)
	}
}

func TestPlaybook_CommandFalseCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"/bin/false"}`)

	if !resp.Failed {
		t.Error("Expected failure for /bin/false")
	}
	if resp.RC != 1 {
		t.Errorf("Expected rc=1, got: %d", resp.RC)
	}
}

func TestPlaybook_CommandCat(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"cat /etc/hostname"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout == "" {
		t.Error("Expected hostname content")
	}
}

func TestPlaybook_CommandEmptyStdout(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getCommandResult(t, client, `{"cmd":"/bin/true"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "" {
		t.Errorf("Expected empty stdout, got: %q", resp.Stdout)
	}
	if len(resp.StdoutLines) != 0 {
		t.Errorf("Expected empty stdout_lines, got: %v", resp.StdoutLines)
	}
}

func TestPlaybook_CommandCreatesWithWildcard(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/wildcard-test-*.txt")
	remoteExec(t, client, "touch /tmp/wildcard-test-abc.txt")
	defer remoteExec(t, client, "rm -f /tmp/wildcard-test-*.txt")

	resp := getCommandResult(t, client, `{"cmd":"echo should skip","creates":"/tmp/wildcard-test-*.txt"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when wildcard creates pattern matches")
	}
}

func TestPlaybook_CommandChangedAlways(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	for i := 0; i < 3; i++ {
		resp := getCommandResult(t, client, `{"cmd":"echo test"}`)
		if !resp.Changed {
			t.Errorf("Run %d: Expected changed=true for command execution", i+1)
		}
	}
}

func TestPlaybook_CommandMkdirWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -rf /tmp/command-mkdir-test")
	defer remoteExec(t, client, "rm -rf /tmp/command-mkdir-test")

	playbook := playbookHeader + `
  - name: Create directory idempotently
    command:
      cmd: mkdir /tmp/command-mkdir-test
      creates: /tmp/command-mkdir-test
`

	output1 := runPlaybook(t, playbook)
	if !strings.Contains(output1, "CHANGED") {
		t.Error("First run should make changes")
	}

	if !remoteDirExists(t, client, "/tmp/command-mkdir-test") {
		t.Error("Directory should exist after first run")
	}

	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Second run should skip (directory exists)")
	}
}
