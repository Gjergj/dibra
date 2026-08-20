package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadParsesHandlersNotifyListenChangedWhenAndMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playbook.yaml")
	data := []byte(`
force_handlers: true
tasks:
  - name: notify handlers
    ping:
    notify:
      - restart web
      - audit
    changed_when: false
  - name: flush now
    meta: flush_handlers
handlers:
  - name: restart service
    listen: restart web
    command:
      cmd: /bin/true
  - name: audit handler
    listen: [restart web, audit]
    ping:
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ForceHandlers || len(cfg.Tasks) != 2 || len(cfg.Handlers) != 2 {
		t.Fatalf("config = %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.Tasks[0].Notify, StringList{"restart web", "audit"}) {
		t.Fatalf("notify = %#v", cfg.Tasks[0].Notify)
	}
	if !reflect.DeepEqual(cfg.Tasks[0].ChangedWhen, When{false}) {
		t.Fatalf("changed_when = %#v", cfg.Tasks[0].ChangedWhen)
	}
	if cfg.Tasks[1].Meta == nil || cfg.Tasks[1].Meta.Action != "flush_handlers" {
		t.Fatalf("meta = %#v", cfg.Tasks[1].Meta)
	}
	if !reflect.DeepEqual(cfg.Handlers[1].Listen, StringList{"restart web", "audit"}) {
		t.Fatalf("listen = %#v", cfg.Handlers[1].Listen)
	}
}

func TestLoadRejectsInvalidHandlerControlValues(t *testing.T) {
	tests := map[string]string{
		"notify mapping": `
tasks:
  - name: bad notify
    ping:
    notify: {name: handler}
`,
		"listen non-string entry": `
tasks: []
handlers:
  - name: bad listen
    listen: [topic, {bad: value}]
    ping:
`,
		"unsupported meta": `
tasks:
  - name: unsupported meta
    meta: reset_connection
`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "playbook.yaml")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected parsing error")
			}
		})
	}
}

func TestExpandImportTasksSupportsHandlerLists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "handlers.yaml"), []byte(`
- name: imported handler
  command:
    cmd: echo imported
`), 0o600); err != nil {
		t.Fatal(err)
	}
	handlers, err := ExpandImportTasks([]Task{{
		Name:        "import handlers",
		ImportTasks: &ImportTasksParams{File: "handlers.yaml"},
	}}, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(handlers) != 1 || handlers[0].Name != "imported handler" || handlers[0].Command == nil {
		t.Fatalf("handlers = %#v", handlers)
	}
}

func TestLoadRejectsUnnamedHandlerAtExecutionBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(path, []byte(`
tasks: []
handlers:
  - ping:
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Handlers) != 1 || strings.TrimSpace(cfg.Handlers[0].Name) != "" {
		t.Fatalf("handlers = %#v", cfg.Handlers)
	}
}
