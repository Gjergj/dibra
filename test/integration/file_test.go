//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPlaybook_FileDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-test-dir"
	client.Run("rm -rf " + testDir)

	playbook := playbookHeader + `
  - name: Create directory
    file:
      path: /tmp/dibra-test-dir
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

	testFile := "/tmp/dibra-test-touch"
	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Touch file
    file:
      path: /tmp/dibra-test-touch
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

	srcFile := "/tmp/dibra-test-src"
	linkFile := "/tmp/dibra-test-link"

	client.Run("rm -f " + srcFile + " " + linkFile)
	client.Run("touch " + srcFile)

	playbook := playbookHeader + `
  - name: Create symlink
    file:
      path: /tmp/dibra-test-link
      src: /tmp/dibra-test-src
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

	testFile := "/tmp/dibra-test-absent"
	client.Run("touch " + testFile)

	playbook := playbookHeader + `
  - name: Delete file
    file:
      path: /tmp/dibra-test-absent
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

func TestPlaybook_FileAbsentDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-test-absent-dir"
	client.Run("rm -rf " + testDir)
	client.Run("mkdir -p " + testDir + "/subdir")
	client.Run("touch " + testDir + "/file.txt")
	client.Run("touch " + testDir + "/subdir/nested.txt")

	playbook := playbookHeader + `
  - name: Delete directory recursively
    file:
      path: /tmp/dibra-test-absent-dir
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for directory deletion")
	}

	// Verify directory is gone
	if remoteDirExists(t, client, testDir) {
		t.Error("Directory should be deleted")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

func TestPlaybook_FileDirectoryWithOwnership(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-test-dir-owner"
	client.Run("rm -rf " + testDir)

	playbook := playbookHeader + `
  - name: Create directory with ownership
    file:
      path: /tmp/dibra-test-dir-owner
      state: directory
      mode: "0750"
      owner: nobody
      group: nogroup
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
	if mode != "750" {
		t.Errorf("Expected mode 750, got %s", mode)
	}

	// Verify ownership
	owner := remoteFileOwner(t, client, testDir)
	if owner != "nobody" {
		t.Errorf("Expected owner nobody, got %s", owner)
	}

	group := remoteFileGroup(t, client, testDir)
	if group != "nogroup" {
		t.Errorf("Expected group nogroup, got %s", group)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -rf " + testDir)
}

func TestPlaybook_FileNestedDirectory(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-test-nested/level1/level2/level3"
	client.Run("rm -rf /tmp/dibra-test-nested")

	playbook := playbookHeader + `
  - name: Create nested directory
    file:
      path: /tmp/dibra-test-nested/level1/level2/level3
      state: directory
      mode: "0755"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for nested directory creation")
	}

	// Verify all directories exist
	if !remoteDirExists(t, client, testDir) {
		t.Error("Nested directory should exist")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -rf /tmp/dibra-test-nested")
}

func TestPlaybook_FileDirectoryRecurse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-test-recurse"
	client.Run("rm -rf " + testDir)
	client.Run("mkdir -p " + testDir + "/subdir")
	client.Run("touch " + testDir + "/file1.txt")
	client.Run("touch " + testDir + "/subdir/file2.txt")
	client.Run("chmod 777 " + testDir + " " + testDir + "/subdir " + testDir + "/file1.txt " + testDir + "/subdir/file2.txt")

	playbook := playbookHeader + `
  - name: Set recursive permissions
    file:
      path: /tmp/dibra-test-recurse
      state: directory
      mode: "0755"
      recurse: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for recursive permission change")
	}

	// Verify permissions on all items
	mode := remoteFileMode(t, client, testDir)
	if mode != "755" {
		t.Errorf("Expected mode 755 on root, got %s", mode)
	}

	mode = remoteFileMode(t, client, testDir+"/subdir")
	if mode != "755" {
		t.Errorf("Expected mode 755 on subdir, got %s", mode)
	}

	mode = remoteFileMode(t, client, testDir+"/file1.txt")
	if mode != "755" {
		t.Errorf("Expected mode 755 on file1, got %s", mode)
	}

	mode = remoteFileMode(t, client, testDir+"/subdir/file2.txt")
	if mode != "755" {
		t.Errorf("Expected mode 755 on file2, got %s", mode)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -rf " + testDir)
}

func TestPlaybook_FileHardlink(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/dibra-test-hard-src"
	linkFile := "/tmp/dibra-test-hard-link"

	client.Run("rm -f " + srcFile + " " + linkFile)
	client.Run("echo 'hardlink test' > " + srcFile)

	playbook := playbookHeader + `
  - name: Create hardlink
    file:
      path: /tmp/dibra-test-hard-link
      src: /tmp/dibra-test-hard-src
      state: hard
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for hardlink creation")
	}

	// Verify hardlink exists
	if !remoteFileExists(t, client, linkFile) {
		t.Error("Hardlink should exist")
	}

	// Verify same inode (hardlink characteristic)
	srcInode := remoteFileInode(t, client, srcFile)
	linkInode := remoteFileInode(t, client, linkFile)
	if srcInode != linkInode {
		t.Errorf("Expected same inode for hardlink, got %s != %s", srcInode, linkInode)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + srcFile + " " + linkFile)
}

func TestPlaybook_FileStateFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-state-file"
	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)
	client.Run("chmod 777 " + testFile)

	playbook := playbookHeader + `
  - name: Set file permissions
    file:
      path: /tmp/dibra-test-state-file
      state: file
      mode: "0644"
      owner: nobody
      group: nogroup
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for file attribute change")
	}

	// Verify permissions
	mode := remoteFileMode(t, client, testFile)
	if mode != "644" {
		t.Errorf("Expected mode 644, got %s", mode)
	}

	// Verify ownership
	owner := remoteFileOwner(t, client, testFile)
	if owner != "nobody" {
		t.Errorf("Expected owner nobody, got %s", owner)
	}

	group := remoteFileGroup(t, client, testFile)
	if group != "nogroup" {
		t.Errorf("Expected group nogroup, got %s", group)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileSymlinkForce(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/dibra-test-force-src"
	linkFile := "/tmp/dibra-test-force-link"

	client.Run("rm -f " + srcFile + " " + linkFile)
	client.Run("touch " + srcFile)
	client.Run("echo 'existing file' > " + linkFile) // Create regular file

	playbook := playbookHeader + `
  - name: Force create symlink over existing file
    file:
      path: /tmp/dibra-test-force-link
      src: /tmp/dibra-test-force-src
      state: link
      force: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for forced symlink creation")
	}

	// Verify it's now a symlink
	if !remoteIsSymlink(t, client, linkFile) {
		t.Error("Should be a symlink")
	}

	// Verify symlink target
	target := remoteSymlinkTarget(t, client, linkFile)
	if target != srcFile {
		t.Errorf("Expected target %s, got %s", srcFile, target)
	}

	// Cleanup
	client.Run("rm -f " + srcFile + " " + linkFile)
}

func TestPlaybook_FileSymlinkReplace(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile1 := "/tmp/dibra-test-replace-src1"
	srcFile2 := "/tmp/dibra-test-replace-src2"
	linkFile := "/tmp/dibra-test-replace-link"

	client.Run("rm -f " + srcFile1 + " " + srcFile2 + " " + linkFile)
	client.Run("touch " + srcFile1)
	client.Run("touch " + srcFile2)
	client.Run("ln -s " + srcFile1 + " " + linkFile)

	// Replace symlink pointing to src1 with symlink pointing to src2
	playbook := playbookHeader + `
  - name: Replace symlink target
    file:
      path: /tmp/dibra-test-replace-link
      src: /tmp/dibra-test-replace-src2
      state: link
      force: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for symlink replacement")
	}

	// Verify new target
	target := remoteSymlinkTarget(t, client, linkFile)
	if target != srcFile2 {
		t.Errorf("Expected target %s, got %s", srcFile2, target)
	}

	// Cleanup
	client.Run("rm -f " + srcFile1 + " " + srcFile2 + " " + linkFile)
}

func TestPlaybook_FileTouchExisting(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-touch-existing"
	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)

	// Get initial mtime
	initialMtime := remoteFileMtime(t, client, testFile)

	// Sleep to ensure mtime changes
	client.Run("sleep 1")

	playbook := playbookHeader + `
  - name: Touch existing file
    file:
      path: /tmp/dibra-test-touch-existing
      state: touch
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for touching existing file")
	}

	// Verify mtime changed
	newMtime := remoteFileMtime(t, client, testFile)
	if newMtime == initialMtime {
		t.Error("Expected mtime to change after touch")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileTouchWithMode(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-touch-mode"
	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Touch file with specific mode
    file:
      path: /tmp/dibra-test-touch-mode
      state: touch
      mode: "0600"
      owner: nobody
      group: nogroup
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for touch with mode")
	}

	// Verify file exists
	if !remoteFileExists(t, client, testFile) {
		t.Error("File should exist")
	}

	// Verify permissions
	mode := remoteFileMode(t, client, testFile)
	if mode != "600" {
		t.Errorf("Expected mode 600, got %s", mode)
	}

	// Verify ownership
	owner := remoteFileOwner(t, client, testFile)
	if owner != "nobody" {
		t.Errorf("Expected owner nobody, got %s", owner)
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileTouchCreatesParentDirs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-touch-parent/subdir/file.txt"
	client.Run("rm -rf /tmp/dibra-test-touch-parent")

	playbook := playbookHeader + `
  - name: Touch file in nested directory
    file:
      path: /tmp/dibra-test-touch-parent/subdir/file.txt
      state: touch
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for touch with parent creation")
	}

	// Verify file exists
	if !remoteFileExists(t, client, testFile) {
		t.Error("File should exist")
	}

	// Verify parent directories exist
	if !remoteDirExists(t, client, "/tmp/dibra-test-touch-parent/subdir") {
		t.Error("Parent directory should exist")
	}

	// Cleanup
	client.Run("rm -rf /tmp/dibra-test-touch-parent")
}

func TestPlaybook_FileStateFileNonExistent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-nonexistent"
	client.Run("rm -f " + testFile)

	playbook := playbookHeader + `
  - name: Modify non-existent file
    file:
      path: /tmp/dibra-test-nonexistent
      state: file
      mode: "0644"
`
	output := runPlaybook(t, playbook)

	// Should fail because file doesn't exist
	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "does not exist") {
		t.Error("Expected FAILED for non-existent file with state=file")
	}
}

func TestPlaybook_FileDirectoryExistsAsFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testPath := "/tmp/dibra-test-dir-as-file"
	client.Run("rm -rf " + testPath)
	client.Run("touch " + testPath) // Create as file

	playbook := playbookHeader + `
  - name: Create directory where file exists
    file:
      path: /tmp/dibra-test-dir-as-file
      state: directory
`
	output := runPlaybook(t, playbook)

	// Should fail because path exists as file
	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "not a directory") {
		t.Error("Expected FAILED when trying to create directory where file exists")
	}

	// Cleanup
	client.Run("rm -f " + testPath)
}

func TestPlaybook_FileSymlinkMissingSrc(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	linkFile := "/tmp/dibra-test-link-no-src"
	client.Run("rm -f " + linkFile)

	playbook := playbookHeader + `
  - name: Create symlink without src
    file:
      path: /tmp/dibra-test-link-no-src
      state: link
`
	output := runPlaybook(t, playbook)

	// Should fail because src is required
	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "src is required") {
		t.Error("Expected FAILED when src is missing for symlink")
	}
}

func TestPlaybook_FileHardlinkMissingSrc(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	linkFile := "/tmp/dibra-test-hard-no-src"
	client.Run("rm -f " + linkFile)

	playbook := playbookHeader + `
  - name: Create hardlink without src
    file:
      path: /tmp/dibra-test-hard-no-src
      state: hard
`
	output := runPlaybook(t, playbook)

	// Should fail because src is required
	if !strings.Contains(output, "FAILED") && !strings.Contains(output, "src is required") {
		t.Error("Expected FAILED when src is missing for hardlink")
	}
}

func TestPlaybook_FileModeChange(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-mode-change"
	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)
	client.Run("chmod 644 " + testFile)

	// Change mode to 755
	playbook := playbookHeader + `
  - name: Change file mode
    file:
      path: /tmp/dibra-test-mode-change
      state: file
      mode: "0755"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for mode change")
	}

	mode := remoteFileMode(t, client, testFile)
	if mode != "755" {
		t.Errorf("Expected mode 755, got %s", mode)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileOwnerChange(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-owner-change"
	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)

	// Change owner to nobody
	playbook := playbookHeader + `
  - name: Change file owner
    file:
      path: /tmp/dibra-test-owner-change
      state: file
      owner: nobody
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for owner change")
	}

	owner := remoteFileOwner(t, client, testFile)
	if owner != "nobody" {
		t.Errorf("Expected owner nobody, got %s", owner)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileGroupChange(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-group-change"
	client.Run("rm -f " + testFile)
	client.Run("touch " + testFile)

	// Change group to nogroup
	playbook := playbookHeader + `
  - name: Change file group
    file:
      path: /tmp/dibra-test-group-change
      state: file
      group: nogroup
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for group change")
	}

	group := remoteFileGroup(t, client, testFile)
	if group != "nogroup" {
		t.Errorf("Expected group nogroup, got %s", group)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -f " + testFile)
}

func TestPlaybook_FileSymlinkToNonExistentTarget(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/dibra-nonexistent-target"
	linkFile := "/tmp/dibra-test-dangling-link"

	client.Run("rm -f " + srcFile + " " + linkFile)

	// Create dangling symlink (target doesn't exist)
	playbook := playbookHeader + `
  - name: Create symlink to non-existent target
    file:
      path: /tmp/dibra-test-dangling-link
      src: /tmp/dibra-nonexistent-target
      state: link
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for symlink creation")
	}

	// Verify symlink exists (even if dangling)
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
	client.Run("rm -f " + linkFile)
}

func TestPlaybook_FileHardlinkForce(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/dibra-test-hard-force-src"
	linkFile := "/tmp/dibra-test-hard-force-link"

	client.Run("rm -f " + srcFile + " " + linkFile)
	client.Run("echo 'source' > " + srcFile)
	client.Run("echo 'existing' > " + linkFile) // Create regular file

	playbook := playbookHeader + `
  - name: Force create hardlink over existing file
    file:
      path: /tmp/dibra-test-hard-force-link
      src: /tmp/dibra-test-hard-force-src
      state: hard
      force: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for forced hardlink creation")
	}

	// Verify same inode
	srcInode := remoteFileInode(t, client, srcFile)
	linkInode := remoteFileInode(t, client, linkFile)
	if srcInode != linkInode {
		t.Errorf("Expected same inode for hardlink, got %s != %s", srcInode, linkInode)
	}

	// Cleanup
	client.Run("rm -f " + srcFile + " " + linkFile)
}

func TestPlaybook_FileDirectoryRecurseOwnership(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testDir := "/tmp/dibra-test-recurse-owner"
	client.Run("rm -rf " + testDir)
	client.Run("mkdir -p " + testDir + "/subdir")
	client.Run("touch " + testDir + "/file1.txt")
	client.Run("touch " + testDir + "/subdir/file2.txt")

	playbook := playbookHeader + `
  - name: Set recursive ownership
    file:
      path: /tmp/dibra-test-recurse-owner
      state: directory
      owner: nobody
      group: nogroup
      recurse: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for recursive ownership change")
	}

	// Verify ownership on all items
	owner := remoteFileOwner(t, client, testDir)
	if owner != "nobody" {
		t.Errorf("Expected owner nobody on root, got %s", owner)
	}

	owner = remoteFileOwner(t, client, testDir+"/file1.txt")
	if owner != "nobody" {
		t.Errorf("Expected owner nobody on file1, got %s", owner)
	}

	owner = remoteFileOwner(t, client, testDir+"/subdir/file2.txt")
	if owner != "nobody" {
		t.Errorf("Expected owner nobody on file2, got %s", owner)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	// Cleanup
	client.Run("rm -rf " + testDir)
}

func TestPlaybook_FileSymlinkCreatesParentDirs(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	srcFile := "/tmp/dibra-symlink-parent-src"
	linkFile := "/tmp/dibra-symlink-parent/subdir/link"

	client.Run("rm -rf /tmp/dibra-symlink-parent")
	client.Run("rm -f " + srcFile)
	client.Run("touch " + srcFile)

	playbook := playbookHeader + `
  - name: Create symlink in nested directory
    file:
      path: /tmp/dibra-symlink-parent/subdir/link
      src: /tmp/dibra-symlink-parent-src
      state: link
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for symlink with parent creation")
	}

	// Verify symlink exists
	if !remoteIsSymlink(t, client, linkFile) {
		t.Error("Should be a symlink")
	}

	// Verify parent directories exist
	if !remoteDirExists(t, client, "/tmp/dibra-symlink-parent/subdir") {
		t.Error("Parent directory should exist")
	}

	// Cleanup
	client.Run("rm -rf /tmp/dibra-symlink-parent " + srcFile)
}

func TestPlaybook_FileAbsentIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	testFile := "/tmp/dibra-test-absent-idem"
	client.Run("rm -f " + testFile) // Ensure doesn't exist

	playbook := playbookHeader + `
  - name: Delete non-existent file
    file:
      path: /tmp/dibra-test-absent-idem
      state: absent
`
	// Should not report changed when file doesn't exist
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when deleting non-existent file")
	}
}
