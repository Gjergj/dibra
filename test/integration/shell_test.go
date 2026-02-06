//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type ShellResponse struct {
	Changed     bool     `json:"changed"`
	Failed      bool     `json:"failed"`
	Msg         string   `json:"msg,omitempty"`
	Cmd         string   `json:"cmd"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	StdoutLines []string `json:"stdout_lines"`
	StderrLines []string `json:"stderr_lines"`
	RC          int      `json:"rc"`
	Start       string   `json:"start,omitempty"`
	End         string   `json:"end,omitempty"`
	Delta       string   `json:"delta,omitempty"`
}

func getShellResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args string) ShellResponse {
	t.Helper()
	cmd := `echo '{"module":"shell","args":` + args + `}' | /tmp/.dibra-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Logf("Agent stderr: %s", stderr)
	}

	var resp ShellResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func TestPlaybook_ShellBasic(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run echo command via shell
    shell:
      cmd: echo hello
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for shell execution")
	}
}

func TestPlaybook_ShellEcho(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo hello"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello" {
		t.Errorf("Expected stdout=hello, got: %q", resp.Stdout)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for shell execution")
	}
	if resp.RC != 0 {
		t.Errorf("Expected rc=0, got: %d", resp.RC)
	}
}

func TestPlaybook_ShellReturnsCmdString(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo hello world"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Cmd != "echo hello world" {
		t.Errorf("Expected cmd='echo hello world', got: %q", resp.Cmd)
	}
}

func TestPlaybook_ShellWithPipe(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo hello world | grep world"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello world" {
		t.Errorf("Expected stdout='hello world', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithRedirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/shell-redirect-test")
	defer remoteExec(t, client, "rm -f /tmp/shell-redirect-test")

	resp := getShellResult(t, client, `{"cmd":"echo 'test content' > /tmp/shell-redirect-test"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	content := remoteFileContent(t, client, "/tmp/shell-redirect-test")
	if content != "test content" {
		t.Errorf("Expected file content='test content', got: %q", content)
	}
}

func TestPlaybook_ShellWithAppendRedirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/shell-append-test")
	remoteExec(t, client, "echo 'first' > /tmp/shell-append-test")
	defer remoteExec(t, client, "rm -f /tmp/shell-append-test")

	resp := getShellResult(t, client, `{"cmd":"echo 'second' >> /tmp/shell-append-test"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	content := remoteFileContent(t, client, "/tmp/shell-append-test")
	if !strings.Contains(content, "first") || !strings.Contains(content, "second") {
		t.Errorf("Expected file to contain 'first' and 'second', got: %q", content)
	}
}

func TestPlaybook_ShellWithEnvVar(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo $HOME"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout == "" || resp.Stdout == "$HOME" {
		t.Errorf("Expected HOME to be expanded, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithCommandSubstitution(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo $(whoami)"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout == "" || resp.Stdout == "$(whoami)" {
		t.Errorf("Expected command substitution to work, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithBacktickSubstitution(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, "{\"cmd\":\"echo `hostname`\"}")

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout == "" {
		t.Error("Expected hostname output")
	}
}

func TestPlaybook_ShellWithLogicalAnd(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"true && echo success"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "success" {
		t.Errorf("Expected stdout='success', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithLogicalOr(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"false || echo fallback"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "fallback" {
		t.Errorf("Expected stdout='fallback', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithSemicolon(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo first; echo second"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "first") || !strings.Contains(resp.Stdout, "second") {
		t.Errorf("Expected stdout to contain 'first' and 'second', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithWildcard(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/shell-wild-*.txt")
	remoteExec(t, client, "touch /tmp/shell-wild-a.txt /tmp/shell-wild-b.txt")
	defer remoteExec(t, client, "rm -f /tmp/shell-wild-*.txt")

	resp := getShellResult(t, client, `{"cmd":"ls /tmp/shell-wild-*.txt | wc -l"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if strings.TrimSpace(resp.Stdout) != "2" {
		t.Errorf("Expected 2 files, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithChdir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"pwd","chdir":"/tmp"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "/tmp" {
		t.Errorf("Expected stdout=/tmp, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellChdirNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"pwd","chdir":"/nonexistent/directory"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-existent directory")
	}
	if !strings.Contains(resp.Msg, "Unable to change directory") {
		t.Errorf("Expected directory error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ShellWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-shell-creates")
	defer remoteExec(t, client, "rm -f /tmp/test-shell-creates")

	resp := getShellResult(t, client, `{"cmd":"echo should not run","creates":"/tmp/test-shell-creates"}`)

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

func TestPlaybook_ShellCreatesNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-shell-creates-not")

	resp := getShellResult(t, client, `{"cmd":"echo should run","creates":"/tmp/test-shell-creates-not"}`)

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

func TestPlaybook_ShellWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-shell-removes")
	defer remoteExec(t, client, "rm -f /tmp/test-shell-removes")

	resp := getShellResult(t, client, `{"cmd":"echo should run","removes":"/tmp/test-shell-removes"}`)

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

func TestPlaybook_ShellRemovesNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-shell-removes-not")

	resp := getShellResult(t, client, `{"cmd":"echo should not run","removes":"/tmp/test-shell-removes-not"}`)

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

func TestPlaybook_ShellCreatesGlobPattern(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/shell-glob-*.txt")
	remoteExec(t, client, "touch /tmp/shell-glob-abc.txt")
	defer remoteExec(t, client, "rm -f /tmp/shell-glob-*.txt")

	resp := getShellResult(t, client, `{"cmd":"echo should skip","creates":"/tmp/shell-glob-*.txt"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when wildcard creates pattern matches")
	}
}

func TestPlaybook_ShellRemovesGlobPattern(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/shell-glob-rem-*.txt")
	remoteExec(t, client, "touch /tmp/shell-glob-rem-abc.txt")
	defer remoteExec(t, client, "rm -f /tmp/shell-glob-rem-*.txt")

	resp := getShellResult(t, client, `{"cmd":"echo should run","removes":"/tmp/shell-glob-rem-*.txt"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true when wildcard removes pattern matches")
	}
}

func TestPlaybook_ShellWithStdin(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"cat","stdin":"hello from stdin"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello from stdin" {
		t.Errorf("Expected stdout='hello from stdin', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithStdinNoNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"wc -l","stdin":"line1\nline2","stdin_add_newline":false}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if strings.TrimSpace(resp.Stdout) != "1" {
		t.Errorf("Expected 1 line (no trailing newline), got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellWithStdinAddNewline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"wc -l","stdin":"line1\nline2","stdin_add_newline":true}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if strings.TrimSpace(resp.Stdout) != "2" {
		t.Errorf("Expected 2 lines (with trailing newline), got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellNonZeroRC(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"exit 42"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-zero exit code")
	}
	if resp.RC != 42 {
		t.Errorf("Expected rc=42, got: %d", resp.RC)
	}
}

func TestPlaybook_ShellStderr(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo error >&2"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stderr != "error" {
		t.Errorf("Expected stderr='error', got: %q", resp.Stderr)
	}
}

func TestPlaybook_ShellStdoutLines(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo 'line1'; echo 'line2'; echo 'line3'"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.StdoutLines) != 3 {
		t.Errorf("Expected 3 stdout_lines, got: %d", len(resp.StdoutLines))
	}
}

func TestPlaybook_ShellStripEmptyEnds(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo hello","strip_empty_ends":true}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello" {
		t.Errorf("Expected stripped stdout='hello', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellNoStripEmptyEnds(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo hello","strip_empty_ends":false}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello\n" {
		t.Errorf("Expected unstripped stdout='hello\\n', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellTiming(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo test"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Start == "" {
		t.Error("Expected start timestamp")
	}
	if resp.End == "" {
		t.Error("Expected end timestamp")
	}
	if resp.Delta == "" {
		t.Error("Expected delta")
	}
}

func TestPlaybook_ShellNoCmd(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{}`)

	if !resp.Failed {
		t.Error("Expected failure when no cmd specified")
	}
	if !strings.Contains(resp.Msg, "no command") {
		t.Errorf("Expected 'no command' error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ShellCustomExecutable(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo $0","executable":"/bin/bash"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "bash") {
		t.Errorf("Expected $0 to contain 'bash', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellCustomExecutableSh(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo $0","executable":"/bin/sh"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout == "" {
		t.Error("Expected some output for $0")
	}
}

func TestPlaybook_ShellCommandNotFound(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"nonexistent_command_xyz123_456"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-existent command")
	}
	if resp.RC == 0 {
		t.Error("Expected non-zero return code")
	}
}

func TestPlaybook_ShellFalseCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"false"}`)

	if !resp.Failed {
		t.Error("Expected failure for false command")
	}
	if resp.RC != 1 {
		t.Errorf("Expected rc=1, got: %d", resp.RC)
	}
}

func TestPlaybook_ShellTrueCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"true"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.RC != 0 {
		t.Errorf("Expected rc=0, got: %d", resp.RC)
	}
}

func TestPlaybook_ShellEmptyStdout(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"true"}`)

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

func TestPlaybook_ShellChangedAlways(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	for i := 0; i < 3; i++ {
		resp := getShellResult(t, client, `{"cmd":"echo test"}`)
		if !resp.Changed {
			t.Errorf("Run %d: Expected changed=true for shell execution", i+1)
		}
	}
}

func TestPlaybook_ShellIdempotencyWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/shell-idem-creates")
	defer remoteExec(t, client, "rm -f /tmp/shell-idem-creates")

	playbook := playbookHeader + `
  - name: Create file if not exists
    shell:
      cmd: touch /tmp/shell-idem-creates
      creates: /tmp/shell-idem-creates
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

func TestPlaybook_ShellIdempotencyWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/shell-idem-removes")
	defer remoteExec(t, client, "rm -f /tmp/shell-idem-removes")

	playbook := playbookHeader + `
  - name: Remove file if exists
    shell:
      cmd: rm /tmp/shell-idem-removes
      removes: /tmp/shell-idem-removes
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

func TestPlaybook_ShellPlaybookWithPipe(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run shell with pipe
    shell:
      cmd: echo -e "a\nb\nc" | wc -l
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for shell execution")
	}
}

func TestPlaybook_ShellPlaybookWithRedirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/shell-pb-redirect")
	defer remoteExec(t, client, "rm -f /tmp/shell-pb-redirect")

	playbook := playbookHeader + `
  - name: Run shell with redirect
    shell:
      cmd: echo "hello from playbook" > /tmp/shell-pb-redirect
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	content := remoteFileContent(t, client, "/tmp/shell-pb-redirect")
	if content != "hello from playbook" {
		t.Errorf("Expected file content, got: %q", content)
	}
}

func TestPlaybook_ShellPlaybookWithChdir(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run pwd in /tmp
    shell:
      cmd: pwd
      chdir: /tmp
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for shell execution")
	}
}

func TestPlaybook_ShellPlaybookWithCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/shell-pb-creates-exists")
	defer remoteExec(t, client, "rm -f /tmp/shell-pb-creates-exists")

	playbook := playbookHeader + `
  - name: Skip if file exists
    shell:
      cmd: echo "should not run"
      creates: /tmp/shell-pb-creates-exists
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Should skip when creates file exists")
	}
}

func TestPlaybook_ShellPlaybookWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/shell-pb-removes-gone")

	playbook := playbookHeader + `
  - name: Skip if file does not exist
    shell:
      cmd: echo "should not run"
      removes: /tmp/shell-pb-removes-gone
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Should skip when removes file does not exist")
	}
}

func TestPlaybook_ShellPlaybookWithExecutable(t *testing.T) {
	playbook := playbookHeader + `
  - name: Run with bash
    shell:
      cmd: echo $BASH_VERSION
      executable: /bin/bash
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for shell execution")
	}
}

func TestPlaybook_ShellMultipleCommands(t *testing.T) {
	playbook := playbookHeader + `
  - name: First shell command
    shell:
      cmd: echo first

  - name: Second shell command
    shell:
      cmd: echo second

  - name: Third shell command
    shell:
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

func TestPlaybook_ShellForLoop(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"for i in 1 2 3; do echo $i; done"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.StdoutLines) != 3 {
		t.Errorf("Expected 3 lines, got: %d", len(resp.StdoutLines))
	}
}

func TestPlaybook_ShellHereDoc(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"cat <<EOF\nline1\nline2\nEOF"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "line1") || !strings.Contains(resp.Stdout, "line2") {
		t.Errorf("Expected heredoc output, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellProcessSubstitution(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"diff <(echo hello) <(echo hello)","executable":"/bin/bash"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.RC != 0 {
		t.Errorf("Expected rc=0 for identical comparison, got: %d", resp.RC)
	}
}

func TestPlaybook_ShellSubshell(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"(cd /tmp && pwd)"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "/tmp" {
		t.Errorf("Expected stdout=/tmp from subshell, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellQuotedArguments(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo 'hello world with spaces'"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "hello world with spaces" {
		t.Errorf("Expected 'hello world with spaces', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellDoubleQuotes(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"VAR=test; echo \"value is $VAR\""}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "value is test" {
		t.Errorf("Expected 'value is test', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellSpecialChars(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo 'dollar sign' \"and quote\""}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "dollar sign and quote" {
		t.Errorf("Expected 'dollar sign and quote', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellExitCodePropagation(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	testCases := []struct {
		cmd      string
		expected int
	}{
		{`{"cmd":"exit 0"}`, 0},
		{`{"cmd":"exit 1"}`, 1},
		{`{"cmd":"exit 127"}`, 127},
		{`{"cmd":"exit 255"}`, 255},
	}

	for _, tc := range testCases {
		resp := getShellResult(t, client, tc.cmd)
		if resp.RC != tc.expected {
			t.Errorf("For %s: expected rc=%d, got: %d", tc.cmd, tc.expected, resp.RC)
		}
	}
}

func TestPlaybook_ShellComplexPipeline(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"ls /etc | sort | head -5"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	lines := resp.StdoutLines
	if len(lines) < 2 {
		t.Errorf("Expected at least 2 lines, got: %d", len(lines))
	}
}

func TestPlaybook_ShellMixedStdoutStderr(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo stdout; echo stderr >&2; echo stdout2"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "stdout") {
		t.Errorf("Expected stdout output, got: %q", resp.Stdout)
	}
	if !strings.Contains(resp.Stderr, "stderr") {
		t.Errorf("Expected stderr output, got: %q", resp.Stderr)
	}
}

func TestPlaybook_ShellStderrToStdout(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo error >&2 2>&1"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
}

func TestPlaybook_ShellDevNull(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo visible; echo hidden > /dev/null"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "visible" {
		t.Errorf("Expected only 'visible', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellInputFromFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "echo 'content from file' > /tmp/shell-input-test")
	defer remoteExec(t, client, "rm -f /tmp/shell-input-test")

	resp := getShellResult(t, client, `{"cmd":"cat < /tmp/shell-input-test"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "content from file" {
		t.Errorf("Expected 'content from file', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellSingleVsDoubleQuotes(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo 'literal text' vs \"$HOME\""}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "literal text vs /") {
		t.Errorf("Expected single quotes to preserve literal, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellArrayBash(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"arr=(one two three); echo ${arr[1]}","executable":"/bin/bash"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "two" {
		t.Errorf("Expected 'two', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellArithmeticExpansion(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getShellResult(t, client, `{"cmd":"echo $((5 + 3))"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "8" {
		t.Errorf("Expected '8', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ShellVsCommand(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	shellResp := getShellResult(t, client, `{"cmd":"echo $HOME | cat"}`)
	if shellResp.Failed {
		t.Fatalf("Shell execution failed: %s", shellResp.Msg)
	}

	cmdResp := getCommandResult(t, client, `{"cmd":"echo | cat"}`)
	if !cmdResp.Failed {
	}
}
