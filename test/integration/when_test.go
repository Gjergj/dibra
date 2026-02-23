//go:build integration

package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_When(t *testing.T) {
	client := getClient(t)
	defer client.Close()
	projectRoot := getProjectRoot()
	testdataDir := filepath.Join(projectRoot, "test/integration/testdata/when")

	t.Run("basic_conditions", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-when-*.txt")

		playbook := filepath.Join(testdataDir, "playbook_basic.yaml")
		output := runPlaybookFromFile(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}
		if !strings.Contains(output, "SKIPPED") {
			t.Fatalf("Expected skipped tasks in output, got: %s", output)
		}

		expectedFiles := map[string]string{
			"/tmp/dibra-when-always.txt":       "always",
			"/tmp/dibra-when-equals.txt":       "equals",
			"/tmp/dibra-when-in-list.txt":      "in-list",
			"/tmp/dibra-when-numeric-true.txt": "numeric-true",
			"/tmp/dibra-when-bool-true.txt":    "bool-true",
			"/tmp/dibra-when-list-true.txt":    "list-true",
			"/tmp/dibra-when-defined.txt":      "defined",
			"/tmp/dibra-when-default.txt":      "default-filter",
			"/tmp/dibra-when-length.txt":       "length-filter",
			"/tmp/dibra-when-substring.txt":    "substring",
			"/tmp/dibra-when-complex.txt":      "complex",
			"/tmp/dibra-when-inventory.txt":    "inventory",
			"/tmp/dibra-when-group.txt":        "group",
			"/tmp/dibra-when-hostvars.txt":     "hostvars",
			"/tmp/dibra-when-register.txt":     "registered",
			"/tmp/dibra-when-literal-true.txt": "literal-true",
			"/tmp/dibra-when-literal-num.txt":  "literal-num",
		}
		for path, expected := range expectedFiles {
			if !remoteFileExists(t, client, path) {
				t.Errorf("Expected file %s to exist", path)
				continue
			}
			content := remoteFileContent(t, client, path)
			if content != expected {
				t.Errorf("Expected %s content %q, got %q", path, expected, content)
			}
		}

		skippedFiles := []string{
			"/tmp/dibra-when-not-equals.txt",
			"/tmp/dibra-when-not-in-list.txt",
			"/tmp/dibra-when-numeric-false.txt",
			"/tmp/dibra-when-bool-false.txt",
			"/tmp/dibra-when-list-false.txt",
			"/tmp/dibra-when-undefined.txt",
			"/tmp/dibra-when-register-skip.txt",
			"/tmp/dibra-when-literal-false.txt",
			"/tmp/dibra-when-literal-zero.txt",
		}
		for _, path := range skippedFiles {
			if remoteFileExists(t, client, path) {
				t.Errorf("Expected file %s to be skipped", path)
			}
		}
	})

	t.Run("include_and_import", func(t *testing.T) {
		remoteExec(t, client, "rm -f /tmp/dibra-when-include-*.txt /tmp/dibra-when-import-*.txt")

		playbook := filepath.Join(testdataDir, "playbook_include_import.yaml")
		output := runPlaybookFromFile(t, playbook)
		if strings.Contains(output, "FAILED") {
			t.Fatalf("Playbook failed: %s", output)
		}
		if !strings.Contains(output, "SKIPPED") {
			t.Fatalf("Expected skipped tasks in output, got: %s", output)
		}

		expectedFiles := map[string]string{
			"/tmp/dibra-when-include-true.txt": "include-true",
			"/tmp/dibra-when-import-true.txt":  "import-true",
		}
		for path, expected := range expectedFiles {
			if !remoteFileExists(t, client, path) {
				t.Errorf("Expected file %s to exist", path)
				continue
			}
			content := remoteFileContent(t, client, path)
			if content != expected {
				t.Errorf("Expected %s content %q, got %q", path, expected, content)
			}
		}

		skippedFiles := []string{
			"/tmp/dibra-when-include-inner-skip.txt",
			"/tmp/dibra-when-include-skip.txt",
			"/tmp/dibra-when-import-inner-skip.txt",
			"/tmp/dibra-when-import-skip.txt",
		}
		for _, path := range skippedFiles {
			if remoteFileExists(t, client, path) {
				t.Errorf("Expected file %s to be skipped", path)
			}
		}
	})
}
