package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadParsesControllerPrimitives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playbook.yaml")
	data := []byte(`
tasks:
  - name: default debug
    debug:
  - name: variable debug
    debug:
      var: application.port
      verbosity: 1
  - name: fail with a message
    fail:
      msg: "bad {{ value }}"
  - name: assert values
    assert:
      that:
        - value == 42
        - true
      fail_msg: invalid
      success_msg: valid
      quiet: true
  - name: set runtime values
    set_fact:
      answer: 42
      copied: "{{ value }}"
      cacheable: true
  - name: include free-form vars
    include_vars: vars/runtime.yml
  - name: include directory vars
    include_vars:
      dir: vars
      depth: 1
      files_matching: '\.ya?ml$'
      ignore_files: [ignored.yml]
      extensions: [yaml, yml]
      ignore_unknown_extensions: true
      hash_behaviour: merge
      name: loaded
  - name: timed pause
    pause:
      seconds: "{{ wait_seconds }}"
      prompt: wait
      echo: false
  - meta: noop
  - meta: end_host
  - meta: end_play
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 11 {
		t.Fatalf("task count = %d", len(cfg.Tasks))
	}
	if cfg.Tasks[0].Debug == nil || cfg.Tasks[0].Debug.MsgSet {
		t.Fatalf("default debug = %#v", cfg.Tasks[0].Debug)
	}
	if got := cfg.Tasks[1].Debug; got == nil || got.Var != "application.port" || got.Verbosity != 1 {
		t.Fatalf("variable debug = %#v", got)
	}
	if got := cfg.Tasks[2].Fail; got == nil || !got.MsgSet || got.Msg != "bad {{ value }}" {
		t.Fatalf("fail = %#v", got)
	}
	if got := cfg.Tasks[3].Assert; got == nil || !reflect.DeepEqual(got.That, When{"value == 42", true}) || !got.Quiet {
		t.Fatalf("assert = %#v", got)
	}
	if got := cfg.Tasks[4].SetFact; got == nil || !got.Cacheable || got.Facts["answer"] != 42 {
		t.Fatalf("set_fact = %#v", got)
	}
	if got := cfg.Tasks[5].IncludeVars; got == nil || got.File != "vars/runtime.yml" {
		t.Fatalf("free-form include_vars = %#v", got)
	}
	if got := cfg.Tasks[6].IncludeVars; got == nil || got.Dir != "vars" || got.Depth != 1 || got.Name != "loaded" {
		t.Fatalf("directory include_vars = %#v", got)
	}
	if got := cfg.Tasks[7].Pause; got == nil || got.Seconds != "{{ wait_seconds }}" || got.Echo != false {
		t.Fatalf("pause = %#v", got)
	}
	for index, action := range []string{"noop", "end_host", "end_play"} {
		if got := cfg.Tasks[index+8].Meta; got == nil || got.Action != action {
			t.Fatalf("meta task %d = %#v", index, got)
		}
	}
}

func TestLoadRejectsInvalidControllerPrimitiveArguments(t *testing.T) {
	tests := map[string]string{
		"debug unknown": `
tasks:
  - debug:
      message: typo
`,
		"assert aliases": `
tasks:
  - assert:
      that: true
      fail_msg: one
      msg: two
`,
		"set_fact scalar": `
tasks:
  - set_fact: answer=42
`,
		"include_vars unknown": `
tasks:
  - include_vars:
      path: vars.yml
`,
		"pause durations": `
tasks:
  - pause:
      minutes: 1
      seconds: 1
`,
	}
	for name, playbook := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "playbook.yaml")
			if err := os.WriteFile(path, []byte(playbook), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected parsing error")
			}
		})
	}
}

func TestLoadRejectsUnsafeMetaActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(path, []byte("tasks:\n  - meta: reset_connection\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported meta action") {
		t.Fatalf("Load() error = %v", err)
	}
}
