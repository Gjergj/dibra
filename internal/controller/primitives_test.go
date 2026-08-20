package controller

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/config"
)

func TestControllerPrimitivesSetFactsAssertAndPause(t *testing.T) {
	contextVars := map[string]interface{}{
		"base":   21,
		"suffix": "answer",
		"payload": map[string]interface{}{
			"enabled": true,
		},
	}
	runtimeVars := map[string]interface{}{}

	setFact := config.Task{SetFact: &config.SetFactParams{Facts: map[string]interface{}{
		"fact_{{ suffix }}": "{{ base * 2 }}",
		"copied":            "{{ payload }}",
	}}}
	result, handled := executeControllerPrimitive(setFact, contextVars, runtimeVars, t.TempDir(), false, nil)
	if !handled || boolResultField(result, "failed") {
		t.Fatalf("set_fact result = %#v, handled = %t", result, handled)
	}
	if runtimeVars["fact_answer"] != 42 {
		t.Fatalf("fact_answer = %#v", runtimeVars["fact_answer"])
	}
	if !reflect.DeepEqual(runtimeVars["copied"], contextVars["payload"]) {
		t.Fatalf("copied = %#v", runtimeVars["copied"])
	}

	assertContext := map[string]interface{}{}
	for key, value := range contextVars {
		assertContext[key] = value
	}
	for key, value := range runtimeVars {
		assertContext[key] = value
	}
	assertion := config.Task{Assert: &config.AssertParams{
		That:       config.When{"fact_answer == 42", "copied.enabled"},
		SuccessMsg: "facts are valid",
	}}
	result, handled = executeControllerPrimitive(assertion, assertContext, runtimeVars, t.TempDir(), false, nil)
	if !handled || boolResultField(result, "failed") || result["msg"] != "facts are valid" {
		t.Fatalf("assert result = %#v, handled = %t", result, handled)
	}

	var slept time.Duration
	pause := config.Task{Pause: &config.PauseParams{Seconds: 0, Echo: false}}
	result, handled = executeControllerPrimitive(pause, assertContext, runtimeVars, t.TempDir(), false, func(duration time.Duration) {
		slept = duration
	})
	if !handled || boolResultField(result, "failed") {
		t.Fatalf("pause result = %#v, handled = %t", result, handled)
	}
	if slept != time.Second {
		t.Fatalf("pause duration = %v, want 1s", slept)
	}
	if result["echo"] != false {
		t.Fatalf("pause echo = %#v", result["echo"])
	}
}

func TestControllerIncludeVarsFileDirectoryAndMerge(t *testing.T) {
	root := t.TempDir()
	varsDir := filepath.Join(root, "vars")
	nestedDir := filepath.Join(varsDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(varsDir, "01-base.yml"), "application:\n  host: localhost\n  port: 80\norder: first\n")
	writeTestFile(t, filepath.Join(varsDir, "02-override.yaml"), "application:\n  port: 8080\norder: second\n")
	writeTestFile(t, filepath.Join(varsDir, "README"), "not vars\n")
	writeTestFile(t, filepath.Join(nestedDir, "03-nested.yml"), "nested: true\n")

	runtimeVars := map[string]interface{}{}
	contextVars := map[string]interface{}{
		"existing": map[string]interface{}{"left": 1},
	}
	fileResult := executeIncludeVars(
		&config.IncludeVarsParams{File: "vars/01-base.yml", Name: "single"},
		contextVars,
		runtimeVars,
		root,
	)
	if boolResultField(fileResult, "failed") {
		t.Fatalf("file include result = %#v", fileResult)
	}
	single, ok := runtimeVars["single"].(map[string]interface{})
	if !ok || single["order"] != "first" {
		t.Fatalf("single vars = %#v", runtimeVars["single"])
	}

	directoryResult := executeIncludeVars(
		&config.IncludeVarsParams{
			Dir:                     "vars",
			Depth:                   1,
			IgnoreUnknownExtensions: true,
			HashBehaviour:           "merge",
		},
		contextVars,
		runtimeVars,
		root,
	)
	if boolResultField(directoryResult, "failed") {
		t.Fatalf("directory include result = %#v", directoryResult)
	}
	if runtimeVars["order"] != "second" {
		t.Fatalf("directory order = %#v", runtimeVars["order"])
	}
	if _, exists := runtimeVars["nested"]; exists {
		t.Fatalf("depth=1 loaded nested vars: %#v", runtimeVars)
	}
	files, ok := directoryResult["ansible_included_var_files"].([]interface{})
	if !ok || len(files) != 2 {
		t.Fatalf("included files = %#v", directoryResult["ansible_included_var_files"])
	}
}

func TestRunControllerPrimitivesNeverInvokeAgent(t *testing.T) {
	temporary := t.TempDir()
	markerPath := filepath.Join(temporary, "agent-called")
	agentPath := writePrimitiveMarkerAgent(t, temporary, markerPath)
	writeTestFile(t, filepath.Join(temporary, "runtime-vars.yml"), "included_answer: 42\nnested:\n  enabled: true\n")
	playbookPath := filepath.Join(temporary, "playbook.yaml")
	playbook := `
vars:
  values: [alpha, omega]
tasks:
  - name: notify from controller debug
    debug:
      msg: "hello {{ inventory_hostname }}"
    changed_when: true
    notify: set handler fact
    register: debug_result
  - name: run controller handler
    meta: flush_handlers
  - name: set loop facts
    set_fact:
      selected: "{{ item }}"
      doubled: "{{ 21 * 2 }}"
    loop: "{{ values }}"
    register: fact_results
  - name: load runtime vars
    include_vars:
      file: runtime-vars.yml
      name: loaded
    register: include_result
  - name: non-interactive pause
    pause:
    register: pause_result
  - name: loop over noop
    meta: noop
    loop: [first, second]
    register: noop_results
  - name: validate runtime state
    assert:
      that:
        - debug_result.msg == "hello localhost"
        - handler_fact == "ready"
        - selected == "omega"
        - doubled == 42
        - loaded.included_answer == 42
        - loaded.nested.enabled
        - (fact_results.results | length) == 2
        - (include_result.ansible_included_var_files | length) == 1
        - pause_result.changed == false
        - (noop_results.results | length) == 2
      success_msg: controller state is valid
  - name: inspect a native value
    debug:
      var: loaded
handlers:
  - name: set handler fact
    set_fact:
      handler_fact: ready
`
	writeTestFile(t, playbookPath, playbook)

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
		t.Fatalf("Run() result = %#v\n%s", result, stdout.String())
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("controller primitive invoked agent; marker error = %v", err)
	}
	for _, expected := range []string{
		"hello localhost",
		"controller state is valid",
		"stdin is not interactive",
		"loaded:",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, stdout.String())
		}
	}
}

func TestRunControllerFailAndMetaEndActionsNeverInvokeAgent(t *testing.T) {
	tests := map[string]struct {
		tasks      string
		handlers   string
		wantFailed bool
	}{
		"fail honors when and register": {
			tasks: `
  - name: skipped failure
    fail:
      msg: must not fail
    when: false
    register: skipped_failure
  - name: validate skipped failure
    assert:
      that: skipped_failure.skipped
  - name: requested failure
    fail:
      msg: expected controller failure
`,
			wantFailed: true,
		},
		"end host": {
			tasks: `
  - name: queue a handler that must not run
    debug:
    changed_when: true
    notify: forbidden handler
  - name: stop this host
    meta: end_host
    register: ended
  - name: unreachable failure
    fail:
      msg: end_host did not stop execution
`,
			handlers: `
  - name: forbidden handler
    fail:
      msg: end_host ran pending handlers
`,
		},
		"end play": {
			tasks: `
  - name: queue a handler that must not run
    debug:
    changed_when: true
    notify: forbidden handler
  - name: stop this play
    meta: end_play
  - name: unreachable failure
    fail:
      msg: end_play did not stop execution
`,
			handlers: `
  - name: forbidden handler
    fail:
      msg: end_play ran pending handlers
`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			temporary := t.TempDir()
			markerPath := filepath.Join(temporary, "agent-called")
			agentPath := writePrimitiveMarkerAgent(t, temporary, markerPath)
			playbookPath := filepath.Join(temporary, "playbook.yaml")
			playbook := "tasks:\n" + test.tasks
			if test.handlers != "" {
				playbook += "handlers:\n" + test.handlers
			}
			writeTestFile(t, playbookPath, playbook)

			result, err := Run(context.Background(), RunOptions{
				ConfigPath:     playbookPath,
				Local:          true,
				LocalAgentPath: agentPath,
				WorkingDir:     temporary,
				CheckMode:      true,
				Stdout:         &bytes.Buffer{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Failed != test.wantFailed {
				t.Fatalf("Run() failed = %t, want %t", result.Failed, test.wantFailed)
			}
			if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
				t.Fatalf("controller primitive invoked agent; marker error = %v", err)
			}
		})
	}
}

func writePrimitiveMarkerAgent(t *testing.T, directory, markerPath string) string {
	t.Helper()
	path := filepath.Join(directory, "agent")
	script := "#!/bin/sh\n: > " + markerPath + "\nprintf '%s\\n' '{\"changed\":false,\"failed\":false}'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
