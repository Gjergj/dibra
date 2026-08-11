package controller

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/agent"
	"github.com/gjergjiramku/dibra/internal/execution"
)

func TestRunLocalUsesStandaloneAgentAndIgnoresInventory(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the agent binary")
	}
	projectRoot := repositoryRoot(t)
	temporary := t.TempDir()
	agentPath := filepath.Join(temporary, "dibra-agent")
	build := exec.Command("go", "build", "-o", agentPath, "./cmd/agent")
	build.Dir = projectRoot
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(temporary, "go-cache"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agent: %v\n%s", err, output)
	}

	destination := filepath.Join(temporary, "written.txt")
	templateDestination := filepath.Join(temporary, "templated.txt")
	scriptDestination := filepath.Join(temporary, "script.txt")
	fetchDestination := filepath.Join(temporary, "fetched.txt")
	unarchiveDestination := filepath.Join(temporary, "unarchived")
	assetsDir := filepath.Join(temporary, "assets")
	if err := os.MkdirAll(assetsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unarchiveDestination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "source.txt"), []byte("staged-copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "message.j2"), []byte("template={{ message }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "write.sh"), []byte("#!/bin/sh\nprintf script-run > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLocalTestZIP(t, filepath.Join(assetsDir, "bundle.zip"), "inside.txt", "unarchived")

	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := "inventory: missing-inventory.yaml\n" +
		"hosts:\n  - name: remote\n    host: does-not-exist\n" +
		"vars:\n  message: local-value\n" +
		"tasks:\n" +
		"  - name: stage copy locally\n" +
		"    copy:\n" +
		"      src: assets/source.txt\n" +
		"      dest: " + destination + "\n"
	playbook += "  - name: render local template\n" +
		"    template:\n" +
		"      src: assets/message.j2\n" +
		"      dest: " + templateDestination + "\n" +
		"  - name: stage and run local script\n" +
		"    script:\n" +
		"      cmd: assets/write.sh " + scriptDestination + "\n" +
		"  - name: stage and extract local archive\n" +
		"    unarchive:\n" +
		"      src: assets/bundle.zip\n" +
		"      dest: " + unarchiveDestination + "\n" +
		"  - name: fetch local file\n" +
		"    fetch:\n" +
		"      src: " + destination + "\n" +
		"      dest: " + fetchDestination + "\n" +
		"      flat: true\n"
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), RunOptions{
		ConfigPath:     playbookPath,
		Version:        "dev",
		AgentMode:      agent.ModePath,
		Local:          true,
		LocalAgentPath: agentPath,
		WorkingDir:     temporary,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Failed {
		t.Fatalf("Run() result = %#v", result)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "staged-copy" {
		t.Fatalf("destination content = %q", data)
	}
	for path, expected := range map[string]string{
		templateDestination: "template=local-value",
		scriptDestination:   "script-run",
		fetchDestination:    "staged-copy",
		filepath.Join(unarchiveDestination, "inside.txt"): "unarchived",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(data) != expected {
			t.Fatalf("%s content = %q, want %q", path, data, expected)
		}
	}
}

func TestLocalClientExecutesAgentProtocol(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	agentPath := filepath.Join(temporary, "agent")
	if err := os.WriteFile(agentPath, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"changed\":false,\"ping\":\"pong\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := NewLocalClient(context.Background())
	output, err := client.ExecuteAgent(agentPath, []byte(`{"module":"ping","args":{}}`))
	if err != nil {
		t.Fatalf("ExecuteAgent() error = %v", err)
	}
	if !strings.Contains(string(output), `"ping":"pong"`) {
		t.Fatalf("ExecuteAgent() output = %q", output)
	}
}

func TestRunCarriesResolvedCheckAndDiffStateAndWarnsForComposeAlias(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	capturedPath := filepath.Join(temporary, "requests.jsonl")
	agentPath := filepath.Join(temporary, "agent")
	agentScript := fmt.Sprintf("#!/bin/sh\nrequest=$(cat)\nprintf '%%s\\n' \"$request\" >> %q\nprintf '%%s\\n' '{\"changed\":false,\"failed\":false}'\n", capturedPath)
	if err := os.WriteFile(agentPath, []byte(agentScript), 0o755); err != nil {
		t.Fatal(err)
	}

	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := `tasks:
  - name: read-only module inherits global state
    docker_container_info:
      name: web
  - name: deprecated alias overrides both modes
    check_mode: false
    diff: true
    docker_compose:
      project_src: /tmp
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	result, err := Run(context.Background(), RunOptions{
		ConfigPath:     playbookPath,
		Local:          true,
		LocalAgentPath: agentPath,
		WorkingDir:     temporary,
		CheckMode:      true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed {
		t.Fatalf("Run() result = %#v", result)
	}
	if !strings.Contains(stdout.String(), `module alias "docker_compose" is deprecated`) {
		t.Fatalf("Run() output does not contain compose deprecation warning:\n%s", stdout.String())
	}

	data, err := os.ReadFile(capturedPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("captured %d requests, want 2: %s", len(lines), data)
	}
	want := []struct {
		module string
		state  execution.State
	}{
		{module: "community.docker.docker_container_info", state: execution.State{CheckMode: true}},
		{module: "community.docker.docker_compose_v2", state: execution.State{DiffMode: true}},
	}
	for index, line := range lines {
		var request execution.ModuleRequest[json.RawMessage]
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatalf("decode request %d: %v (%s)", index, err, line)
		}
		if request.Module != want[index].module || request.State != want[index].state {
			t.Errorf("request %d = module %q state %#v, want module %q state %#v", index, request.Module, request.State, want[index].module, want[index].state)
		}
	}
}

func TestRunRedactsRegisteredDockerArgumentsResultsAndDiffOutput(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	capturedPath := filepath.Join(temporary, "request.json")
	agentPath := filepath.Join(temporary, "agent")
	agentScript := fmt.Sprintf(`#!/bin/sh
request=$(cat)
printf '%%s\n' "$request" > %q
printf '%%s\n' '{"token":"registry-token-value","msg":"registry-password-value private-key-value registry-token-value","stderr":"registry-token-value","registry":"registry.example.test","diff":{"registry":{"before":"old","after":"new"}}}'
`, capturedPath)
	if err := os.WriteFile(agentPath, []byte(agentScript), 0o755); err != nil {
		t.Fatal(err)
	}

	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := `tasks:
  - name: sensitive registry login
    docker_login:
      username: deploy
      password: registry-password-value
      client_key: private-key-value
      registry: registry.example.test
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	result, err := Run(context.Background(), RunOptions{
		ConfigPath:     playbookPath,
		Local:          true,
		LocalAgentPath: agentPath,
		WorkingDir:     temporary,
		Verbose:        true,
		DiffMode:       true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed {
		t.Fatalf("Run() result = %#v\n%s", result, stdout.String())
	}
	output := stdout.String()
	for _, secret := range []string{"registry-password-value", "private-key-value", "registry-token-value"} {
		if strings.Contains(output, secret) {
			t.Fatalf("controller output leaked %q:\n%s", secret, output)
		}
	}
	for _, want := range []string{execution.RedactedValue, "Diff:", `"before"`, `"after"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("controller output does not contain %q:\n%s", want, output)
		}
	}

	captured, err := os.ReadFile(capturedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"registry-password-value", "private-key-value"} {
		if !strings.Contains(string(captured), value) {
			t.Fatalf("agent request lost executable value %q: %s", value, captured)
		}
	}
}

func TestRunSuppressesMalformedRegisteredModuleOutput(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	agentPath := filepath.Join(temporary, "agent")
	if err := os.WriteFile(agentPath, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' 'invalid registry-token-from-malformed-result'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := `tasks:
  - name: malformed registry login response
    docker_login:
      username: deploy
      password: registry-password-value
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	result, err := Run(context.Background(), RunOptions{
		ConfigPath:     playbookPath,
		Local:          true,
		LocalAgentPath: agentPath,
		WorkingDir:     temporary,
		Verbose:        true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Failed {
		t.Fatalf("Run() result = %#v, want failure", result)
	}
	output := stdout.String()
	for _, secret := range []string{"registry-password-value", "registry-token-from-malformed-result"} {
		if strings.Contains(output, secret) {
			t.Fatalf("controller output leaked %q:\n%s", secret, output)
		}
	}
	if !strings.Contains(output, "Raw output: "+execution.RedactedValue) {
		t.Fatalf("malformed output was not suppressed:\n%s", output)
	}
}

func TestRunCheckModeSkipsLegacyModuleBeforeControllerSideEffects(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := `tasks:
  - name: copy a missing source
    copy:
      src: does-not-exist
      dest: /tmp/must-not-be-written
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	result, err := Run(context.Background(), RunOptions{
		ConfigPath:     playbookPath,
		Local:          true,
		LocalAgentPath: filepath.Join(temporary, "agent-that-must-not-run"),
		WorkingDir:     temporary,
		CheckMode:      true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed {
		t.Fatalf("Run() result = %#v", result)
	}
	if !strings.Contains(stdout.String(), "SKIPPED") || !strings.Contains(stdout.String(), "does not yet implement check mode") {
		t.Fatalf("Run() output does not report a safe check-mode skip:\n%s", stdout.String())
	}
}

func TestRunLocalRebootGuardsAndResult(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	agentPath := filepath.Join(temporary, "agent")
	if err := os.WriteFile(agentPath, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"changed\":true,\"failed\":false,\"rebooted\":true,\"msg\":\"test reboot\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("successful final reboot", func(t *testing.T) {
		playbookPath := filepath.Join(temporary, "reboot.yaml")
		if err := os.WriteFile(playbookPath, []byte("tasks:\n  - name: final reboot\n    reboot:\n      reboot_command: /bin/true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := Run(context.Background(), RunOptions{
			ConfigPath: playbookPath, Local: true, LocalAgentPath: agentPath,
			WorkingDir: temporary, AllowReboot: true, Stdout: &bytes.Buffer{},
		})
		if err != nil || result.Failed || !result.RebootInitiated {
			t.Fatalf("Run() = %#v, %v", result, err)
		}
	})

	t.Run("skipped final reboot", func(t *testing.T) {
		playbookPath := filepath.Join(temporary, "skipped.yaml")
		if err := os.WriteFile(playbookPath, []byte("tasks:\n  - name: skipped reboot\n    when: false\n    reboot:\n      reboot_command: /bin/true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := Run(context.Background(), RunOptions{
			ConfigPath: playbookPath, Local: true, LocalAgentPath: agentPath,
			WorkingDir: temporary, AllowReboot: true, Stdout: &bytes.Buffer{},
		})
		if err != nil || result.Failed || result.RebootInitiated {
			t.Fatalf("Run() = %#v, %v", result, err)
		}
	})

	t.Run("dynamic reboot before final task", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(temporary, "included.yaml"), []byte("- name: included reboot\n  reboot:\n    reboot_command: /bin/true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		playbookPath := filepath.Join(temporary, "dynamic.yaml")
		if err := os.WriteFile(playbookPath, []byte("tasks:\n  - name: dynamic include\n    include_tasks: included.yaml\n  - name: trailing ping\n    ping:\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := Run(context.Background(), RunOptions{
			ConfigPath: playbookPath, Local: true, LocalAgentPath: agentPath,
			WorkingDir: temporary, AllowReboot: true, Stdout: &bytes.Buffer{},
		})
		if err != nil || !result.Failed || result.RebootInitiated {
			t.Fatalf("Run() = %#v, %v", result, err)
		}
	})
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func writeLocalTestZIP(t *testing.T, destination, name, contents string) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
