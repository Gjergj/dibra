//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ScriptResponse struct {
	Changed     bool     `json:"changed"`
	Failed      bool     `json:"failed,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"`
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

func getScriptResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args string) ScriptResponse {
	t.Helper()
	cmd := `echo '{"module":"script","args":` + args + `}' | /tmp/.dibra-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Logf("Agent stderr: %s", stderr)
	}

	var resp ScriptResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func createTestScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}
	return path
}

func TestPlaybook_ScriptBasic(t *testing.T) {
	scriptPath := createTestScript(t, "test.sh", `#!/bin/bash
echo "win"
`)

	playbook := playbookHeader + `
  - name: Run basic script
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptWithOutput(t *testing.T) {
	scriptPath := createTestScript(t, "output.sh", `#!/bin/bash
echo "hello from script"
`)

	playbook := playbookHeader + `
  - name: Run script with output
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptNonZeroExit(t *testing.T) {
	scriptPath := createTestScript(t, "exit1.sh", `#!/bin/bash
exit 1
`)

	playbook := playbookHeader + `
  - name: Run script with non-zero exit
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Fatalf("Expected failure for non-zero exit, got: %s", output)
	}
}

func TestPlaybook_ScriptWithArguments(t *testing.T) {
	scriptPath := createTestScript(t, "args.sh", `#!/bin/bash
echo "arg1=$1 arg2=$2"
`)

	playbook := playbookHeader + `
  - name: Run script with arguments
    script:
      cmd: ` + scriptPath + ` hello world
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptWithUnicodeArguments(t *testing.T) {
	scriptPath := createTestScript(t, "unicode.sh", `#!/bin/bash
echo "arg1=$1"
`)

	playbook := playbookHeader + `
  - name: Run script with unicode arguments
    script:
      cmd: ` + scriptPath + ` "Ӧther"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptCreatesFileExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/script-creates-test")
	defer remoteExec(t, client, "rm -f /tmp/script-creates-test")

	scriptPath := createTestScript(t, "creates.sh", `#!/bin/bash
echo "should not run"
`)

	playbook := playbookHeader + `
  - name: Run script with creates (should skip)
    script:
      cmd: ` + scriptPath + `
      creates: /tmp/script-creates-test
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success (skip), got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected NO changes when creates file exists")
	}
}

func TestPlaybook_ScriptCreatesFileNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/script-creates-not-exists")

	scriptPath := createTestScript(t, "creates.sh", `#!/bin/bash
echo "should run"
`)

	playbook := playbookHeader + `
  - name: Run script with creates (should run)
    script:
      cmd: ` + scriptPath + `
      creates: /tmp/script-creates-not-exists
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes when creates file does not exist")
	}
}

func TestPlaybook_ScriptRemovesFileExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/script-removes-test")
	defer remoteExec(t, client, "rm -f /tmp/script-removes-test")

	scriptPath := createTestScript(t, "removes.sh", `#!/bin/bash
echo "should run"
`)

	playbook := playbookHeader + `
  - name: Run script with removes (should run)
    script:
      cmd: ` + scriptPath + `
      removes: /tmp/script-removes-test
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes when removes file exists")
	}
}

func TestPlaybook_ScriptRemovesFileNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/script-removes-not-exists")

	scriptPath := createTestScript(t, "removes.sh", `#!/bin/bash
echo "should not run"
`)

	playbook := playbookHeader + `
  - name: Run script with removes (should skip)
    script:
      cmd: ` + scriptPath + `
      removes: /tmp/script-removes-not-exists
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success (skip), got: %s", output)
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected NO changes when removes file does not exist")
	}
}

func TestPlaybook_ScriptWithChdir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	scriptPath := createTestScript(t, "chdir.sh", `#!/bin/bash
pwd
`)

	playbook := playbookHeader + `
  - name: Run script with chdir
    script:
      cmd: ` + scriptPath + `
      chdir: /tmp
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptWithExecutable(t *testing.T) {
	scriptPath := createTestScript(t, "noshebang.py", `import sys
print("Python version:", sys.version_info.major)
`)

	playbook := playbookHeader + `
  - name: Run Python script with executable
    script:
      cmd: ` + scriptPath + `
      executable: /usr/bin/python3
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptIdempotency(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/script-idempotent-marker")
	defer remoteExec(t, client, "rm -f /tmp/script-idempotent-marker")

	scriptPath := createTestScript(t, "idempotent.sh", `#!/bin/bash
touch /tmp/script-idempotent-marker
echo "created marker"
`)

	playbook := playbookHeader + `
  - name: Run idempotent script
    script:
      cmd: ` + scriptPath + `
      creates: /tmp/script-idempotent-marker
`

	output1 := runPlaybook(t, playbook)
	if !strings.Contains(output1, "CHANGED") {
		t.Error("First run should make changes")
	}

	if !remoteFileExists(t, client, "/tmp/script-idempotent-marker") {
		t.Error("Marker file should exist after first run")
	}

	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Second run should NOT make changes (creates file exists)")
	}
}

func TestPlaybook_ScriptIdempotencyWithRemoves(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "touch /tmp/script-removes-marker")
	defer remoteExec(t, client, "rm -f /tmp/script-removes-marker")

	scriptPath := createTestScript(t, "remove.sh", `#!/bin/bash
rm -f /tmp/script-removes-marker
echo "removed marker"
`)

	playbook := playbookHeader + `
  - name: Run removal script
    script:
      cmd: ` + scriptPath + `
      removes: /tmp/script-removes-marker
`

	output1 := runPlaybook(t, playbook)
	if !strings.Contains(output1, "CHANGED") {
		t.Error("First run should make changes")
	}

	if remoteFileExists(t, client, "/tmp/script-removes-marker") {
		t.Error("Marker file should NOT exist after first run")
	}

	output2 := runPlaybook(t, playbook)
	if strings.Contains(output2, "CHANGED") {
		t.Error("Second run should NOT make changes (removes file no longer exists)")
	}
}

func TestPlaybook_ScriptStdout(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-stdout.sh")
	remoteExec(t, client, `cat > /tmp/test-stdout.sh << 'EOF'
#!/bin/bash
echo "line1"
echo "line2"
echo "line3"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-stdout.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-stdout.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-stdout.sh"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if len(resp.StdoutLines) != 3 {
		t.Errorf("Expected 3 stdout lines, got: %d (%v)", len(resp.StdoutLines), resp.StdoutLines)
	}
	if resp.Stdout != "line1\nline2\nline3" {
		t.Errorf("Expected 'line1\\nline2\\nline3', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ScriptStderr(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-stderr.sh")
	remoteExec(t, client, `cat > /tmp/test-stderr.sh << 'EOF'
#!/bin/bash
echo "stdout message"
echo "stderr message" >&2
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-stderr.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-stderr.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-stderr.sh"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "stdout message" {
		t.Errorf("Expected 'stdout message', got: %q", resp.Stdout)
	}
	if resp.Stderr != "stderr message" {
		t.Errorf("Expected 'stderr message', got: %q", resp.Stderr)
	}
}

func TestPlaybook_ScriptReturnCode(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	testCases := []struct {
		name     string
		script   string
		expected int
	}{
		{"exit0", "#!/bin/bash\nexit 0", 0},
		{"exit1", "#!/bin/bash\nexit 1", 1},
		{"exit42", "#!/bin/bash\nexit 42", 42},
		{"exit127", "#!/bin/bash\nexit 127", 127},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scriptName := "/tmp/test-rc-" + tc.name + ".sh"
			remoteExec(t, client, "rm -f "+scriptName)
			remoteExec(t, client, "cat > "+scriptName+" << 'EOF'\n"+tc.script+"\nEOF")
			remoteExec(t, client, "chmod +x "+scriptName)
			defer remoteExec(t, client, "rm -f "+scriptName)

			resp := getScriptResult(t, client, `{"script_path":"`+scriptName+`"}`)

			if resp.RC != tc.expected {
				t.Errorf("Expected rc=%d, got: %d", tc.expected, resp.RC)
			}
			if tc.expected == 0 && resp.Failed {
				t.Error("Expected success for exit 0")
			}
			if tc.expected != 0 && !resp.Failed {
				t.Errorf("Expected failure for exit %d", tc.expected)
			}
		})
	}
}

func TestPlaybook_ScriptWithArgumentsDirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-args.sh")
	remoteExec(t, client, `cat > /tmp/test-args.sh << 'EOF'
#!/bin/bash
echo "args: $@"
echo "arg1: $1"
echo "arg2: $2"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-args.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-args.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-args.sh","args":"hello world"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "arg1: hello") {
		t.Errorf("Expected arg1: hello, got: %q", resp.Stdout)
	}
	if !strings.Contains(resp.Stdout, "arg2: world") {
		t.Errorf("Expected arg2: world, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ScriptCreatesDirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/test-creates-marker")
	defer remoteExec(t, client, "rm -f /tmp/test-creates-marker")

	remoteExec(t, client, "rm -f /tmp/test-creates-direct.sh")
	remoteExec(t, client, `cat > /tmp/test-creates-direct.sh << 'EOF'
#!/bin/bash
echo "should not run"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-creates-direct.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-creates-direct.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-creates-direct.sh","creates":"/tmp/test-creates-marker"}`)

	if resp.Failed {
		t.Fatalf("Expected success (skip), got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when creates file exists")
	}
	if !resp.Skipped {
		t.Error("Expected skipped=true when creates file exists")
	}
	if !strings.Contains(resp.Msg, "skipped") {
		t.Errorf("Expected skip message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ScriptRemovesDirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-removes-marker")

	remoteExec(t, client, "rm -f /tmp/test-removes-direct.sh")
	remoteExec(t, client, `cat > /tmp/test-removes-direct.sh << 'EOF'
#!/bin/bash
echo "should not run"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-removes-direct.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-removes-direct.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-removes-direct.sh","removes":"/tmp/test-removes-marker"}`)

	if resp.Failed {
		t.Fatalf("Expected success (skip), got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when removes file does not exist")
	}
	if !resp.Skipped {
		t.Error("Expected skipped=true when removes file does not exist")
	}
	if !strings.Contains(resp.Msg, "skipped") {
		t.Errorf("Expected skip message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ScriptChdirDirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-chdir.sh")
	remoteExec(t, client, `cat > /tmp/test-chdir.sh << 'EOF'
#!/bin/bash
pwd
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-chdir.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-chdir.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-chdir.sh","chdir":"/var"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "/var" {
		t.Errorf("Expected stdout=/var, got: %q", resp.Stdout)
	}
}

func TestPlaybook_ScriptChdirNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-chdir-fail.sh")
	remoteExec(t, client, `cat > /tmp/test-chdir-fail.sh << 'EOF'
#!/bin/bash
pwd
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-chdir-fail.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-chdir-fail.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-chdir-fail.sh","chdir":"/nonexistent/directory"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-existent chdir directory")
	}
	if !strings.Contains(resp.Msg, "Unable to change directory") {
		t.Errorf("Expected directory error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ScriptExecutableDirect(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-exec.py")
	remoteExec(t, client, `cat > /tmp/test-exec.py << 'EOF'
import sys
print("Python works")
EOF`)
	defer remoteExec(t, client, "rm -f /tmp/test-exec.py")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-exec.py","executable":"/usr/bin/python3"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.Stdout != "Python works" {
		t.Errorf("Expected 'Python works', got: %q", resp.Stdout)
	}
}

func TestPlaybook_ScriptNotFound(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getScriptResult(t, client, `{"script_path":"/tmp/nonexistent-script-xyz.sh"}`)

	if !resp.Failed {
		t.Error("Expected failure for non-existent script")
	}
	if !strings.Contains(resp.Msg, "does not exist") {
		t.Errorf("Expected 'does not exist' error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ScriptNoScriptPath(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getScriptResult(t, client, `{}`)

	if !resp.Failed {
		t.Error("Expected failure when no script path specified")
	}
	if !strings.Contains(resp.Msg, "no script path") {
		t.Errorf("Expected 'no script path' error message, got: %s", resp.Msg)
	}
}

func TestPlaybook_ScriptTimings(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-timing.sh")
	remoteExec(t, client, `cat > /tmp/test-timing.sh << 'EOF'
#!/bin/bash
echo "done"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-timing.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-timing.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-timing.sh"}`)

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

func TestPlaybook_ScriptChangedAlwaysTrue(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-changed.sh")
	remoteExec(t, client, `cat > /tmp/test-changed.sh << 'EOF'
#!/bin/bash
echo "test"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-changed.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-changed.sh")

	for i := 0; i < 3; i++ {
		resp := getScriptResult(t, client, `{"script_path":"/tmp/test-changed.sh"}`)
		if !resp.Changed {
			t.Errorf("Run %d: Expected changed=true for script execution", i+1)
		}
	}
}

func TestPlaybook_ScriptEmptyOutput(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-empty.sh")
	remoteExec(t, client, `cat > /tmp/test-empty.sh << 'EOF'
#!/bin/bash
exit 0
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-empty.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-empty.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-empty.sh"}`)

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

func TestPlaybook_ScriptWithWildcardCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/wildcard-script-*.txt")
	remoteExec(t, client, "touch /tmp/wildcard-script-abc.txt")
	defer remoteExec(t, client, "rm -f /tmp/wildcard-script-*.txt")

	remoteExec(t, client, "rm -f /tmp/test-wildcard.sh")
	remoteExec(t, client, `cat > /tmp/test-wildcard.sh << 'EOF'
#!/bin/bash
echo "should not run"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-wildcard.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-wildcard.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-wildcard.sh","creates":"/tmp/wildcard-script-*.txt"}`)

	if resp.Failed {
		t.Fatalf("Expected success (skip), got failed: %s", resp.Msg)
	}
	if resp.Changed {
		t.Error("Expected changed=false when wildcard creates pattern matches")
	}
}

func TestPlaybook_ScriptMultipleScripts(t *testing.T) {
	scriptPath1 := createTestScript(t, "first.sh", `#!/bin/bash
echo "first"
`)
	scriptPath2 := createTestScript(t, "second.sh", `#!/bin/bash
echo "second"
`)
	scriptPath3 := createTestScript(t, "third.sh", `#!/bin/bash
echo "third"
`)

	playbook := playbookHeader + `
  - name: Run first script
    script:
      cmd: ` + scriptPath1 + `

  - name: Run second script
    script:
      cmd: ` + scriptPath2 + `

  - name: Run third script
    script:
      cmd: ` + scriptPath3 + `
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

func TestPlaybook_ScriptCreatesFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/created-by-script.txt")
	defer remoteExec(t, client, "rm -f /tmp/created-by-script.txt")

	scriptPath := createTestScript(t, "creator.sh", `#!/bin/bash
echo "Hello from script" > /tmp/created-by-script.txt
echo "File created"
`)

	playbook := playbookHeader + `
  - name: Run script that creates file
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	if !remoteFileExists(t, client, "/tmp/created-by-script.txt") {
		t.Error("Expected file to be created by script")
	}

	content := remoteFileContent(t, client, "/tmp/created-by-script.txt")
	if !strings.Contains(content, "Hello from script") {
		t.Errorf("Expected file content from script, got: %q", content)
	}
}

func TestPlaybook_ScriptReadRemoteFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "echo 'test data' > /tmp/remote-data.txt")
	defer remoteExec(t, client, "rm -f /tmp/remote-data.txt")

	scriptPath := createTestScript(t, "reader.sh", `#!/bin/bash
cat /tmp/remote-data.txt
`)

	playbook := playbookHeader + `
  - name: Run script that reads remote file
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptWithSpacesInPath(t *testing.T) {
	dir := t.TempDir()
	spacePath := filepath.Join(dir, "space path")
	if err := os.MkdirAll(spacePath, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	scriptPath := filepath.Join(spacePath, "test.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/bash
echo "Script with space in path"
`), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	playbook := playbookHeader + `
  - name: Run script with space in path
    script:
      cmd: '"` + scriptPath + `"'
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changes for script execution")
	}
}

func TestPlaybook_ScriptWithQuotedPath(t *testing.T) {
	dir := t.TempDir()
	spacePath := filepath.Join(dir, "another space path")
	if err := os.MkdirAll(spacePath, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	scriptPath := filepath.Join(spacePath, "test.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/bash
echo "Quoted path works"
`), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	playbook := playbookHeader + `
  - name: Run script with quoted path
    script:
      cmd: '"` + scriptPath + `"'
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
}

func TestPlaybook_ScriptBashFeatures(t *testing.T) {
	scriptPath := createTestScript(t, "bash_features.sh", `#!/bin/bash
arr=(one two three)
echo "${arr[1]}"
echo $((5 + 3))
`)

	playbook := playbookHeader + `
  - name: Run bash script with advanced features
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
}

func TestPlaybook_ScriptEnvironmentVariables(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "rm -f /tmp/test-env.sh")
	remoteExec(t, client, `cat > /tmp/test-env.sh << 'EOF'
#!/bin/bash
echo "HOME=$HOME"
echo "PATH=$PATH"
EOF`)
	remoteExec(t, client, "chmod +x /tmp/test-env.sh")
	defer remoteExec(t, client, "rm -f /tmp/test-env.sh")

	resp := getScriptResult(t, client, `{"script_path":"/tmp/test-env.sh"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Stdout, "HOME=") {
		t.Error("Expected HOME environment variable to be set")
	}
	if !strings.Contains(resp.Stdout, "PATH=") {
		t.Error("Expected PATH environment variable to be set")
	}
}

func TestPlaybook_ScriptWithComplexArgs(t *testing.T) {
	scriptPath := createTestScript(t, "complex_args.sh", `#!/bin/bash
echo "all args: $@"
for arg in "$@"; do
    echo "arg: $arg"
done
`)

	playbook := playbookHeader + `
  - name: Run script with complex arguments
    script:
      cmd: ` + scriptPath + ` arg1 "arg with spaces" arg3
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
}

func TestPlaybook_ScriptCleanupAfterExecution(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	scriptPath := createTestScript(t, "cleanup.sh", `#!/bin/bash
echo "test cleanup"
`)

	remoteExec(t, client, "rm -f /tmp/.dibra-script-*")

	playbook := playbookHeader + `
  - name: Run script
    script:
      cmd: ` + scriptPath + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}

	leftoverScripts := remoteExec(t, client, "ls /tmp/.dibra-script-* 2>/dev/null || true")
	if leftoverScripts != "" {
		t.Errorf("Expected cleanup of temp script files, found: %s", leftoverScripts)
	}
}

func TestPlaybook_ScriptCleanupOnFailure(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	scriptPath := createTestScript(t, "fail_cleanup.sh", `#!/bin/bash
exit 1
`)

	remoteExec(t, client, "rm -f /tmp/.dibra-script-*")

	playbook := playbookHeader + `
  - name: Run failing script
    script:
      cmd: ` + scriptPath + `
`
	runPlaybook(t, playbook)

	leftoverScripts := remoteExec(t, client, "ls /tmp/.dibra-script-* 2>/dev/null || true")
	if leftoverScripts != "" {
		t.Errorf("Expected cleanup of temp script files even on failure, found: %s", leftoverScripts)
	}
}
