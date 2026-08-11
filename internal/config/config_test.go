package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker_container_exec"
)

func TestLoad_StrictParsing_UnknownTopLevel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "playbook.yaml")
	_ = os.WriteFile(p, []byte(`
hosts: []
tasks: []
bogus_field: true
`), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for unknown top-level field, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestLoad_StrictParsing_UnknownHostField(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "playbook.yaml")
	_ = os.WriteFile(p, []byte(`
hosts:
  - name: test
    host: 1.2.3.4
    user: root
    unknown_field: oops
tasks: []
`), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for unknown host field, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestLoad_StrictParsing_UnknownTaskField(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "playbook.yaml")
	_ = os.WriteFile(p, []byte(`
hosts: []
tasks:
  - name: test
    not_a_module: true
`), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for unknown task field, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestLoad_StrictParsing_ValidPlaybook(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "playbook.yaml")
	_ = os.WriteFile(p, []byte(`
hosts:
  - name: test
    host: 1.2.3.4
    user: root
tasks:
  - name: ping test
    ping:
`), 0644)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("unexpected error for valid playbook: %v", err)
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(cfg.Tasks))
	}
	if cfg.Tasks[0].Ping == nil {
		t.Fatal("expected ping to be initialized")
	}
}

func TestLoad_RegisteredDockerModuleUsesTypedDecoder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yaml")
	data := []byte(`
hosts: []
tasks:
  - name: execute command
    docker_container_exec:
      container: web
      argv: [whoami]
      privileged: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	invocation := cfg.Tasks[0].Module
	if invocation == nil {
		t.Fatal("registered module was not decoded")
	}
	if invocation.CanonicalName != "community.docker.docker_container_exec" {
		t.Fatalf("canonical name = %q", invocation.CanonicalName)
	}
	request, ok := invocation.Arguments.(docker_container_exec.Request)
	if !ok {
		t.Fatalf("arguments type = %T", invocation.Arguments)
	}
	if request.Container != "web" || !request.Privileged {
		t.Fatalf("request = %#v", request)
	}
}

func TestLoad_RegisteredDockerModuleAcceptsFullyQualifiedName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yaml")
	data := []byte(`
hosts: []
tasks:
  - community.docker.docker_container_exec:
      container: web
      command: whoami
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Tasks[0].Module.CanonicalName; got != "community.docker.docker_container_exec" {
		t.Fatalf("canonical name = %q", got)
	}
}

func TestLoad_TaskCheckAndDiffOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yaml")
	data := []byte(`
hosts: []
tasks:
  - name: inspect without global check mode
    check_mode: false
    diff: true
    docker_container_info:
      name: web
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	task := cfg.Tasks[0]
	if task.CheckMode == nil || *task.CheckMode {
		t.Fatalf("check_mode = %#v", task.CheckMode)
	}
	if task.Diff == nil || !*task.Diff {
		t.Fatalf("diff = %#v", task.Diff)
	}
	if task.Module == nil {
		t.Fatal("registered module was not decoded")
	}
}

func TestLoad_RegisteredDockerModuleRejectsUnknownArgument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yaml")
	data := []byte(`
hosts: []
tasks:
  - docker_container_info:
      name: web
      container_nmae: typo
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `unknown field "container_nmae"`) {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoad_RejectsMultipleRegisteredDockerModules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yaml")
	data := []byte(`
hosts: []
tasks:
  - docker_container_info:
      name: web
    docker_image_info:
      name: nginx:latest
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "multiple registered modules") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoad_RejectsRegisteredAndLegacyModulesOnSameTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playbook.yaml")
	data := []byte(`
hosts: []
tasks:
  - ping:
    docker_container_info:
      name: web
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "multiple modules") {
		t.Fatalf("Load() error = %v", err)
	}
}
