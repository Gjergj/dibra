//go:build integration

package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_Loops(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteExec(t, client, "rm -f /tmp/dibra-loop-*.txt")

	projectRoot := getProjectRoot()
	playbook := filepath.Join(projectRoot, "test/integration/testdata/loops/playbook.yaml")
	output := runPlaybookFromFile(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("Playbook failed: %s", output)
	}

	expectedFiles := map[string]string{
		"/tmp/dibra-loop-alpha.txt":          "alpha",
		"/tmp/dibra-loop-bravo.txt":          "bravo",
		"/tmp/dibra-loop-selected-bravo.txt": "selected=bravo",
		"/tmp/dibra-loop-flat-red.txt":       "red",
		"/tmp/dibra-loop-flat-green.txt":     "green",
		"/tmp/dibra-loop-flat-blue.txt":      "blue",
		"/tmp/dibra-loop-list-0.txt":         "uno",
		"/tmp/dibra-loop-list-1.txt":         "dos",
		"/tmp/dibra-loop-dict-first.txt":     "one",
		"/tmp/dibra-loop-dict-second.txt":    "two",
		"/tmp/dibra-loop-var-0.txt":          "alpha-0",
		"/tmp/dibra-loop-var-1.txt":          "bravo-1",
		"/tmp/dibra-loop-register.txt":       "alpha|bravo",
		"/tmp/dibra-loop-include-first.txt":  "100",
		"/tmp/dibra-loop-include-second.txt": "200",
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

	extendedChecks := map[string]string{
		"/tmp/dibra-loop-extended-alpha.txt": "index=1 index0=0 first=true last=false",
		"/tmp/dibra-loop-extended-bravo.txt": "index=2 index0=1 first=false last=true",
	}
	for path, expected := range extendedChecks {
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
		"/tmp/dibra-loop-selected-alpha.txt",
		"/tmp/dibra-loop-empty.txt",
	}
	for _, path := range skippedFiles {
		if remoteFileExists(t, client, path) {
			t.Errorf("Expected file %s to be skipped", path)
		}
	}
}
