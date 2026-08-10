package controller

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/agent"
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
