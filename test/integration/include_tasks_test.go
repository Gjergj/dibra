//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_IncludeTasks(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/include_tasks")

	t.Run("basic_include", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-before.txt /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt /tmp/dibra-include-after.txt")

		playbook := filepath.Join(testdataDir, "playbook_basic.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		before := remoteFileContent(t, client, "/tmp/dibra-include-before.txt")
		if before != "before-include" {
			t.Errorf("Expected 'before-include', got: %s", before)
		}

		basic := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if basic != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", basic)
		}

		basic2 := remoteFileContent(t, client, "/tmp/dibra-include-basic2.txt")
		if basic2 != "second-included-task" {
			t.Errorf("Expected 'second-included-task', got: %s", basic2)
		}

		after := remoteFileContent(t, client, "/tmp/dibra-include-after.txt")
		if after != "after-include" {
			t.Errorf("Expected 'after-include', got: %s", after)
		}
	})

	t.Run("freeform_syntax", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_freeform.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if content != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", content)
		}
	})

	t.Run("file_param_syntax", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_file_param.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if content != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", content)
		}
	})

	t.Run("subdirectory_include", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-subdir.txt")

		playbook := filepath.Join(testdataDir, "playbook_subdir.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-subdir.txt")
		if content != "from-subtasks-dir" {
			t.Errorf("Expected 'from-subtasks-dir', got: %s", content)
		}
	})

	t.Run("nested_includes", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-level1.txt /tmp/dibra-include-level2.txt")

		playbook := filepath.Join(testdataDir, "playbook_nested.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		level1 := remoteFileContent(t, client, "/tmp/dibra-include-level1.txt")
		if level1 != "level1-task" {
			t.Errorf("Expected 'level1-task', got: %s", level1)
		}

		level2 := remoteFileContent(t, client, "/tmp/dibra-include-level2.txt")
		if level2 != "level2-task" {
			t.Errorf("Expected 'level2-task', got: %s", level2)
		}
	})

	t.Run("nonexistent_file", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_nonexistent.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "failed to read") {
			t.Fatalf("Expected file not found error, got: %s", output)
		}
	})

	t.Run("vars_inheritance", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-vars.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-vars.txt")
		if !strings.Contains(content, "greeting=hello") {
			t.Errorf("Expected greeting=hello in content, got: %s", content)
		}
		if !strings.Contains(content, "target=world") {
			t.Errorf("Expected target=world in content, got: %s", content)
		}
	})

	t.Run("vars_override", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-vars-override.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars_override.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-vars-override.txt")
		if !strings.Contains(content, "greeting=hello") {
			t.Errorf("Expected greeting=hello (inherited), got: %s", content)
		}
		if !strings.Contains(content, "target=overridden") {
			t.Errorf("Expected target=overridden (task's own var wins), got: %s", content)
		}
	})

	t.Run("multiple_includes", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt /tmp/dibra-include-multi.txt")

		playbook := filepath.Join(testdataDir, "playbook_multiple.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		basic := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if basic != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", basic)
		}

		multi := remoteFileContent(t, client, "/tmp/dibra-include-multi.txt")
		if multi != "multi-include-marker" {
			t.Errorf("Expected 'multi-include-marker', got: %s", multi)
		}

		if !strings.Contains(output, "OK") {
			t.Errorf("Expected ping OK in output, got: %s", output)
		}
	})

	t.Run("mixed_modules", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-mixed.txt")
		remoteExec(t, client, "rm -rf /tmp/dibra-include-mixed-dir")

		playbook := filepath.Join(testdataDir, "playbook_mixed_modules.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-mixed.txt")
		if content != "mixed-include-file" {
			t.Errorf("Expected 'mixed-include-file', got: %s", content)
		}

		if !remoteDirExists(t, client, "/tmp/dibra-include-mixed-dir") {
			t.Error("Expected /tmp/dibra-include-mixed-dir to be created")
		}
	})

	t.Run("ping_include", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_ping.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("Expected OK for ping, got: %s", output)
		}
	})

	t.Run("command_include", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_command.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}
		if !strings.Contains(output, "CHANGED") {
			t.Errorf("Expected CHANGED for command, got: %s", output)
		}
	})

	t.Run("same_file_included_twice", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-between.txt /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt")

		playbook := filepath.Join(testdataDir, "playbook_same_file_twice.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		basic := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if basic != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", basic)
		}

		between := remoteFileContent(t, client, "/tmp/dibra-include-between.txt")
		if between != "between-includes" {
			t.Errorf("Expected 'between-includes', got: %s", between)
		}
	})

	t.Run("templated_path", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-templated.txt /tmp/dibra-include-templated2.txt")

		playbook := filepath.Join(testdataDir, "playbook_templated.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-templated.txt")
		if content != "templated-one" {
			t.Errorf("Expected 'templated-one', got: %s", content)
		}

		content2 := remoteFileContent(t, client, "/tmp/dibra-include-templated2.txt")
		if content2 != "templated-two" {
			t.Errorf("Expected 'templated-two', got: %s", content2)
		}
	})

	t.Run("templated_path_with_host_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-templated.txt /tmp/dibra-include-templated2.txt")

		playbook := playbookHeader + `
  - name: Include with host-derived templated path
    vars:
      task_file: templated/one.yaml
    include_tasks: "{{ task_file }}"
`
		tmpDir := t.TempDir()
		templatedDir := filepath.Join(tmpDir, "templated")
		os.MkdirAll(templatedDir, 0755)

		oneContent := `- name: Host var templated task
  copy:
    content: "host-templated"
    dest: /tmp/dibra-include-host-templated.txt
    mode: "0644"
`
		os.WriteFile(filepath.Join(templatedDir, "one.yaml"), []byte(oneContent), 0644)

		playbookPath := filepath.Join(tmpDir, "playbook.yaml")
		os.WriteFile(playbookPath, []byte(playbook), 0644)

		remoteExec(t, client, "rm -f /tmp/dibra-include-host-templated.txt")
		output := runPlaybookFromFile(t, playbookPath)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-host-templated.txt")
		if content != "host-templated" {
			t.Errorf("Expected 'host-templated', got: %s", content)
		}
	})

	t.Run("nested_include_with_import", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-adjacent.txt")

		playbook := filepath.Join(testdataDir, "playbook_nested_include_import.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		adjacent := remoteFileContent(t, client, "/tmp/dibra-include-adjacent.txt")
		if adjacent != "adjacent-include" {
			t.Errorf("Expected 'adjacent-include', got: %s", adjacent)
		}
	})

	t.Run("execution_order", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-order1.txt /tmp/dibra-include-order2.txt /tmp/dibra-include-order3.txt /tmp/dibra-include-order4.txt")

		playbook := filepath.Join(testdataDir, "playbook_interleaved.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		for i := 1; i <= 4; i++ {
			path := "/tmp/dibra-include-order" + string(rune('0'+i)) + ".txt"
			expected := "order-" + string(rune('0'+i))
			content := remoteFileContent(t, client, path)
			if content != expected {
				t.Errorf("File %s: expected %q, got: %s", path, expected, content)
			}
		}

		output1Idx := strings.Index(output, "First regular task")
		output2Idx := strings.Index(output, "Included order 2")
		output3Idx := strings.Index(output, "Included order 3")
		output4Idx := strings.Index(output, "Last regular task")
		if output1Idx >= output2Idx || output2Idx >= output3Idx || output3Idx >= output4Idx {
			t.Errorf("Tasks not executed in correct order")
		}
	})

	t.Run("play_level_vars_accessible", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-play-vars.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars_play_level.yaml")
		output := runPlaybookFromFile(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-play-vars.txt")
		if !strings.Contains(content, "app=myapp") {
			t.Errorf("Expected app=myapp, got: %s", content)
		}
		if !strings.Contains(content, "version=v1.0") {
			t.Errorf("Expected version=v1.0, got: %s", content)
		}
	})

	t.Run("idempotency", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt")

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

	t.Run("include_preserves_task_names", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook_basic.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "Create file from included task") {
			t.Errorf("Expected included task name in output, got: %s", output)
		}
		if !strings.Contains(output, "Create second file from included task") {
			t.Errorf("Expected second included task name in output, got: %s", output)
		}
		if !strings.Contains(output, "Task before include") {
			t.Errorf("Expected regular task name in output, got: %s", output)
		}
		if !strings.Contains(output, "Task after include") {
			t.Errorf("Expected regular task name in output, got: %s", output)
		}
	})

	t.Run("empty_include_path_error", func(t *testing.T) {
		playbook := playbookHeader + `
  - name: Include with empty path
    include_tasks:
      file: ""
`
		output := runPlaybook(t, playbook)

		if !strings.Contains(output, "file path is required") {
			t.Fatalf("Expected empty path error, got: %s", output)
		}
	})

	t.Run("include_with_extra_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-extra.txt")

		taskContent := `- name: Write extra var
  copy:
    content: "{{ custom_msg }}"
    dest: /tmp/dibra-include-extra.txt
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
  - name: Include tasks using extra vars
    include_tasks: extra_vars_tasks.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_extra_vars.yaml")
		if err := os.WriteFile(playbookPath, []byte(playbookContent), 0644); err != nil {
			t.Fatalf("Failed to create playbook_extra_vars.yaml: %v", err)
		}

		output := runPlaybookFromFile(t, playbookPath, "-e", "custom_msg=from-extra-vars")

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-extra.txt")
		if content != "from-extra-vars" {
			t.Errorf("Expected 'from-extra-vars', got: %s", content)
		}
	})

	t.Run("deeply_nested_three_levels", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-deep1.txt /tmp/dibra-include-deep2.txt /tmp/dibra-include-deep3.txt")

		deepDir := filepath.Join(testdataDir, "deep")
		os.MkdirAll(filepath.Join(deepDir, "l2/l3"), 0755)

		l3 := `- name: Deep level 3
  copy:
    content: "deep-level3"
    dest: /tmp/dibra-include-deep3.txt
    mode: "0644"
`
		os.WriteFile(filepath.Join(deepDir, "l2/l3/tasks.yaml"), []byte(l3), 0644)

		l2 := `- name: Deep level 2
  copy:
    content: "deep-level2"
    dest: /tmp/dibra-include-deep2.txt
    mode: "0644"

- name: Include level 3
  include_tasks: l3/tasks.yaml
`
		os.WriteFile(filepath.Join(deepDir, "l2/tasks.yaml"), []byte(l2), 0644)

		l1 := `- name: Deep level 1
  copy:
    content: "deep-level1"
    dest: /tmp/dibra-include-deep1.txt
    mode: "0644"

- name: Include level 2
  include_tasks: l2/tasks.yaml
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
  - name: Include deep nested
    include_tasks: deep/tasks.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_deep_nested.yaml")
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		output := runPlaybookFromFile(t, playbookPath)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		for i, expected := range []string{"deep-level1", "deep-level2", "deep-level3"} {
			path := "/tmp/dibra-include-deep" + string(rune('1'+i)) + ".txt"
			content := remoteFileContent(t, client, path)
			if content != expected {
				t.Errorf("File %s: expected %q, got: %s", path, expected, content)
			}
		}
	})

	t.Run("vars_inheritance_multi_level", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-vars-multi.txt")

		mlDir := filepath.Join(testdataDir, "vars_multi")
		os.MkdirAll(filepath.Join(mlDir, "inner"), 0755)

		inner := `- name: Write multi-level var
  copy:
    content: "color={{ color }}"
    dest: /tmp/dibra-include-vars-multi.txt
    mode: "0644"
`
		os.WriteFile(filepath.Join(mlDir, "inner/tasks.yaml"), []byte(inner), 0644)

		outer := `- name: Include inner with vars
  vars:
    color: blue
  include_tasks: inner/tasks.yaml
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
  - name: Include outer
    include_tasks: vars_multi/tasks.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_vars_multi_level.yaml")
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		output := runPlaybookFromFile(t, playbookPath)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-vars-multi.txt")
		if !strings.Contains(content, "color=blue") {
			t.Errorf("Expected color=blue (inherited through nesting), got: %s", content)
		}
	})

	t.Run("include_with_absolute_path", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt")

		absPath := filepath.Join(testdataDir, "basic_tasks.yaml")
		playbook := playbookHeader + `
  - name: Include using absolute path
    include_tasks: ` + absPath + `
`
		output := runPlaybook(t, playbook)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if content != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", content)
		}
	})

	t.Run("include_then_import_interaction", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-adjacent.txt")

		importDir := filepath.Join(testdataDir, "include_import_mix")
		os.MkdirAll(importDir, 0755)

		importedTasks := `- name: Imported via include chain
  copy:
    content: "import-in-include"
    dest: /tmp/dibra-import-in-include.txt
    mode: "0644"
`
		os.WriteFile(filepath.Join(importDir, "imported.yaml"), []byte(importedTasks), 0644)

		includedTasks := `- name: Include contains import
  import_tasks: imported.yaml
`
		os.WriteFile(filepath.Join(importDir, "included.yaml"), []byte(includedTasks), 0644)

		playbookContent := `hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
  - name: Include file that imports
    include_tasks: include_import_mix/included.yaml
`
		playbookPath := filepath.Join(testdataDir, "playbook_include_import_mix.yaml")
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		remoteExec(t, client, "rm -f /tmp/dibra-import-in-include.txt")
		output := runPlaybookFromFile(t, playbookPath)

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-import-in-include.txt")
		if content != "import-in-include" {
			t.Errorf("Expected 'import-in-include', got: %s", content)
		}
	})

	t.Run("templated_path_with_extra_vars", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-include-basic.txt /tmp/dibra-include-basic2.txt")

		playbookContent := `hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
  - name: Include with extra-var-templated path
    include_tasks: "{{ include_file }}"
`
		playbookPath := filepath.Join(testdataDir, "playbook_templated_extra.yaml")
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		output := runPlaybookFromFile(t, playbookPath, "-e", "include_file=basic_tasks.yaml")

		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-include-basic.txt")
		if content != "included-task-was-here" {
			t.Errorf("Expected 'included-task-was-here', got: %s", content)
		}
	})
}
