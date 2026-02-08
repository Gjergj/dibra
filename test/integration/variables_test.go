//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_Variables(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/variables")

	t.Run("precedence_and_namespaces", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-vars.txt /tmp/dibra-task.txt")

		playbook := filepath.Join(testdataDir, "playbook.yaml")
		output := runPlaybookFromFile(t, playbook, "-e", "extra_key=extra_value")
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-vars.txt")
		checks := map[string]string{
			"greeting=hello":                "play-level greeting",
			"app_name=play":                 "play overrides host (play > host precedence)",
			"app_port=9090":                 "vars_files override (port from vars_files.yml)",
			"merged_value=from_vars_file":   "merged value from vars_files",
			"namespace_group=group-value":   "group namespace via vars.group",
			"namespace_host=host-value":     "host namespace via vars.host",
			"namespace_extra=extra_value":   "extra namespace via vars.extra",
			"inventory=testhost":            "inventory_hostname magic var",
			"group_names=web":               "group_names magic var",
			"groups_members=testhost":       "groups magic var",
			"hostvars_self=testhost":        "hostvars inventory_hostname",
			"hostvars_key=host-value":       "hostvars cross-host key access",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}

		taskContent := remoteFileContent(t, client, "/tmp/dibra-task.txt")
		if !strings.Contains(taskContent, "task=task-value") {
			t.Errorf("expected task namespace override, got: %s", taskContent)
		}
	})

	t.Run("idempotency", func(t *testing.T) {
		playbook := filepath.Join(testdataDir, "playbook.yaml")
		output := runPlaybookFromFile(t, playbook, "-e", "extra_key=extra_value")
		if strings.Contains(output, "CHANGED") {
			t.Fatalf("expected idempotent run (no changes), got: %s", output)
		}
	})

	t.Run("fetch_with_variables", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/fetch.example.txt")
		os.RemoveAll("/tmp/dibra-fetch/")

		fetchPlaybook := filepath.Join(testdataDir, "playbook_fetch.yaml")
		output := runPlaybookFromFile(t, fetchPlaybook, "-e", "fetch_name=fetch.example")
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected fetch playbook to change resources, got: %s", output)
		}
	})

	t.Run("replace_merge_strategy", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-replace.txt")

		playbook := filepath.Join(testdataDir, "playbook_replace.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-replace.txt")
		if !strings.Contains(content, "app_name=play") {
			t.Errorf("expected app_name=play, got: %s", content)
		}
		if !strings.Contains(content, "base_dir=/tmp/dibra-replace-test") {
			t.Errorf("expected base_dir rendered, got: %s", content)
		}
	})

	t.Run("extra_vars_override_play", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-extra-override.txt")

		playbook := filepath.Join(testdataDir, "playbook_extra_override.yaml")
		output := runPlaybookFromFile(t, playbook, "-e", "greeting=overridden")
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-extra-override.txt")
		if !strings.Contains(content, "greeting=overridden") {
			t.Errorf("extra vars should override play vars, got: %s", content)
		}
		if !strings.Contains(content, "color=blue") {
			t.Errorf("non-overridden play var should remain, got: %s", content)
		}
	})

	t.Run("extra_vars_from_file", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-extra-override.txt")

		playbook := filepath.Join(testdataDir, "playbook_extra_override.yaml")
		extraFile := filepath.Join(testdataDir, "extra_vars_file.yml")
		output := runPlaybookFromFile(t, playbook, "-e", "@"+extraFile)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-extra-override.txt")
		if !strings.Contains(content, "greeting=from-file") {
			t.Errorf("extra vars from file should override play, got: %s", content)
		}
	})

	t.Run("nested_vars_and_list_access", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-nested.txt")

		playbook := filepath.Join(testdataDir, "playbook_nested.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-nested.txt")
		checks := map[string]string{
			"db_host=db.example.com": "nested map access",
			"db_port=5432":           "nested map access (int)",
			"db_user=admin":          "deeply nested map access",
			"item0=first":            "list index 0",
			"item2=third":            "list index 2",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("variables_in_file_module", func(t *testing.T) {
		remoteExec(t, client, "rm -rf /tmp/dibra-vars-dir")

		playbook := filepath.Join(testdataDir, "playbook_file_module.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		if !remoteDirExists(t, client, "/tmp/dibra-vars-dir") {
			t.Error("expected directory /tmp/dibra-vars-dir to exist")
		}
		mode := remoteFileMode(t, client, "/tmp/dibra-vars-dir")
		if !strings.Contains(mode, "755") {
			t.Errorf("expected mode 755, got: %s", mode)
		}

		output = runPlaybookFromFile(t, playbook)
		if strings.Contains(output, "CHANGED") {
			t.Fatalf("expected idempotent run, got: %s", output)
		}
	})

	t.Run("multi_group_precedence_alpha_merge", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-multigroup.txt")

		playbook := filepath.Join(testdataDir, "playbook_multi_group.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-multigroup.txt")
		checks := map[string]string{
			"aaa_only=aaa":           "aaa group var present via merge union",
			"zzz_only=zzz":           "zzz group var present via merge union",
			"nested_a=from_aaa":      "deep merge: nested.a from aaa group",
			"nested_c=from_zzz":      "deep merge: nested.c from zzz group",
			"nested_common=zzz":      "alphabetical override: zzz wins over web wins over aaa",
			"group_key_multi=zzz":    "alphabetical override: zzz is last",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("vars_files_deep_merge", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-varsfiles-deep-merge.txt")

		playbook := filepath.Join(testdataDir, "playbook_vars_files_deep_merge.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-varsfiles-deep-merge.txt")
		checks := map[string]string{
			"a=A":        "config.a from vars_deep_1",
			"b=B":        "config.b from vars_deep_2",
			"shared_x=1": "config.shared.x from vars_deep_1 preserved",
			"shared_y=2": "config.shared.y from vars_deep_2 merged in",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("iterative_template_resolution_chain", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-iterative.txt")

		playbook := filepath.Join(testdataDir, "playbook_iterative_templates.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-iterative.txt")
		checks := map[string]string{
			"deploy_dir=/opt/myapp":                    "two-level chain: base + app_name",
			"config_dir=/opt/myapp/config":             "three-level chain: deploy_dir + /config",
			"final_path=/opt/myapp/config/settings.yml": "four-level chain: config_dir + file_name",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("%s: expected %q in output, got: %s", desc, expected, content)
			}
		}
	})

	t.Run("undefined_variable_fails_task", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-should-not-write.txt")

		playbook := filepath.Join(testdataDir, "playbook_undefined_var.yaml")
		output := runPlaybookFromFile(t, playbook)

		if !strings.Contains(output, "unknown variable") || !strings.Contains(output, "nonexistent") {
			t.Errorf("expected error about unknown variable 'nonexistent', got: %s", output)
		}

		if remoteFileExists(t, client, "/tmp/dibra-should-not-write.txt") {
			t.Error("file should not have been written when variable is undefined")
		}
	})

	t.Run("task_vars_do_not_leak_to_next_task", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-task-scope-1.txt /tmp/dibra-task-scope-2.txt")

		playbook := filepath.Join(testdataDir, "playbook_task_vars_scope.yaml")
		output := runPlaybookFromFile(t, playbook)

		content := remoteFileContent(t, client, "/tmp/dibra-task-scope-1.txt")
		if !strings.Contains(content, "scoped=only-here") {
			t.Errorf("task1 should have written scoped var, got: %s", content)
		}

		if !strings.Contains(output, "unknown variable") {
			t.Errorf("task2 should fail with unknown variable error, got: %s", output)
		}

		if remoteFileExists(t, client, "/tmp/dibra-task-scope-2.txt") {
			t.Error("task2 file should not exist since scoped var is undefined in task2")
		}
	})

	t.Run("hostvars_bracket_quotes_multi_host", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-hostvars-testhost.txt /tmp/dibra-hostvars-otherhost.txt")

		playbook := filepath.Join(testdataDir, "playbook_hostvars_multi.yaml")
		output := runPlaybookFromFile(t, playbook)
		if !strings.Contains(output, "CHANGED") {
			t.Fatalf("expected playbook to change resources, got: %s", output)
		}

		content := remoteFileContent(t, client, "/tmp/dibra-hostvars-testhost.txt")
		checks := map[string]string{
			"self=testhost":          "inventory_hostname for testhost",
			"other_key=other-value":  "hostvars bracket access to otherhost",
			"testhost_key=host-value": "hostvars bracket access to self",
		}
		for expected, desc := range checks {
			if !strings.Contains(content, expected) {
				t.Errorf("testhost: %s: expected %q in output, got: %s", desc, expected, content)
			}
		}

		content2 := remoteFileContent(t, client, "/tmp/dibra-hostvars-otherhost.txt")
		checks2 := map[string]string{
			"self=otherhost":          "inventory_hostname for otherhost",
			"other_key=other-value":   "hostvars bracket access to self (otherhost)",
			"testhost_key=host-value": "hostvars bracket access to testhost from otherhost",
		}
		for expected, desc := range checks2 {
			if !strings.Contains(content2, expected) {
				t.Errorf("otherhost: %s: expected %q in output, got: %s", desc, expected, content2)
			}
		}
	})
}

func runPlaybookFromFile(t *testing.T, playbook string, extraArgs ...string) string {
	projectRoot := getProjectRoot()
	args := []string{"run", "./cmd/controller", "-config", playbook, "-v", "-force-agent-upload", "-agent-build"}
	args = append(args, extraArgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "DIBRA_TEST=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Playbook output:\n%s", string(output))
	}
	return string(output)
}
