//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_FetchBasic(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/goansible-test-fetch-src"
	content := "fetch test content 123"

	client.Run("echo -n '" + content + "' > " + remoteFile)

	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch file from remote
    fetch:
      src: /tmp/goansible-test-fetch-src
      dest: ` + localDest + `
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch")
	}

	expectedPath := filepath.Join(localDest, "testhost", remoteFile)
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read fetched file at %s: %v", expectedPath, err)
	}
	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	client.Run("rm -f " + remoteFile)
}

func TestPlaybook_FetchFlat(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/goansible-test-fetch-flat"
	content := "flat fetch content"

	client.Run("echo -n '" + content + "' > " + remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "fetched-file.txt")

	playbook := playbookHeader + `
  - name: Fetch file with flat destination
    fetch:
      src: /tmp/goansible-test-fetch-flat
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch")
	}

	data, err := os.ReadFile(localDest)
	if err != nil {
		t.Fatalf("Failed to read fetched file at %s: %v", localDest, err)
	}
	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}

	client.Run("rm -f " + remoteFile)
}

func TestPlaybook_FetchFlatToDir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/goansible-test-fetch-flatdir"
	content := "flat dir fetch"

	client.Run("echo -n '" + content + "' > " + remoteFile)

	localDir := t.TempDir() + "/"

	playbook := playbookHeader + `
  - name: Fetch file with flat to directory
    fetch:
      src: /tmp/goansible-test-fetch-flatdir
      dest: ` + localDir + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch")
	}

	expectedPath := filepath.Join(strings.TrimSuffix(localDir, "/"), "goansible-test-fetch-flatdir")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read fetched file at %s: %v", expectedPath, err)
	}
	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}

	client.Run("rm -f " + remoteFile)
}

func TestPlaybook_FetchMissingFile(t *testing.T) {
	playbook := playbookHeader + `
  - name: Fetch non-existent file
    fetch:
      src: /tmp/goansible-nonexistent-file-12345
      dest: /tmp/
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for missing file")
	}
	if !strings.Contains(output, "does not exist") {
		t.Error("Expected 'does not exist' error message")
	}
}

func TestPlaybook_FetchMissingFileNoFail(t *testing.T) {
	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch non-existent file with fail_on_missing=false
    fetch:
      src: /tmp/goansible-nonexistent-file-12345
      dest: ` + localDest + `
      fail_on_missing: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Error("Expected no failure with fail_on_missing=false")
	}
	if !strings.Contains(output, "OK") {
		t.Error("Expected OK status")
	}
}

func TestPlaybook_FetchDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteDir := "/tmp/goansible-test-fetch-dir"
	client.Run("mkdir -p " + remoteDir)
	defer client.Run("rm -rf " + remoteDir)

	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch directory (should fail)
    fetch:
      src: /tmp/goansible-test-fetch-dir
      dest: ` + localDest + `
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for directory fetch")
	}
	if !strings.Contains(output, "does not support directories") {
		t.Error("Expected directory error message")
	}
}
