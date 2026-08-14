//go:build integration

package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_Register(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/register")

	t.Run("basic_shell_register", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-test.txt")

		playbook := filepath.Join(testdataDir, "playbook.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-test.txt")
		if strings.TrimSpace(content) != "hello world" {
			t.Errorf("expected 'hello world', got %q", content)
		}
	})

	t.Run("registered_failure_stops_remaining_tasks", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-fail.txt")

		playbook := filepath.Join(testdataDir, "playbook_error.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "FAILED") {
			t.Errorf("expected the registered task to fail, got: %s", output)
		}
		if !strings.Contains(output, "stopping remaining tasks") {
			t.Errorf("expected the host to stop after failure, got: %s", output)
		}
		if remoteFileExists(t, client, "/tmp/dibra-fail.txt") {
			t.Error("task after the registered failure unexpectedly ran")
		}
	})

	t.Run("register_overwrite", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-overwrite.txt")

		playbook := filepath.Join(testdataDir, "playbook_overwrite.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "invalid variable name") {
			t.Fatalf("unexpected validation error: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-overwrite.txt")
		if strings.TrimSpace(content) != "second" {
			t.Errorf("expected 'second' (overwritten), got %q", content)
		}
	})

	t.Run("register_command_module", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-command.txt")

		playbook := filepath.Join(testdataDir, "playbook_command.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-command.txt")
		if !strings.Contains(content, "stdout=hello world") {
			t.Errorf("expected stdout=hello world in content, got: %s", content)
		}
		if !strings.Contains(content, "rc=0") {
			t.Errorf("expected rc=0 in content, got: %s", content)
		}
		if !strings.Contains(content, "changed=true") {
			t.Errorf("expected changed=true in content, got: %s", content)
		}
	})

	t.Run("register_ping_module_specific_field", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-ping.txt")

		playbook := filepath.Join(testdataDir, "playbook_ping.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-ping.txt")
		if !strings.Contains(content, "ping=pong") {
			t.Errorf("expected ping=pong (module-specific field), got: %s", content)
		}
		if !strings.Contains(content, "changed=false") {
			t.Errorf("expected changed=false, got: %s", content)
		}
	})

	t.Run("register_stdout_lines_access", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-lines.txt")

		playbook := filepath.Join(testdataDir, "playbook_stdout_lines.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-lines.txt")
		if !strings.Contains(content, "first=line1") {
			t.Errorf("expected first=line1, got: %s", content)
		}
		if !strings.Contains(content, "second=line2") {
			t.Errorf("expected second=line2, got: %s", content)
		}
		if !strings.Contains(content, "third=line3") {
			t.Errorf("expected third=line3, got: %s", content)
		}
	})

	t.Run("register_chain_across_tasks", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-chain.txt")

		playbook := filepath.Join(testdataDir, "playbook_chain.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-chain.txt")
		if !strings.HasPrefix(content, "got=") {
			t.Errorf("expected 'got=<hostname>', got: %s", content)
		}
	})

	t.Run("register_file_module_specific_fields", func(t *testing.T) {
		remoteExec(t, client, "rm -rf /tmp/dibra-register-dir /tmp/dibra-register-file-mod.txt")

		playbook := filepath.Join(testdataDir, "playbook_file_module.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-file-mod.txt")
		if !strings.Contains(content, "path=/tmp/dibra-register-dir") {
			t.Errorf("expected path=/tmp/dibra-register-dir, got: %s", content)
		}
		if !strings.Contains(content, "state=directory") {
			t.Errorf("expected state=directory, got: %s", content)
		}
	})

	t.Run("register_copy_module", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-copy-src.txt /tmp/dibra-register-copy-result.txt")

		playbook := filepath.Join(testdataDir, "playbook_copy_module.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-copy-result.txt")
		if !strings.Contains(content, "changed=true") {
			t.Errorf("expected changed=true, got: %s", content)
		}
		if !strings.Contains(content, "failed=false") {
			t.Errorf("expected failed=false, got: %s", content)
		}
	})

	t.Run("register_multiple_modules", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-multi.txt")

		playbook := filepath.Join(testdataDir, "playbook_multi_module.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-multi.txt")
		if !strings.Contains(content, "shell=from_shell") {
			t.Errorf("expected shell=from_shell, got: %s", content)
		}
		if !strings.Contains(content, "cmd=from_command") {
			t.Errorf("expected cmd=from_command, got: %s", content)
		}
		if !strings.Contains(content, "ping=pong") {
			t.Errorf("expected ping=pong, got: %s", content)
		}
	})

	t.Run("register_idempotency", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-idemp.txt /tmp/dibra-register-idemp-result.txt")

		playbook := filepath.Join(testdataDir, "playbook_idempotent.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-idemp-result.txt")
		if !strings.Contains(content, "first_changed=true") {
			t.Errorf("expected first_changed=true, got: %s", content)
		}
		if !strings.Contains(content, "second_changed=false") {
			t.Errorf("expected second_changed=false (idempotent), got: %s", content)
		}
	})

	t.Run("register_tempfile_module", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-tempfile.txt")

		playbook := filepath.Join(testdataDir, "playbook_tempfile.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-tempfile.txt")
		if !strings.Contains(content, "path=") {
			t.Errorf("expected path= in content, got: %s", content)
		}
		if !strings.Contains(content, "dibra-reg-test.") {
			t.Errorf("expected dibra-reg-test. prefix in path, got: %s", content)
		}
		if !strings.Contains(content, ".tmp") {
			t.Errorf("expected .tmp suffix in path, got: %s", content)
		}
		if !strings.Contains(content, "changed=true") {
			t.Errorf("expected changed=true, got: %s", content)
		}
		if !strings.Contains(content, "state=file") {
			t.Errorf("expected state=file, got: %s", content)
		}
	})

	t.Run("register_used_in_template_expression", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-template-expr.txt")
		remoteExec(t, client, "rm -rf /tmp/dibra-register-*")

		playbook := filepath.Join(testdataDir, "playbook_template_expr.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-template-expr.txt")
		if !strings.HasPrefix(content, "/tmp/dibra-register-") {
			t.Errorf("expected path starting with /tmp/dibra-register-, got: %s", content)
		}
		kernelVersion := remoteExec(t, client, "uname -r")
		if !strings.Contains(content, kernelVersion) {
			t.Errorf("expected kernel version %q in path, got: %s", kernelVersion, content)
		}
	})

	t.Run("register_across_include_tasks", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-include-result.txt /tmp/dibra-register-include-combined.txt")

		playbook := filepath.Join(testdataDir, "playbook_include.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-include-result.txt")
		if !strings.Contains(content, "included=from_included") {
			t.Errorf("expected included=from_included, got: %s", content)
		}

		combined := remoteFileContent(t, client, "/tmp/dibra-register-include-combined.txt")
		if !strings.Contains(combined, "before=before_include") {
			t.Errorf("expected before=before_include, got: %s", combined)
		}
		if !strings.Contains(combined, "included=from_included") {
			t.Errorf("expected included=from_included in combined, got: %s", combined)
		}
	})

	t.Run("register_across_import_tasks", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-import-result.txt /tmp/dibra-register-import-combined.txt")

		playbook := filepath.Join(testdataDir, "playbook_import.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-import-result.txt")
		if !strings.Contains(content, "imported=from_imported") {
			t.Errorf("expected imported=from_imported, got: %s", content)
		}

		combined := remoteFileContent(t, client, "/tmp/dibra-register-import-combined.txt")
		if !strings.Contains(combined, "before=before_import") {
			t.Errorf("expected before=before_import, got: %s", combined)
		}
		if !strings.Contains(combined, "imported=from_imported") {
			t.Errorf("expected imported=from_imported in combined, got: %s", combined)
		}
	})

	t.Run("register_invalid_variable_name_numeric", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Register with invalid name
    ping:
    register: 200
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "invalid variable name") {
			t.Errorf("expected 'invalid variable name' error, got: %s", output)
		}
	})

	t.Run("register_invalid_variable_name_hyphen", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Register with hyphen
    ping:
    register: my-var
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "invalid variable name") {
			t.Errorf("expected 'invalid variable name' error for hyphenated name, got: %s", output)
		}
	})

	t.Run("register_invalid_variable_name_space", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Register with space
    ping:
    register: "my var"
`
		output := runPlaybook(t, playbook)
		if !strings.Contains(output, "invalid variable name") {
			t.Errorf("expected 'invalid variable name' error for space in name, got: %s", output)
		}
	})

	t.Run("register_valid_underscore_prefix", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Register with underscore prefix
    shell:
      cmd: echo underscore_test
    register: _my_var

  - name: Use underscore var
    copy:
      content: "{{ _my_var.stdout }}"
      dest: /tmp/dibra-register-underscore.txt
      mode: "0644"
`
		remoteExec(t, client, "rm -f /tmp/dibra-register-underscore.txt")
		output := runPlaybook(t, playbook)

		if strings.Contains(output, "invalid variable name") {
			t.Fatalf("underscore prefix should be valid, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-underscore.txt")
		if strings.TrimSpace(content) != "underscore_test" {
			t.Errorf("expected 'underscore_test', got %q", content)
		}
	})

	t.Run("register_without_register_no_side_effects", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Run command without register
    shell:
      cmd: echo no_register

  - name: Create marker file
    copy:
      content: "done"
      dest: /tmp/dibra-register-noside.txt
      mode: "0644"
`
		remoteExec(t, client, "rm -f /tmp/dibra-register-noside.txt")
		output := runPlaybook(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-noside.txt")
		if strings.TrimSpace(content) != "done" {
			t.Errorf("expected 'done', got %q", content)
		}
	})

	t.Run("register_rerun_idempotent", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-test.txt")

		playbook := filepath.Join(testdataDir, "playbook.yaml")

		output1 := runPlaybookFromFile(t, playbook)
		if strings.Contains(output1, "FAILED") {
			t.Fatalf("first run failed: %s", output1)
		}

		output2 := runPlaybookFromFile(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("second run failed: %s", output2)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-test.txt")
		if strings.TrimSpace(content) != "hello world" {
			t.Errorf("expected 'hello world' after rerun, got %q", content)
		}
	})

	t.Run("register_common_return_values", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-common.txt")

		playbook := playbookHeader + `
  - name: Run simple command
    shell:
      cmd: echo "test"
    register: common_result

  - name: Check common fields
    copy:
      content: "attempts={{ common_result.attempts }} retries={{ common_result.retries }} skipped={{ common_result.skipped }} failed={{ common_result.failed }}"
      dest: /tmp/dibra-register-common.txt
      mode: "0644"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-common.txt")
		if !strings.Contains(content, "attempts=1") {
			t.Errorf("expected attempts=1, got: %s", content)
		}
		if !strings.Contains(content, "retries=0") {
			t.Errorf("expected retries=0, got: %s", content)
		}
		if !strings.Contains(content, "skipped=false") {
			t.Errorf("expected skipped=false, got: %s", content)
		}
		if !strings.Contains(content, "failed=false") {
			t.Errorf("expected failed=false, got: %s", content)
		}
	})

	t.Run("register_rc_integer", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-rc.txt")

		playbook := playbookHeader + `
  - name: Run successful command
    shell:
      cmd: echo ok
    register: rc_result

  - name: Check rc is integer
    copy:
      content: "rc={{ rc_result.rc }}"
      dest: /tmp/dibra-register-rc.txt
      mode: "0644"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-rc.txt")
		content = strings.TrimSpace(content)
		if content != "rc=0" {
			t.Errorf("expected rc=0 (integer), got %q", content)
		}
	})

	t.Run("register_ping_common_fields", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-register-ping-common.txt")

		playbook := playbookHeader + `
  - name: Ping
    ping:
    register: ping_common

  - name: Check common fields on ping
    copy:
      content: "ping={{ ping_common.ping }} attempts={{ ping_common.attempts }} failed={{ ping_common.failed }} changed={{ ping_common.changed }}"
      dest: /tmp/dibra-register-ping-common.txt
      mode: "0644"
`
		output := runPlaybook(t, playbook)
		if strings.Contains(output, "FAILED") || strings.Contains(output, "could not execute task") {
			t.Fatalf("expected no failures, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-register-ping-common.txt")
		if !strings.Contains(content, "ping=pong") {
			t.Errorf("expected ping=pong, got: %s", content)
		}
		if !strings.Contains(content, "attempts=1") {
			t.Errorf("expected attempts=1, got: %s", content)
		}
		if !strings.Contains(content, "failed=false") {
			t.Errorf("expected failed=false, got: %s", content)
		}
		if !strings.Contains(content, "changed=false") {
			t.Errorf("expected changed=false, got: %s", content)
		}
	})
}
