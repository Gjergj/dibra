//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_CopyContent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-content"
	expectedContent := "Hello from GoAnsible!"

	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Copy content to file
    copy:
      content: "Hello from GoAnsible!"
      dest: /tmp/goansible-test-content
      mode: "0644"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for content copy")
	}

	// Verify file exists with correct content
	content := remoteFileContent(t, client, testFile)
	if content != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, content)
	}

	// Verify permissions
	mode := remoteFileMode(t, client, testFile)
	if mode != "644" {
		t.Errorf("Expected mode 644, got %s", mode)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_CopyRemoteSrc(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/goansible-test-remote-src"
	destFile := "/tmp/goansible-test-remote-dest"
	content := "remote source content"

	client.Run("rm -f " + srcFile + " " + destFile)
	client.Run("echo -n '" + content + "' > " + srcFile)

	playbook := playbookHeader + `
  - name: Copy remote file
    copy:
      src: /tmp/goansible-test-remote-src
      dest: /tmp/goansible-test-remote-dest
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for remote copy")
	}

	// Verify destination has same content
	destContent := remoteFileContent(t, client, destFile)
	if destContent != content {
		t.Errorf("Expected content '%s', got '%s'", content, destContent)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + srcFile + " " + destFile)
}

func TestPlaybook_CopyWithBackup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-backup"
	client.Run("rm -f " + testFile + "*")
	client.Run("echo 'original' > " + testFile)

	playbook := playbookHeader + `
  - name: Copy with backup
    copy:
      content: "new content"
      dest: /tmp/goansible-test-backup
      backup: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for copy with backup")
	}

	// Verify new content
	content := remoteFileContent(t, client, testFile)
	if content != "new content" {
		t.Errorf("Expected 'new content', got '%s'", content)
	}

	// Verify backup exists
	backupCheck := remoteExec(t, client, "ls "+testFile+".*~ 2>/dev/null | wc -l")
	if backupCheck == "0" {
		t.Error("Backup file should exist")
	}

	// Cleanup
	client.Run("rm -f " + testFile + "*")
}
