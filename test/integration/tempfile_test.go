//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type TempfileResponse struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Path    string `json:"path,omitempty"`
	State   string `json:"state,omitempty"`
}

func getTempfileResult(t *testing.T, client interface {
	Run(string) (string, string, error)
}, args string) TempfileResponse {
	t.Helper()
	cmd := `echo '{"module":"tempfile","args":` + args + `}' | /tmp/.dibra-agent`
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Fatalf("Agent execution failed: %v, stderr: %s", err, stderr)
	}

	var resp TempfileResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v, output: %s", err, stdout)
	}
	return resp
}

func TestPlaybook_TempfileBasicFile(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create temp file with defaults
    tempfile:
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changed=true for temp file creation")
	}
}

func TestPlaybook_TempfileBasicDirectory(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create temp directory with defaults
    tempfile:
      state: directory
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changed=true for temp directory creation")
	}
}

func TestPlaybook_TempfileDefaultFileResponse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for temp file creation")
	}
	if resp.Path == "" {
		t.Error("Expected path to be set")
	}
	if resp.State != "file" {
		t.Errorf("Expected state=file, got: %s", resp.State)
	}
	if !strings.Contains(resp.Path, "ansible.") {
		t.Errorf("Expected path to contain default prefix 'ansible.', got: %s", resp.Path)
	}

	if !remoteIsFile(t, client, resp.Path) {
		t.Errorf("Temp file does not exist: %s", resp.Path)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}

func TestPlaybook_TempfileDefaultDirectoryResponse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"state":"directory"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !resp.Changed {
		t.Error("Expected changed=true for temp directory creation")
	}
	if resp.Path == "" {
		t.Error("Expected path to be set")
	}
	if resp.State != "directory" {
		t.Errorf("Expected state=directory, got: %s", resp.State)
	}
	if !strings.Contains(resp.Path, "ansible.") {
		t.Errorf("Expected path to contain default prefix 'ansible.', got: %s", resp.Path)
	}

	if !remoteDirExists(t, client, resp.Path) {
		t.Errorf("Temp directory does not exist: %s", resp.Path)
	}

	remoteExec(t, client, "rm -rf "+resp.Path)
}

func TestPlaybook_TempfileCustomPrefix(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"prefix":"hello."}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Path, "hello.") {
		t.Errorf("Expected path to contain prefix 'hello.', got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}

func TestPlaybook_TempfileCustomSuffix(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"suffix":".goodbye"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.HasSuffix(resp.Path, ".goodbye") {
		t.Errorf("Expected path to end with '.goodbye', got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}

func TestPlaybook_TempfileCustomPrefixAndSuffix(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"prefix":"hello.","suffix":".goodbye"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.Contains(resp.Path, "hello.") {
		t.Errorf("Expected path to contain prefix 'hello.', got: %s", resp.Path)
	}
	if !strings.HasSuffix(resp.Path, ".goodbye") {
		t.Errorf("Expected path to end with '.goodbye', got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}

func TestPlaybook_TempfileCustomPath(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "mkdir -p /tmp/custom-temp-dir")

	resp := getTempfileResult(t, client, `{"path":"/tmp/custom-temp-dir"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.HasPrefix(resp.Path, "/tmp/custom-temp-dir/") {
		t.Errorf("Expected path to start with '/tmp/custom-temp-dir/', got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -rf /tmp/custom-temp-dir")
}

func TestPlaybook_TempfileCustomPathWithPrefixSuffix(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "mkdir -p /tmp/custom-temp-dir2")

	resp := getTempfileResult(t, client, `{"path":"/tmp/custom-temp-dir2","prefix":"myprefix.","suffix":".mysuffix"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if !strings.HasPrefix(resp.Path, "/tmp/custom-temp-dir2/") {
		t.Errorf("Expected path to start with '/tmp/custom-temp-dir2/', got: %s", resp.Path)
	}
	if !strings.Contains(resp.Path, "myprefix.") {
		t.Errorf("Expected path to contain 'myprefix.', got: %s", resp.Path)
	}
	if !strings.HasSuffix(resp.Path, ".mysuffix") {
		t.Errorf("Expected path to end with '.mysuffix', got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -rf /tmp/custom-temp-dir2")
}

func TestPlaybook_TempfileDirectoryWithOptions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "mkdir -p /tmp/custom-temp-dir3")

	resp := getTempfileResult(t, client, `{"path":"/tmp/custom-temp-dir3","prefix":"dirprefix.","suffix":".dirsuffix","state":"directory"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if resp.State != "directory" {
		t.Errorf("Expected state=directory, got: %s", resp.State)
	}
	if !strings.HasPrefix(resp.Path, "/tmp/custom-temp-dir3/") {
		t.Errorf("Expected path to start with '/tmp/custom-temp-dir3/', got: %s", resp.Path)
	}
	if !strings.Contains(resp.Path, "dirprefix.") {
		t.Errorf("Expected path to contain 'dirprefix.', got: %s", resp.Path)
	}
	if !strings.HasSuffix(resp.Path, ".dirsuffix") {
		t.Errorf("Expected path to end with '.dirsuffix', got: %s", resp.Path)
	}
	if !remoteDirExists(t, client, resp.Path) {
		t.Errorf("Temp directory does not exist: %s", resp.Path)
	}

	remoteExec(t, client, "rm -rf /tmp/custom-temp-dir3")
}

func TestPlaybook_TempfileNonExistentPath(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"path":"/tmp/does_not_exist_12345"}`)

	if !resp.Failed {
		t.Error("Expected failure when path does not exist")
		remoteExec(t, client, "rm -rf "+resp.Path)
	}
	if !strings.Contains(resp.Msg, "does not exist") {
		t.Errorf("Expected error message about non-existent path, got: %s", resp.Msg)
	}
}

func TestPlaybook_TempfileDirectoryNonExistentPath(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"path":"/tmp/does_not_exist_67890","state":"directory"}`)

	if !resp.Failed {
		t.Error("Expected failure when path does not exist")
		remoteExec(t, client, "rm -rf "+resp.Path)
	}
	if !strings.Contains(resp.Msg, "does not exist") {
		t.Errorf("Expected error message about non-existent path, got: %s", resp.Msg)
	}
}

func TestPlaybook_TempfileInvalidState(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"state":"invalid"}`)

	if !resp.Failed {
		t.Error("Expected failure for invalid state")
	}
	if !strings.Contains(resp.Msg, "state must be") {
		t.Errorf("Expected error message about invalid state, got: %s", resp.Msg)
	}
}

func TestPlaybook_TempfilePathIsFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	remoteExec(t, client, "touch /tmp/temp-test-file")

	resp := getTempfileResult(t, client, `{"path":"/tmp/temp-test-file"}`)

	if !resp.Failed {
		t.Error("Expected failure when path is a file, not a directory")
		remoteExec(t, client, "rm -f "+resp.Path)
	}
	if !strings.Contains(resp.Msg, "not a directory") {
		t.Errorf("Expected error message about path not being a directory, got: %s", resp.Msg)
	}

	remoteExec(t, client, "rm -f /tmp/temp-test-file")
}

func TestPlaybook_TempfileAlwaysChanged(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp1 := getTempfileResult(t, client, `{}`)
	resp2 := getTempfileResult(t, client, `{}`)

	if !resp1.Changed || !resp2.Changed {
		t.Error("tempfile should always report changed=true")
	}
	if resp1.Path == resp2.Path {
		t.Error("Each tempfile call should create a unique path")
	}

	remoteExec(t, client, "rm -f "+resp1.Path)
	remoteExec(t, client, "rm -f "+resp2.Path)
}

func TestPlaybook_TempfileUniquePaths(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	paths := make(map[string]bool)
	var createdPaths []string

	for i := 0; i < 5; i++ {
		resp := getTempfileResult(t, client, `{}`)
		if resp.Failed {
			t.Fatalf("Run %d failed: %s", i+1, resp.Msg)
		}
		if paths[resp.Path] {
			t.Errorf("Duplicate path created: %s", resp.Path)
		}
		paths[resp.Path] = true
		createdPaths = append(createdPaths, resp.Path)
	}

	for _, p := range createdPaths {
		remoteExec(t, client, "rm -f "+p)
	}
}

func TestPlaybook_TempfileEmptyPrefix(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"prefix":""}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}
	if strings.Contains(resp.Path, "ansible.") {
		t.Errorf("Expected path to NOT contain default prefix when empty prefix specified, got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}

func TestPlaybook_TempfileFilePermissions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	mode := remoteFileMode(t, client, resp.Path)
	if mode != "600" {
		t.Errorf("Expected restrictive permissions (600), got: %s", mode)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}

func TestPlaybook_TempfileDirectoryPermissions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{"state":"directory"}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	mode := remoteFileMode(t, client, resp.Path)
	if mode != "700" {
		t.Errorf("Expected restrictive permissions (700), got: %s", mode)
	}

	remoteExec(t, client, "rm -rf "+resp.Path)
}

func TestPlaybook_TempfileFileWithPlaybook(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create temp file with prefix and suffix
    tempfile:
      prefix: myapp.
      suffix: .tmp
      state: file
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changed=true for temp file creation")
	}
}

func TestPlaybook_TempfileDirectoryWithPlaybook(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create temp directory with prefix and suffix
    tempfile:
      prefix: myapp.
      suffix: .d
      state: directory
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changed=true for temp directory creation")
	}
}

func TestPlaybook_TempfileWithCustomPathPlaybook(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "mkdir -p /tmp/playbook-temp-test")

	playbook := playbookHeader + `
  - name: Create temp file in custom path
    tempfile:
      path: /tmp/playbook-temp-test
      prefix: test.
      suffix: .cfg
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Fatalf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected changed=true for temp file creation")
	}

	remoteExec(t, client, "rm -rf /tmp/playbook-temp-test")
}

func TestPlaybook_TempfileFailOnNonExistentPathPlaybook(t *testing.T) {
	playbook := playbookHeader + `
  - name: Create temp file in non-existent path
    tempfile:
      path: /tmp/nonexistent_path_test_12345
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Fatalf("Expected failure, got: %s", output)
	}
}

func TestPlaybook_TempfileDefaultsToSystemTemp(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	runPlaybook(t, playbookHeader+`
  - name: Upload agent
    ping:
`)

	resp := getTempfileResult(t, client, `{}`)

	if resp.Failed {
		t.Fatalf("Expected success, got failed: %s", resp.Msg)
	}

	if !strings.HasPrefix(resp.Path, "/tmp/") {
		t.Errorf("Expected path to be in system temp (/tmp/), got: %s", resp.Path)
	}

	remoteExec(t, client, "rm -f "+resp.Path)
}
