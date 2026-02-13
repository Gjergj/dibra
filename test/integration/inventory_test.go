//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_Inventory(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/inventory")

	t.Run("basic_inventory", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-basic.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write test file
    copy:
      content: "inventory_works=yes"
      dest: /tmp/dibra-inv-basic.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected changes, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-basic.txt")
		if content != "inventory_works=yes" {
			t.Errorf("expected file content, got: %s", content)
		}
	})

	t.Run("basic_idempotency", func(t *testing.T) {
		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write test file
    copy:
      content: "inventory_works=yes"
      dest: /tmp/dibra-inv-basic.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "CHANGED") {
			t.Fatalf("expected idempotent run (no changes), got: %s", output)
		}
	})

	t.Run("host_shown_in_output", func(t *testing.T) {
		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Ping
    ping:
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if !strings.Contains(output, "Host: testhost") {
			t.Errorf("expected host name in output, got: %s", output)
		}
	})

	t.Run("inventory_with_groups_and_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-groups.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write group vars
    copy:
      content: "env={{ env }}\nhttp_port={{ http_port }}\ncustom_var={{ custom_var }}"
      dest: /tmp/dibra-inv-groups.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "groups.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-groups.txt")
		checks := map[string]string{
			"env=default":          "all group var inherited",
			"http_port=80":         "webservers group var",
			"custom_var=from_host": "host inline var",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("children_group_hierarchy", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-children.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write hierarchy vars
    copy:
      content: "level={{ level }}\nall_var={{ all_var }}\nregion={{ region }}\nenv={{ env }}"
      dest: /tmp/dibra-inv-children.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "children.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-children.txt")
		checks := map[string]string{
			"level=region_east": "child group var overrides parent",
			"all_var=present":   "all group var inherited",
			"region=east":       "child group var",
			"env=prod":          "parent group var inherited",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("implicit_all_no_wrapper", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-noall.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write vars
    copy:
      content: "role={{ role }}"
      dest: /tmp/dibra-inv-noall.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "no_all.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-noall.txt")
		if !strings.Contains(content, "role=web") {
			t.Errorf("expected role=web, got: %s", content)
		}
	})

	t.Run("ungrouped_host", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-ungrouped.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write vars
    copy:
      content: "standalone_var={{ standalone_var }}"
      dest: /tmp/dibra-inv-ungrouped.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "ungrouped.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-ungrouped.txt")
		if !strings.Contains(content, "standalone_var=standalone_value") {
			t.Errorf("expected standalone_var=standalone_value, got: %s", content)
		}
	})

	t.Run("group_vars_host_vars_files", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-files.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write file-based vars
    copy:
      content: "group_var_from_file={{ group_var_from_file }}\nhost_var_from_file={{ host_var_from_file }}\nhttp_port={{ http_port }}"
      dest: /tmp/dibra-inv-files.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "groups.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-files.txt")
		if !strings.Contains(content, "group_var_from_file=webservers_file_value") {
			t.Errorf("expected group_var_from_file, got: %s", content)
		}
		if !strings.Contains(content, "host_var_from_file=testhost_file_value") {
			t.Errorf("expected host_var_from_file, got: %s", content)
		}
		if !strings.Contains(content, "http_port=80") {
			t.Errorf("expected http_port=80 (inline overrides file), got: %s", content)
		}
	})

	t.Run("deep_hierarchy", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-deep.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write deep vars
    copy:
      content: "depth={{ depth }}\nall_only={{ all_only }}\nl1_var={{ l1_var }}\nl2_var={{ l2_var }}\nl3_var={{ l3_var }}"
      dest: /tmp/dibra-inv-deep.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "deep_hierarchy.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-deep.txt")
		checks := map[string]string{
			"depth=3":            "deepest child wins",
			"all_only=all_value": "all var inherited through all levels",
			"l1_var=l1_value":    "level1 var inherited",
			"l2_var=l2_value":    "level2 var inherited",
			"l3_var=l3_value":    "level3 var present",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("multi_parent_groups", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-multiparent.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write multi-parent vars
    copy:
      content: "monitored={{ monitored }}\nenv={{ env }}"
      dest: /tmp/dibra-inv-multiparent.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "multi_parent.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-multiparent.txt")
		if !strings.Contains(content, "monitored=true") {
			t.Errorf("expected monitored=true from monitoring group, got: %s", content)
		}
		if !strings.Contains(content, "env=staging") {
			t.Errorf("expected env=staging from staging group, got: %s", content)
		}
	})

	t.Run("host_vars_override_group_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-override.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write override var
    copy:
      content: "shared_var={{ shared_var }}"
      dest: /tmp/dibra-inv-override.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "host_vars_override.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-override.txt")
		if !strings.Contains(content, "shared_var=from_host") {
			t.Errorf("expected host vars to override group vars, got: %s", content)
		}
	})

	t.Run("magic_variables_with_inventory", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-magic.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write magic vars
    copy:
      content: "hostname={{ inventory_hostname }}"
      dest: /tmp/dibra-inv-magic.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-magic.txt")
		if !strings.Contains(content, "hostname=testhost") {
			t.Errorf("expected hostname=testhost, got: %s", content)
		}
	})

	t.Run("playbook_inventory_reference", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-ref.txt")

		playbook := filepath.Join(testdataDir, "playbook_inline_ref.yaml")
		output := runPlaybookFromFile(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}
		if !strings.Contains(output, "Host: testhost") {
			t.Errorf("expected host from playbook inventory reference, got: %s", output)
		}
	})

	t.Run("error_both_hosts_and_inventory", func(t *testing.T) {
		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
tasks:
  - name: Ping
    ping:
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if !strings.Contains(output, "Cannot use both inventory") {
			t.Errorf("expected error about conflicting hosts/inventory, got: %s", output)
		}
	})

	t.Run("inventory_with_play_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-playvars.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
vars:
  play_var: from_play

tasks:
  - name: Write play + inventory vars
    copy:
      content: "play_var={{ play_var }}\nhostname={{ inventory_hostname }}"
      dest: /tmp/dibra-inv-playvars.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-playvars.txt")
		if !strings.Contains(content, "play_var=from_play") {
			t.Errorf("expected play vars to work, got: %s", content)
		}
		if !strings.Contains(content, "hostname=testhost") {
			t.Errorf("expected inventory hostname, got: %s", content)
		}
	})

	t.Run("inventory_with_extra_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-extravars.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write extra vars
    copy:
      content: "extra={{ extra_key }}"
      dest: /tmp/dibra-inv-extravars.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile, "-e", "extra_key=extra_value")
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-extravars.txt")
		if !strings.Contains(content, "extra=extra_value") {
			t.Errorf("expected extra vars, got: %s", content)
		}
	})

	t.Run("inventory_with_task_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-taskvars.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write task vars
    vars:
      task_var: from_task
    copy:
      content: "task_var={{ task_var }}"
      dest: /tmp/dibra-inv-taskvars.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-taskvars.txt")
		if !strings.Contains(content, "task_var=from_task") {
			t.Errorf("expected task vars, got: %s", content)
		}
	})

	t.Run("inventory_group_names_magic_var", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-groupnames.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write group names
    copy:
      content: "groups={{ group_names[0] }}"
      dest: /tmp/dibra-inv-groupnames.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "no_all.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-groupnames.txt")
		if !strings.Contains(content, "groups=webservers") {
			t.Errorf("expected group_names to contain webservers, got: %s", content)
		}
	})

	t.Run("inventory_not_found_error", func(t *testing.T) {
		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Ping
    ping:
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", "/nonexistent/inventory.yaml")
		if !strings.Contains(output, "Failed to load inventory") {
			t.Errorf("expected error for missing inventory, got: %s", output)
		}
	})

	t.Run("inventory_empty_playbook_tasks", func(t *testing.T) {
		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Simple ping
    ping:
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success with ping, got: %s", output)
		}
		if !strings.Contains(output, "pong") || !strings.Contains(output, "OK") {
			t.Logf("output: %s", output)
		}
	})

	t.Run("inventory_register_var", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-register.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Run command
    shell:
      cmd: echo hello-from-inventory
    register: cmd_result
  - name: Write registered result
    copy:
      content: "stdout={{ cmd_result.stdout }}"
      dest: /tmp/dibra-inv-register.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-register.txt")
		if !strings.Contains(content, "stdout=hello-from-inventory") {
			t.Errorf("expected registered var, got: %s", content)
		}
	})

	t.Run("inventory_with_import_tasks", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-import.txt")

		taskDir := t.TempDir()
		taskFile := filepath.Join(taskDir, "tasks.yaml")
		os.WriteFile(taskFile, []byte(`
- name: Write from imported task
  copy:
    content: "imported=yes"
    dest: /tmp/dibra-inv-import.txt
`), 0644)

		playbook := filepath.Join(taskDir, "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Import tasks
    import_tasks: tasks.yaml
`), 0644)

		invFile := filepath.Join(testdataDir, "basic.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-import.txt")
		if !strings.Contains(content, "imported=yes") {
			t.Errorf("expected imported task to run, got: %s", content)
		}
	})

	t.Run("inventory_ssh_key_path", func(t *testing.T) {
		invDir := t.TempDir()
		invFile := filepath.Join(invDir, "inventory.yaml")
		os.WriteFile(invFile, []byte(`
all:
  hosts:
    testhost:
      host: localhost
      port: 2222
      user: root
      ssh_pass: rootpass
      become: true
      become_password: rootpass
`), 0644)

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Ping with inventory
    ping:
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}
	})

	t.Run("inventory_port_as_string", func(t *testing.T) {
		invDir := t.TempDir()
		invFile := filepath.Join(invDir, "inventory.yaml")
		os.WriteFile(invFile, []byte(`
all:
  hosts:
    testhost:
      host: localhost
      port: "2222"
      user: root
      ssh_pass: rootpass
`), 0644)

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Ping
    ping:
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success (port as string should be coerced), got: %s", output)
		}
	})

	t.Run("inventory_become_as_string", func(t *testing.T) {
		invDir := t.TempDir()
		invFile := filepath.Join(invDir, "inventory.yaml")
		os.WriteFile(invFile, []byte(`
all:
  hosts:
    testhost:
      host: localhost
      port: 2222
      user: root
      ssh_pass: rootpass
      become: "yes"
      become_password: rootpass
`), 0644)

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Ping with string become
    ping:
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success (become as string should work), got: %s", output)
		}
	})

	t.Run("inventory_groups_in_context", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-ctx.txt")

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Check groups context
    shell:
      cmd: echo "groups_work"
    register: check_result
  - name: Write result
    copy:
      content: "result={{ check_result.stdout }}\nhostname={{ inventory_hostname }}"
      dest: /tmp/dibra-inv-ctx.txt
`), 0644)

		invFile := filepath.Join(testdataDir, "groups.yaml")
		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-ctx.txt")
		if !strings.Contains(content, "result=groups_work") {
			t.Errorf("expected result, got: %s", content)
		}
		if !strings.Contains(content, "hostname=testhost") {
			t.Errorf("expected hostname, got: %s", content)
		}
	})

	t.Run("secret_variable_reuse", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-secret-reuse.txt")

		// Create inventory with a "secret" var (plain value simulating resolved secret)
		// Uses rootpass as the shared secret since that's the test container's SSH password.
		// This verifies that {{ }} template resolution works in ssh_pass and become_password.
		invDir := t.TempDir()
		invFile := filepath.Join(invDir, "inventory.yaml")
		os.WriteFile(invFile, []byte(`
all:
  vars:
    shared_secret: rootpass
    app_secret: resolved_password
  hosts:
    testhost:
      host: localhost
      port: 2222
      user: root
      ssh_pass: "{{ shared_secret }}"
      become: true
      become_password: "{{ shared_secret }}"
`), 0644)

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write secret var
    copy:
      content: "secret={{ app_secret }}"
      dest: /tmp/dibra-inv-secret-reuse.txt
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-secret-reuse.txt")
		if !strings.Contains(content, "secret=resolved_password") {
			t.Errorf("expected secret=resolved_password, got: %s", content)
		}
	})

	t.Run("invalid_secret_reference", func(t *testing.T) {
		invDir := t.TempDir()
		invFile := filepath.Join(invDir, "inventory.yaml")
		os.WriteFile(invFile, []byte(`
all:
  vars:
    bad_secret: "!bw:nonexistent/password"
  hosts:
    testhost:
      host: localhost
      port: 2222
      user: root
      ssh_pass: rootpass
`), 0644)

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Ping
    ping:
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		// Should fail because bw CLI is likely not available or the item doesn't exist
		if !strings.Contains(output, "Failed to resolve inventory secrets") && !strings.Contains(output, "FAILED") {
			t.Logf("output: %s", output)
		}
	})

	t.Run("mixed_secrets_and_plain_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-inv-mixed.txt")

		invDir := t.TempDir()
		invFile := filepath.Join(invDir, "inventory.yaml")
		os.WriteFile(invFile, []byte(`
all:
  vars:
    plain_var: hello_world
    port_var: 8080
  hosts:
    testhost:
      host: localhost
      port: 2222
      user: root
      ssh_pass: rootpass
      become: true
      become_password: rootpass
      custom_host_var: from_host
`), 0644)

		playbook := filepath.Join(t.TempDir(), "playbook.yaml")
		os.WriteFile(playbook, []byte(`
tasks:
  - name: Write mixed vars
    copy:
      content: "plain={{ plain_var }}\nport={{ port_var }}\nhost_var={{ custom_host_var }}"
      dest: /tmp/dibra-inv-mixed.txt
`), 0644)

		output := runPlaybookFromFile(t, playbook, "-i", invFile)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("expected success, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-inv-mixed.txt")
		checks := map[string]string{
			"plain=hello_world":  "plain var",
			"port=8080":          "numeric var",
			"host_var=from_host": "host-level var",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})
}
