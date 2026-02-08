//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_ImportTasks(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/import_tasks")

	t.Run("basic_import", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-before.txt /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt /tmp/dibra-import-after.txt")

		playbook := filepath.Join(testdataDir, "playbook_basic.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		before := remoteFileContent(t, client, "/tmp/dibra-import-before.txt")
		if before != "before-import" {
			t.Errorf("Expected 'before-import', got: %s", before)
		}

		basic := remoteFileContent(t, client, "/tmp/dibra-import-basic.txt")
		if basic != "imported-task-was-here" {
			t.Errorf("Expected 'imported-task-was-here', got: %s", basic)
		}

		basic2 := remoteFileContent(t, client, "/tmp/dibra-import-basic2.txt")
		if basic2 != "second-imported-task" {
			t.Errorf("Expected 'second-imported-task', got: %s", basic2)
		}

		after := remoteFileContent(t, client, "/tmp/dibra-import-after.txt")
		if after != "after-import" {
			t.Errorf("Expected 'after-import', got: %s", after)
		}
	})

	t.Run("freeform_syntax", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_freeform.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-basic.txt")
		if content != "imported-task-was-here" {
			t.Errorf("Expected 'imported-task-was-here', got: %s", content)
		}
	})

	t.Run("file_param_syntax", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_file_param.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-basic.txt")
		if content != "imported-task-was-here" {
			t.Errorf("Expected 'imported-task-was-here', got: %s", content)
		}
	})

	t.Run("subdirectory_import", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-subdir.txt")

		playbook := filepath.Join(testdataDir, "playbook_subdir.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-subdir.txt")
		if content != "from-subtasks-dir" {
			t.Errorf("Expected 'from-subtasks-dir', got: %s", content)
		}
	})

	t.Run("nested_imports", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-level1.txt /tmp/dibra-import-level2.txt")

		playbook := filepath.Join(testdataDir, "playbook_nested.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		level1 := remoteFileContent(t, client, "/tmp/dibra-import-level1.txt")
		if level1 != "level1-task" {
			t.Errorf("Expected 'level1-task', got: %s", level1)
		}

		level2 := remoteFileContent(t, client, "/tmp/dibra-import-level2.txt")
		if level2 != "level2-task" {
			t.Errorf("Expected 'level2-task', got: %s", level2)
		}
	})

	t.Run("circular_import_detection", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_circular.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "circular import") {
			t.Fatalf("Expected circular import error, got: %s", output)
		}
	})

	t.Run("nonexistent_file", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_nonexistent.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "failed to load") && !strings.Contains(output, "Failed to expand") {
			t.Fatalf("Expected file not found error, got: %s", output)
		}
	})

	t.Run("vars_inheritance", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-vars.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-vars.txt")
		if !strings.Contains(content, "greeting=hello") {
			t.Errorf("Expected greeting=hello in content, got: %s", content)
		}
		if !strings.Contains(content, "target=world") {
			t.Errorf("Expected target=world in content, got: %s", content)
		}
	})

	t.Run("vars_override", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-vars-override.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars_override.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-vars-override.txt")
		if !strings.Contains(content, "greeting=hello") {
			t.Errorf("Expected greeting=hello (inherited), got: %s", content)
		}
		if !strings.Contains(content, "target=overridden") {
			t.Errorf("Expected target=overridden (task's own var wins), got: %s", content)
		}
	})

	t.Run("multiple_imports", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt /tmp/dibra-import-multi.txt")

		playbook := filepath.Join(testdataDir, "playbook_multiple_imports.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		basic := remoteFileContent(t, client, "/tmp/dibra-import-basic.txt")
		if basic != "imported-task-was-here" {
			t.Errorf("Expected 'imported-task-was-here', got: %s", basic)
		}

		multi := remoteFileContent(t, client, "/tmp/dibra-import-multi.txt")
		if multi != "multi-import-marker" {
			t.Errorf("Expected 'multi-import-marker', got: %s", multi)
		}

		if !strings.Contains(output, "OK") {
			t.Errorf("Expected ping OK in output, got: %s", output)
		}
	})

	t.Run("mixed_modules", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-mixed.txt")
		remoteExec(t, client, "rm -rf /tmp/dibra-import-mixed-dir")

		playbook := filepath.Join(testdataDir, "playbook_mixed_modules.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-mixed.txt")
		if content != "mixed-import-file" {
			t.Errorf("Expected 'mixed-import-file', got: %s", content)
		}

		if !remoteDirExists(t, client, "/tmp/dibra-import-mixed-dir") {
			t.Error("Expected /tmp/dibra-import-mixed-dir to be created")
		}
	})

	t.Run("ping_import", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_ping_import.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK for ping, got: %s", output)
		}
	})

	t.Run("command_import", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_command_import.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}
		if !strings.Contains(output, "CHANGED") {
			t.Errorf("Expected CHANGED for command, got: %s", output)
		}
	})

	t.Run("same_file_imported_twice", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-between.txt")

		playbook := filepath.Join(testdataDir, "playbook_same_file_twice.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		between := remoteFileContent(t, client, "/tmp/dibra-import-between.txt")
		if between != "between-imports" {
			t.Errorf("Expected 'between-imports', got: %s", between)
		}

		pingCount := strings.Count(output, "Ping from imported file")
		if pingCount != 2 {
			t.Errorf("Expected ping task to appear twice, found %d times", pingCount)
		}
	})

	t.Run("interleaved_imports", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-interleave1.txt /tmp/dibra-import-interleave2.txt /tmp/dibra-import-interleave3.txt /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_interleaved.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		for _, pair := range [][2]string{
			{"/tmp/dibra-import-interleave1.txt", "regular-1"},
			{"/tmp/dibra-import-interleave2.txt", "regular-2"},
			{"/tmp/dibra-import-interleave3.txt", "regular-3"},
			{"/tmp/dibra-import-basic.txt", "imported-task-was-here"},
			{"/tmp/dibra-import-basic2.txt", "second-imported-task"},
		} {
			content := remoteFileContent(t, client, pair[0])
			if content != pair[1] {
				t.Errorf("File %s: expected %q, got: %s", pair[0], pair[1], content)
			}
		}
	})

	t.Run("task_execution_order", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-order.txt")

		orderContent := `- name: Write middle from imported
  shell:
    cmd: echo -n "2," >> /tmp/dibra-import-order.txt
`
		orderPath := filepath.Join(testdataDir, "order_tasks.yaml")
		if err := os.WriteFile(orderPath, []byte(orderContent), 0644); err != nil {
			t.Fatalf("Failed to create order_tasks.yaml: %v", err)
		}

		playbook := playbookHeader + `
  - name: Write first
    shell:
      cmd: echo -n "1," >> /tmp/dibra-import-order.txt

  - name: Import tasks that write
    import_tasks: ` + orderPath + `

  - name: Write last
    shell:
      cmd: echo -n "3" >> /tmp/dibra-import-order.txt
`
		output := runPlaybook(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-order.txt")
		if content != "1,2,3" {
			t.Errorf("Expected task execution order '1,2,3', got: %s", content)
		}
	})

	t.Run("templated_path", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-subdir.txt")

		playbook := filepath.Join(testdataDir, "playbook_templated.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-subdir.txt")
		if content != "from-subtasks-dir" {
			t.Errorf("Expected 'from-subtasks-dir', got: %s", content)
		}
	})

	t.Run("play_level_vars_available", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-play-vars.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars_play_level.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-play-vars.txt")
		if !strings.Contains(content, "app=myapp") {
			t.Errorf("Expected app=myapp, got: %s", content)
		}
		if !strings.Contains(content, "version=1.0") {
			t.Errorf("Expected version=1.0, got: %s", content)
		}
	})

	t.Run("adjacent_relative_imports", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-adjacent.txt /tmp/dibra-import-level2.txt")

		playbook := filepath.Join(testdataDir, "playbook_adjacent.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		adjacent := remoteFileContent(t, client, "/tmp/dibra-import-adjacent.txt")
		if adjacent != "adjacent-import" {
			t.Errorf("Expected 'adjacent-import', got: %s", adjacent)
		}

		level2 := remoteFileContent(t, client, "/tmp/dibra-import-level2.txt")
		if level2 != "level2-task" {
			t.Errorf("Expected 'level2-task', got: %s", level2)
		}
	})

	t.Run("idempotency", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_freeform.yaml")

		output1 := runPlaybookFromFile(t, playbook)
		if strings.Contains(output1, "FAILED") {
			t.Fatalf("First run failed: %s", output1)
		}
		if !strings.Contains(output1, "CHANGED") {
			t.Errorf("Expected CHANGED on first run, got: %s", output1)
		}

		output2 := runPlaybookFromFile(t, playbook)
		if strings.Contains(output2, "FAILED") {
			t.Fatalf("Second run failed: %s", output2)
		}
		if strings.Contains(output2, "CHANGED") {
			t.Errorf("Expected no changes on second run (idempotent), got: %s", output2)
		}
	})

	t.Run("import_with_extra_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-extra.txt")

		taskContent := `- name: Write extra var
  copy:
    content: "{{ custom_msg }}"
    dest: /tmp/dibra-import-extra.txt
    mode: "0644"
`
		taskPath := filepath.Join(testdataDir, "extra_vars_tasks.yaml")
		if err := os.WriteFile(taskPath, []byte(taskContent), 0644); err != nil {
			t.Fatalf("Failed to create extra_vars_tasks.yaml: %v", err)
		}

		playbookContent := `hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
  - name: Import tasks using extra vars
    import_tasks: extra_vars_tasks.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_extra_vars.yaml")
		if err := os.WriteFile(playbookPath, []byte(playbookContent), 0644); err != nil {
			t.Fatalf("Failed to create playbook_extra_vars.yaml: %v", err)
		}

		output := runPlaybookFromFile(t, playbookPath, "-e", "custom_msg=from-extra-vars")

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-extra.txt")
		if content != "from-extra-vars" {
			t.Errorf("Expected 'from-extra-vars', got: %s", content)
		}
	})

	t.Run("deeply_nested_three_levels", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-deep1.txt /tmp/dibra-import-deep2.txt /tmp/dibra-import-deep3.txt")

		deepDir := filepath.Join(testdataDir, "deep")
		os.MkdirAll(filepath.Join(deepDir, "l2/l3"), 0755)

		l3 := `- name: Deep level 3
  copy:
    content: "deep-level3"
    dest: /tmp/dibra-import-deep3.txt
    mode: "0644"
`
		os.WriteFile(filepath.Join(deepDir, "l2/l3/tasks.yaml"), []byte(l3), 0644)

		l2 := `- name: Deep level 2
  copy:
    content: "deep-level2"
    dest: /tmp/dibra-import-deep2.txt
    mode: "0644"

- name: Import level 3
  import_tasks: l3/tasks.yaml
`
		os.WriteFile(filepath.Join(deepDir, "l2/tasks.yaml"), []byte(l2), 0644)

		l1 := `- name: Deep level 1
  copy:
    content: "deep-level1"
    dest: /tmp/dibra-import-deep1.txt
    mode: "0644"

- name: Import level 2
  import_tasks: l2/tasks.yaml
`
		os.WriteFile(filepath.Join(deepDir, "tasks.yaml"), []byte(l1), 0644)

		playbookContent := `hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
  - name: Import deep nested
    import_tasks: deep/tasks.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_deep_nested.yaml")
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		output := runPlaybookFromFile(t, playbookPath)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		for i, expected := range []string{"deep-level1", "deep-level2", "deep-level3"} {
			path := "/tmp/dibra-import-deep" + string(rune('1'+i)) + ".txt"
			content := remoteFileContent(t, client, path)
			if content != expected {
				t.Errorf("File %s: expected %q, got: %s", path, expected, content)
			}
		}
	})

	t.Run("vars_inheritance_multi_level", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-vars-multi.txt")

		mlDir := filepath.Join(testdataDir, "vars_multi")
		os.MkdirAll(filepath.Join(mlDir, "inner"), 0755)

		inner := `- name: Write multi-level var
  copy:
    content: "color={{ color }}"
    dest: /tmp/dibra-import-vars-multi.txt
    mode: "0644"
`
		os.WriteFile(filepath.Join(mlDir, "inner/tasks.yaml"), []byte(inner), 0644)

		outer := `- name: Import inner with vars
  vars:
    color: blue
  import_tasks: inner/tasks.yaml
`
		os.WriteFile(filepath.Join(mlDir, "tasks.yaml"), []byte(outer), 0644)

		playbookContent := `hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
  - name: Import outer
    import_tasks: vars_multi/tasks.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_vars_multi_level.yaml")
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		output := runPlaybookFromFile(t, playbookPath)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-vars-multi.txt")
		if !strings.Contains(content, "color=blue") {
			t.Errorf("Expected color=blue (inherited through nesting), got: %s", content)
		}
	})

	t.Run("import_with_absolute_path", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-import-basic.txt /tmp/dibra-import-basic2.txt")

		absPath := filepath.Join(testdataDir, "basic_tasks.yaml")
		playbook := playbookHeader + `
  - name: Import using absolute path
    import_tasks: ` + absPath + `
`
		output := runPlaybook(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-basic.txt")
		if content != "imported-task-was-here" {
			t.Errorf("Expected 'imported-task-was-here', got: %s", content)
		}
	})

	t.Run("import_preserves_task_names", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_basic.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "Create file from imported task") {
			t.Errorf("Expected imported task name in output, got: %s", output)
		}
		if !strings.Contains(output, "Create second file from imported task") {
			t.Errorf("Expected second imported task name in output, got: %s", output)
		}
		if !strings.Contains(output, "Task before import") {
			t.Errorf("Expected regular task name in output, got: %s", output)
		}
		if !strings.Contains(output, "Task after import") {
			t.Errorf("Expected regular task name in output, got: %s", output)
		}
	})

	t.Run("empty_import_path_error", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Import with empty path
    import_tasks:
      file: ""
`
		output := runPlaybook(t, playbook)

		if !strings.Contains(output, "file path is required") && !strings.Contains(output, "Failed to expand") {
			t.Fatalf("Expected empty path error, got: %s", output)
		}
	})
}
