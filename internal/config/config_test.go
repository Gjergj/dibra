package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_StrictParsing_UnknownTopLevel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "playbook.yaml")
	os.WriteFile(p, []byte(`
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
	os.WriteFile(p, []byte(`
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
	os.WriteFile(p, []byte(`
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
	os.WriteFile(p, []byte(`
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
