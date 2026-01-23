//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_FileDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/goansible-test-dir"
	client.Run("rm -rf " + testDir)

	playbook := playbookHeader + `
  - name: Create directory
    file:
      path: /tmp/goansible-test-dir
      state: directory
      mode: "0755"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for directory creation")
	}

	// Verify directory exists
	if !remoteDirExists(t, client, testDir) {
		t.Error("Directory should exist")
	}

	// Verify permissions
	mode := remoteFileMode(t, client, testDir)
	if mode != "755" {
		t.Errorf("Expected mode 755, got %s", mode)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -rf " + testDir)
}

func TestPlaybook_FileTouch(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-touch"
	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Touch file
    file:
      path: /tmp/goansible-test-touch
      state: touch
      mode: "0644"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for touch")
	}

	// Verify file exists
	if !remoteFileExists(t, client, testFile) {
		t.Error("File should exist")
	}

	// Verify permissions
	mode := remoteFileMode(t, client, testFile)
	if mode != "644" {
		t.Errorf("Expected mode 644, got %s", mode)
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileSymlink(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/goansible-test-src"
	linkFile := "/tmp/goansible-test-link"

	client.Run("rm -f " + srcFile + " " + linkFile)
	client.Run("touch " + srcFile)

	playbook := playbookHeader + `
  - name: Create symlink
    file:
      path: /tmp/goansible-test-link
      src: /tmp/goansible-test-src
      state: link
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for symlink creation")
	}

	// Verify symlink exists
	if !remoteIsSymlink(t, client, linkFile) {
		t.Error("Should be a symlink")
	}

	// Verify symlink target
	target := remoteSymlinkTarget(t, client, linkFile)
	if target != srcFile {
		t.Errorf("Expected target %s, got %s", srcFile, target)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + srcFile + " " + linkFile)
}

func TestPlaybook_FileAbsent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/goansible-test-absent"
	client.Run("touch " + testFile)

	playbook := playbookHeader + `
  - name: Delete file
    file:
      path: /tmp/goansible-test-absent
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for file deletion")
	}

	// Verify file is gone
	if remoteFileExists(t, client, testFile) {
		t.Error("File should be deleted")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}
