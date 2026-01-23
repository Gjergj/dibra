//go:build integration

package integration

import (
	"os"
	"path/filepath"
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

func TestPlaybook_CopyLocalFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Create a local temp file to copy
	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "test-local-file.txt")
	localContent := "content from local file"
	if err := os.WriteFile(localFile, []byte(localContent), 0644); err != nil {
		t.Fatalf("Failed to create local file: %v", err)
	}

	destFile := "/tmp/goansible-test-local-copy"
	client.Run("rm -f " + destFile)

	playbook := playbookHeader + `
  - name: Copy local file to remote
    copy:
      src: ` + localFile + `
      dest: /tmp/goansible-test-local-copy
      mode: "0644"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for local file copy")
	}

	// Verify file exists with correct content
	content := remoteFileContent(t, client, destFile)
	if content != localContent {
		t.Errorf("Expected content '%s', got '%s'", localContent, content)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + destFile)
}

func TestPlaybook_CopyOwnerGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-owner"
	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Copy with owner and group
    copy:
      content: "owned content"
      dest: /tmp/goansible-test-owner
      mode: "0640"
      owner: root
      group: root
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for copy with owner")
	}

	// Verify owner is root
	owner := remoteExec(t, client, "stat -c %U "+testFile)
	if owner != "root" {
		t.Errorf("Expected owner 'root', got '%s'", owner)
	}

	// Verify group is root
	group := remoteExec(t, client, "stat -c %G "+testFile)
	if group != "root" {
		t.Errorf("Expected group 'root', got '%s'", group)
	}

	// Verify mode
	mode := remoteFileMode(t, client, testFile)
	if mode != "640" {
		t.Errorf("Expected mode 640, got %s", mode)
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_CopyForce(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-force"
	client.Run("rm -f " + testFile)
	// Create file with same content
	client.Run("echo -n 'same content' > " + testFile)

	// First verify without force - should not change
	playbookNoForce := playbookHeader + `
  - name: Copy without force
    copy:
      content: "same content"
      dest: /tmp/goansible-test-force
`
	output := runPlaybook(t, playbookNoForce)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change without force when content matches")
	}

	// Now with force - should change even though content matches
	playbookForce := playbookHeader + `
  - name: Copy with force
    copy:
      content: "same content"
      dest: /tmp/goansible-test-force
      force: true
`
	output = runPlaybook(t, playbookForce)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED with force=true")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_CopyToDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/goansible-test-srcfile.txt"
	destDir := "/tmp/goansible-test-destdir"

	client.Run("rm -rf " + destDir)
	client.Run("mkdir -p " + destDir)
	client.Run("echo -n 'source content' > " + srcFile)

	playbook := playbookHeader + `
  - name: Copy file to directory
    copy:
      src: /tmp/goansible-test-srcfile.txt
      dest: /tmp/goansible-test-destdir/
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for copy to directory")
	}

	// File should be copied with original filename
	expectedFile := destDir + "/goansible-test-srcfile.txt"
	if !remoteFileExists(t, client, expectedFile) {
		t.Error("File should exist in destination directory")
	}

	content := remoteFileContent(t, client, expectedFile)
	if content != "source content" {
		t.Errorf("Expected 'source content', got '%s'", content)
	}

	// Cleanup
	client.Run("rm -rf " + destDir + " " + srcFile)
}

func TestPlaybook_CopyCreateParentDirs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Nested path that doesn't exist
	destFile := "/tmp/goansible-test-parent/subdir/nested/file.txt"
	client.Run("rm -rf /tmp/goansible-test-parent")

	playbook := playbookHeader + `
  - name: Copy creating parent directories
    copy:
      content: "nested content"
      dest: /tmp/goansible-test-parent/subdir/nested/file.txt
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for copy with parent creation")
	}

	// Verify file exists
	if !remoteFileExists(t, client, destFile) {
		t.Error("File should exist in nested directory")
	}

	content := remoteFileContent(t, client, destFile)
	if content != "nested content" {
		t.Errorf("Expected 'nested content', got '%s'", content)
	}

	// Cleanup
	client.Run("rm -rf /tmp/goansible-test-parent")
}

func TestPlaybook_CopyMultilineContent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-multiline"
	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Copy multiline content
    copy:
      content: |
        line 1
        line 2
        line 3
      dest: /tmp/goansible-test-multiline
      mode: "0644"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for multiline content")
	}

	// Verify content has multiple lines
	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "line 1") || !strings.Contains(content, "line 2") {
		t.Errorf("Expected multiline content, got '%s'", content)
	}

	lineCount := remoteExec(t, client, "wc -l < "+testFile)
	if lineCount != "3" {
		t.Errorf("Expected 3 lines, got %s", lineCount)
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_CopyDirectoryRemoteSrc(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcDir := "/tmp/goansible-test-srcdir"
	destDir := "/tmp/goansible-test-destdir-copy"

	// Create source directory with files
	client.Run("rm -rf " + srcDir + " " + destDir)
	client.Run("mkdir -p " + srcDir + "/subdir")
	client.Run("echo 'file1' > " + srcDir + "/file1.txt")
	client.Run("echo 'file2' > " + srcDir + "/subdir/file2.txt")

	playbook := playbookHeader + `
  - name: Copy directory recursively
    copy:
      src: /tmp/goansible-test-srcdir/
      dest: /tmp/goansible-test-destdir-copy
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for directory copy")
	}

	// Verify files were copied
	if !remoteFileExists(t, client, destDir+"/file1.txt") {
		t.Error("file1.txt should exist in destination")
	}
	if !remoteFileExists(t, client, destDir+"/subdir/file2.txt") {
		t.Error("subdir/file2.txt should exist in destination")
	}

	// Verify content
	content := remoteFileContent(t, client, destDir+"/file1.txt")
	if strings.TrimSpace(content) != "file1" {
		t.Errorf("Expected 'file1', got '%s'", content)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	// Cleanup
	client.Run("rm -rf " + srcDir + " " + destDir)
}

func TestPlaybook_CopyConfigFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-config.yaml"
	client.Run("rm -f " + testFile)

	// Simulates a common use case: deploying config files
	playbook := playbookHeader + `
  - name: Deploy application config
    copy:
      content: |
        server:
          host: 0.0.0.0
          port: 8080
        database:
          host: localhost
          port: 5432
          name: myapp
        logging:
          level: info
          format: json
      dest: /tmp/goansible-test-config.yaml
      mode: "0644"
      owner: root
      group: root
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for config deployment")
	}

	// Verify config content
	content := remoteFileContent(t, client, testFile)
	if !strings.Contains(content, "port: 8080") {
		t.Error("Config should contain port setting")
	}
	if !strings.Contains(content, "level: info") {
		t.Error("Config should contain logging level")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}
