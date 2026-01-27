//go:build integration

package integration

import (
	"strings"
	"testing"
)

// ====================
// TAR ARCHIVE TESTS
// ====================

func TestPlaybook_UnarchiveTar(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-tar"
	archiveFile := "/tmp/goansible-test.tar"

	// Cleanup
	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/tar-source")
	client.Run("echo 'file1 content' > /tmp/tar-source/file1.txt")
	client.Run("echo 'file2 content' > /tmp/tar-source/file2.txt")
	client.Run("mkdir -p /tmp/tar-source/subdir")
	client.Run("echo 'nested content' > /tmp/tar-source/subdir/nested.txt")
	client.Run("tar -cf " + archiveFile + " -C /tmp/tar-source .")
	client.Run("rm -rf /tmp/tar-source")

	playbook := playbookHeader + `
  - name: Unarchive tar file
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED for tar extraction, got:\n%s", output)
	}

	// Verify files extracted
	if !remoteFileExists(t, client, destDir+"/file1.txt") {
		t.Error("file1.txt should exist")
	}
	if !remoteFileExists(t, client, destDir+"/file2.txt") {
		t.Error("file2.txt should exist")
	}
	if !remoteFileExists(t, client, destDir+"/subdir/nested.txt") {
		t.Error("subdir/nested.txt should exist")
	}

	// Verify content
	content := remoteFileContent(t, client, destDir+"/file1.txt")
	if !strings.Contains(content, "file1 content") {
		t.Errorf("Expected 'file1 content', got '%s'", content)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	// Cleanup
	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveTarGz(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-tgz"
	archiveFile := "/tmp/goansible-test.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/tgz-source")
	client.Run("echo 'gzip content' > /tmp/tgz-source/gzipfile.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/tgz-source .")
	client.Run("rm -rf /tmp/tgz-source")

	playbook := playbookHeader + `
  - name: Unarchive tar.gz file
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED for tar.gz extraction, got:\n%s", output)
	}

	// Verify extraction
	if !remoteFileExists(t, client, destDir+"/gzipfile.txt") {
		t.Error("gzipfile.txt should exist")
	}

	content := remoteFileContent(t, client, destDir+"/gzipfile.txt")
	if !strings.Contains(content, "gzip content") {
		t.Errorf("Expected 'gzip content', got '%s'", content)
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveTarBz2(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-bz2"
	archiveFile := "/tmp/goansible-test.tar.bz2"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Check if bzip2 is available
	_, _, err := client.Run("which bzip2")
	if err != nil {
		t.Skip("bzip2 not available, skipping tar.bz2 test")
	}

	// Create test archive
	client.Run("mkdir -p /tmp/bz2-source")
	client.Run("echo 'bzip2 content' > /tmp/bz2-source/bz2file.txt")
	client.Run("tar -cjf " + archiveFile + " -C /tmp/bz2-source .")
	client.Run("rm -rf /tmp/bz2-source")

	playbook := playbookHeader + `
  - name: Unarchive tar.bz2 file
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED for tar.bz2 extraction, got:\n%s", output)
	}

	if !remoteFileExists(t, client, destDir+"/bz2file.txt") {
		t.Error("bz2file.txt should exist")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveTarXz(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-xz"
	archiveFile := "/tmp/goansible-test.tar.xz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Check if xz is available
	_, stderr, err := client.Run("which xz")
	if err != nil {
		t.Skip("xz not available, skipping tar.xz test")
	}
	_ = stderr

	// Create test archive
	client.Run("mkdir -p /tmp/xz-source")
	client.Run("echo 'xz content' > /tmp/xz-source/xzfile.txt")
	client.Run("tar -cJf " + archiveFile + " -C /tmp/xz-source .")
	client.Run("rm -rf /tmp/xz-source")

	playbook := playbookHeader + `
  - name: Unarchive tar.xz file
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED for tar.xz extraction, got:\n%s", output)
	}

	if !remoteFileExists(t, client, destDir+"/xzfile.txt") {
		t.Error("xzfile.txt should exist")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// ZIP ARCHIVE TESTS
// ====================

func TestPlaybook_UnarchiveZip(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-zip"
	archiveFile := "/tmp/goansible-test.zip"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Ensure zip is installed
	client.Run("apt-get update && apt-get install -y zip unzip 2>/dev/null || true")

	// Create test archive
	client.Run("mkdir -p /tmp/zip-source")
	client.Run("echo 'zip content 1' > /tmp/zip-source/zipfile1.txt")
	client.Run("echo 'zip content 2' > /tmp/zip-source/zipfile2.txt")
	client.Run("mkdir -p /tmp/zip-source/subdir")
	client.Run("echo 'nested zip' > /tmp/zip-source/subdir/nested.txt")
	client.Run("cd /tmp/zip-source && zip -r " + archiveFile + " .")
	client.Run("rm -rf /tmp/zip-source")

	playbook := playbookHeader + `
  - name: Unarchive zip file
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED for zip extraction, got:\n%s", output)
	}

	// Verify files extracted
	if !remoteFileExists(t, client, destDir+"/zipfile1.txt") {
		t.Error("zipfile1.txt should exist")
	}
	if !remoteFileExists(t, client, destDir+"/zipfile2.txt") {
		t.Error("zipfile2.txt should exist")
	}
	if !remoteFileExists(t, client, destDir+"/subdir/nested.txt") {
		t.Error("subdir/nested.txt should exist")
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// CREATES PARAMETER TESTS
// ====================

func TestPlaybook_UnarchiveCreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-creates"
	archiveFile := "/tmp/goansible-creates.tar.gz"
	createsFile := destDir + "/marker.txt"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/creates-source")
	client.Run("echo 'marker' > /tmp/creates-source/marker.txt")
	client.Run("echo 'data' > /tmp/creates-source/data.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/creates-source .")
	client.Run("rm -rf /tmp/creates-source")

	playbook := playbookHeader + `
  - name: Unarchive with creates
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      creates: ` + createsFile + `
`
	// First run - file doesn't exist, should extract
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED when creates file doesn't exist, got:\n%s", output)
	}

	// Verify marker file exists
	if !remoteFileExists(t, client, createsFile) {
		t.Error("marker.txt should exist after extraction")
	}

	// Second run - creates file exists, should skip
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no change when creates file exists")
	}

	if !strings.Contains(output, "skipped") && !strings.Contains(output, "OK") {
		t.Log("Output should indicate skipped:", output)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveCreatesNotExists(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-creates-ne"
	archiveFile := "/tmp/goansible-creates-ne.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/creates-ne-source")
	client.Run("echo 'content' > /tmp/creates-ne-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/creates-ne-source .")
	client.Run("rm -rf /tmp/creates-ne-source")

	playbook := playbookHeader + `
  - name: Unarchive with non-existent creates
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      creates: /tmp/this-file-does-not-exist-xyz
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED when creates file doesn't exist, got:\n%s", output)
	}

	if !remoteFileExists(t, client, destDir+"/file.txt") {
		t.Error("file.txt should exist after extraction")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// LIST_FILES PARAMETER TESTS
// ====================

func TestPlaybook_UnarchiveListFiles(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-list"
	archiveFile := "/tmp/goansible-list.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with known files
	client.Run("mkdir -p /tmp/list-source/subdir")
	client.Run("echo 'a' > /tmp/list-source/alpha.txt")
	client.Run("echo 'b' > /tmp/list-source/beta.txt")
	client.Run("echo 'c' > /tmp/list-source/subdir/gamma.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/list-source .")
	client.Run("rm -rf /tmp/list-source")

	playbook := playbookHeader + `
  - name: Unarchive with list_files
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      list_files: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Files should be extracted
	if !remoteFileExists(t, client, destDir+"/alpha.txt") {
		t.Error("alpha.txt should exist")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// EXCLUDE PARAMETER TESTS
// ====================

func TestPlaybook_UnarchiveExclude(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-exclude"
	archiveFile := "/tmp/goansible-exclude.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/exclude-source")
	client.Run("echo 'keep' > /tmp/exclude-source/keep.txt")
	client.Run("echo 'skip' > /tmp/exclude-source/skip.txt")
	client.Run("echo 'also keep' > /tmp/exclude-source/alsokeep.dat")
	client.Run("tar -czf " + archiveFile + " -C /tmp/exclude-source .")
	client.Run("rm -rf /tmp/exclude-source")

	playbook := playbookHeader + `
  - name: Unarchive with exclude
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      exclude:
        - "skip.txt"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// keep.txt and alsokeep.dat should exist
	if !remoteFileExists(t, client, destDir+"/keep.txt") {
		t.Error("keep.txt should exist")
	}
	if !remoteFileExists(t, client, destDir+"/alsokeep.dat") {
		t.Error("alsokeep.dat should exist")
	}

	// skip.txt should NOT exist (excluded)
	if remoteFileExists(t, client, destDir+"/skip.txt") {
		t.Error("skip.txt should NOT exist (was excluded)")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveExcludeGlob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-exclude-glob"
	archiveFile := "/tmp/goansible-exclude-glob.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/exclude-glob-source")
	client.Run("echo 'keep' > /tmp/exclude-glob-source/keep.txt")
	client.Run("echo 'skip1' > /tmp/exclude-glob-source/skip1.log")
	client.Run("echo 'skip2' > /tmp/exclude-glob-source/skip2.log")
	client.Run("tar -czf " + archiveFile + " -C /tmp/exclude-glob-source .")
	client.Run("rm -rf /tmp/exclude-glob-source")

	playbook := playbookHeader + `
  - name: Unarchive excluding all log files
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      exclude:
        - "*.log"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// keep.txt should exist
	if !remoteFileExists(t, client, destDir+"/keep.txt") {
		t.Error("keep.txt should exist")
	}

	// .log files should NOT exist
	if remoteFileExists(t, client, destDir+"/skip1.log") {
		t.Error("skip1.log should NOT exist")
	}
	if remoteFileExists(t, client, destDir+"/skip2.log") {
		t.Error("skip2.log should NOT exist")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// INCLUDE PARAMETER TESTS
// ====================

func TestPlaybook_UnarchiveInclude(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-include"
	archiveFile := "/tmp/goansible-include.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with explicit filenames (not using .)
	client.Run("mkdir -p /tmp/include-source")
	client.Run("echo 'wanted' > /tmp/include-source/wanted.txt")
	client.Run("echo 'unwanted' > /tmp/include-source/unwanted.txt")
	client.Run("echo 'other' > /tmp/include-source/other.dat")
	client.Run("cd /tmp/include-source && tar -czf " + archiveFile + " wanted.txt unwanted.txt other.dat")
	client.Run("rm -rf /tmp/include-source")

	playbook := playbookHeader + `
  - name: Unarchive with include
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      include:
        - "wanted.txt"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// wanted.txt should exist
	if !remoteFileExists(t, client, destDir+"/wanted.txt") {
		t.Error("wanted.txt should exist")
	}

	// Other files should NOT exist (not included)
	if remoteFileExists(t, client, destDir+"/unwanted.txt") {
		t.Error("unwanted.txt should NOT exist (not included)")
	}
	if remoteFileExists(t, client, destDir+"/other.dat") {
		t.Error("other.dat should NOT exist (not included)")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// KEEP_NEWER PARAMETER TESTS
// ====================

func TestPlaybook_UnarchiveKeepNewer(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-newer"
	archiveFile := "/tmp/goansible-newer.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with old content
	client.Run("mkdir -p /tmp/newer-source")
	client.Run("echo 'old content' > /tmp/newer-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/newer-source .")
	client.Run("rm -rf /tmp/newer-source")

	// Create newer file in destination
	client.Run("echo 'new content' > " + destDir + "/file.txt")
	client.Run("touch " + destDir + "/file.txt") // ensure timestamp is newer

	playbook := playbookHeader + `
  - name: Unarchive with keep_newer
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      keep_newer: true
`
	output := runPlaybook(t, playbook)
	_ = output // May or may not show changed depending on tar behavior

	// File should still have new content
	content := remoteFileContent(t, client, destDir+"/file.txt")
	if !strings.Contains(content, "new content") {
		t.Errorf("Expected 'new content' (newer file preserved), got '%s'", content)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// MODE/OWNER/GROUP TESTS
// ====================

func TestPlaybook_UnarchiveWithMode(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-mode"
	archiveFile := "/tmp/goansible-mode.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/mode-source")
	client.Run("echo 'test' > /tmp/mode-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/mode-source .")
	client.Run("rm -rf /tmp/mode-source")

	playbook := playbookHeader + `
  - name: Unarchive with mode
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      mode: "0755"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Verify mode
	mode := remoteFileMode(t, client, destDir+"/file.txt")
	if mode != "755" {
		t.Errorf("Expected mode 755, got %s", mode)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveWithOwnerGroup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-owner"
	archiveFile := "/tmp/goansible-owner.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/owner-source")
	client.Run("echo 'test' > /tmp/owner-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/owner-source .")
	client.Run("rm -rf /tmp/owner-source")

	playbook := playbookHeader + `
  - name: Unarchive with owner and group
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      owner: root
      group: root
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Verify owner
	owner := remoteFileOwner(t, client, destDir+"/file.txt")
	if owner != "root" {
		t.Errorf("Expected owner 'root', got '%s'", owner)
	}

	// Verify group
	group := remoteFileGroup(t, client, destDir+"/file.txt")
	if group != "root" {
		t.Errorf("Expected group 'root', got '%s'", group)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// IDEMPOTENCY TESTS
// ====================

func TestPlaybook_UnarchiveIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-idem"
	archiveFile := "/tmp/goansible-idem.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/idem-source")
	client.Run("echo 'idem' > /tmp/idem-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/idem-source .")
	client.Run("rm -rf /tmp/idem-source")

	playbook := playbookHeader + `
  - name: Unarchive idempotency test
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	// First run
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED on first run, got:\n%s", output)
	}

	// Second run
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	// Third run
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on third run (idempotent)")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

func TestPlaybook_UnarchiveZipIdempotent(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-zip-idem"
	archiveFile := "/tmp/goansible-zip-idem.zip"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Ensure zip is installed
	client.Run("apt-get update && apt-get install -y zip unzip 2>/dev/null || true")

	// Create test archive
	client.Run("mkdir -p /tmp/zip-idem-source")
	client.Run("echo 'idem' > /tmp/zip-idem-source/file.txt")
	client.Run("cd /tmp/zip-idem-source && zip -r " + archiveFile + " .")
	client.Run("rm -rf /tmp/zip-idem-source")

	playbook := playbookHeader + `
  - name: Unarchive zip idempotency test
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	// First run
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED on first run, got:\n%s", output)
	}

	// Second run
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// MISSING FILES TESTS
// ====================

func TestPlaybook_UnarchiveMissingSrc(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-missing"
	client.Run("mkdir -p " + destDir)

	playbook := playbookHeader + `
  - name: Unarchive missing source
    unarchive:
      src: /tmp/this-archive-does-not-exist.tar.gz
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED for missing source, got:\n%s", output)
	}

	client.Run("rm -rf " + destDir)
}

func TestPlaybook_UnarchiveMissingDest(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	archiveFile := "/tmp/goansible-missing-dest.tar.gz"

	// Create a valid archive
	client.Run("mkdir -p /tmp/missing-dest-source")
	client.Run("echo 'test' > /tmp/missing-dest-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/missing-dest-source .")
	client.Run("rm -rf /tmp/missing-dest-source")

	playbook := playbookHeader + `
  - name: Unarchive to missing destination
    unarchive:
      src: ` + archiveFile + `
      dest: /tmp/this-dir-does-not-exist-xyz
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED for missing destination, got:\n%s", output)
	}

	client.Run("rm -f " + archiveFile)
}

// ====================
// REEXTRACTION TESTS
// ====================

func TestPlaybook_UnarchiveReextractAfterDelete(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-reextract"
	archiveFile := "/tmp/goansible-reextract.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/reextract-source")
	client.Run("echo 'file1' > /tmp/reextract-source/file1.txt")
	client.Run("echo 'file2' > /tmp/reextract-source/file2.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/reextract-source .")
	client.Run("rm -rf /tmp/reextract-source")

	playbook := playbookHeader + `
  - name: Unarchive reextract test
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	// First run
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED on first run, got:\n%s", output)
	}

	// Delete one file
	client.Run("rm " + destDir + "/file1.txt")

	// Should re-extract since file is missing
	output = runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED after deleting a file")
	}

	// Verify file restored
	if !remoteFileExists(t, client, destDir+"/file1.txt") {
		t.Error("file1.txt should be restored")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// CONTENT CHANGE TESTS
// ====================

func TestPlaybook_UnarchiveContentDiffers(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-content"
	archiveFile := "/tmp/goansible-content.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/content-source")
	client.Run("echo 'original' > /tmp/content-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/content-source .")
	client.Run("rm -rf /tmp/content-source")

	playbook := playbookHeader + `
  - name: Unarchive content test
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	// First run
	output := runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED on first run, got:\n%s", output)
	}

	// Modify file content
	client.Run("echo 'modified' > " + destDir + "/file.txt")

	// Should detect content change and re-extract
	output = runPlaybook(t, playbook)
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED after modifying file content")
	}

	// Verify original content restored
	content := remoteFileContent(t, client, destDir+"/file.txt")
	if !strings.Contains(content, "original") {
		t.Errorf("Expected 'original' content after re-extraction, got '%s'", content)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// SYMLINK TESTS
// ====================

func TestPlaybook_UnarchiveSymlink(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-symlink"
	archiveFile := "/tmp/goansible-symlink.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with symlink
	client.Run("mkdir -p /tmp/symlink-source")
	client.Run("echo 'target content' > /tmp/symlink-source/target.txt")
	client.Run("ln -s target.txt /tmp/symlink-source/link.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/symlink-source .")
	client.Run("rm -rf /tmp/symlink-source")

	playbook := playbookHeader + `
  - name: Unarchive with symlink
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Verify symlink exists
	if !remoteIsSymlink(t, client, destDir+"/link.txt") {
		t.Error("link.txt should be a symlink")
	}

	// Verify symlink target
	target := remoteSymlinkTarget(t, client, destDir+"/link.txt")
	if target != "target.txt" {
		t.Errorf("Expected symlink target 'target.txt', got '%s'", target)
	}

	// Verify content accessible through symlink
	content := remoteFileContent(t, client, destDir+"/link.txt")
	if !strings.Contains(content, "target content") {
		t.Errorf("Expected 'target content' via symlink, got '%s'", content)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// DESTINATION IS SYMLINK TESTS
// ====================

func TestPlaybook_UnarchiveDestSymlinkToDir(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	realDir := "/tmp/goansible-unarchive-real-dest"
	symlinkDir := "/tmp/goansible-unarchive-sym-dest"
	archiveFile := "/tmp/goansible-symdir.tar.gz"

	client.Run("rm -rf " + realDir + " " + symlinkDir + " " + archiveFile)
	client.Run("mkdir -p " + realDir)
	client.Run("ln -s " + realDir + " " + symlinkDir)

	// Create test archive
	client.Run("mkdir -p /tmp/symdir-source")
	client.Run("echo 'content' > /tmp/symdir-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/symdir-source .")
	client.Run("rm -rf /tmp/symdir-source")

	playbook := playbookHeader + `
  - name: Unarchive to symlink directory
    unarchive:
      src: ` + archiveFile + `
      dest: ` + symlinkDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// File should exist in real directory
	if !remoteFileExists(t, client, realDir+"/file.txt") {
		t.Error("file.txt should exist in real directory")
	}

	client.Run("rm -rf " + realDir + " " + symlinkDir + " " + archiveFile)
}

// ====================
// MUTUAL EXCLUSIVITY TESTS
// ====================

func TestPlaybook_UnarchiveExcludeIncludeMutuallyExclusive(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-mutex"
	archiveFile := "/tmp/goansible-mutex.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/mutex-source")
	client.Run("echo 'test' > /tmp/mutex-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/mutex-source .")
	client.Run("rm -rf /tmp/mutex-source")

	playbook := playbookHeader + `
  - name: Unarchive with both exclude and include
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      exclude:
        - "*.log"
      include:
        - "file.txt"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED for mutually exclusive options, got:\n%s", output)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// EXTRA_OPTS TESTS
// ====================

func TestPlaybook_UnarchiveExtraOpts(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-extra"
	archiveFile := "/tmp/goansible-extra.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive
	client.Run("mkdir -p /tmp/extra-source")
	client.Run("echo 'test' > /tmp/extra-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/extra-source .")
	client.Run("rm -rf /tmp/extra-source")

	playbook := playbookHeader + `
  - name: Unarchive with extra_opts
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      extra_opts:
        - "--verbose"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	if !remoteFileExists(t, client, destDir+"/file.txt") {
		t.Error("file.txt should exist")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// ZIP-SPECIFIC TESTS
// ====================

func TestPlaybook_UnarchiveZipExclude(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-zip-exc"
	archiveFile := "/tmp/goansible-zip-exc.zip"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Ensure zip is installed
	client.Run("apt-get update && apt-get install -y zip unzip 2>/dev/null || true")

	// Create test archive
	client.Run("mkdir -p /tmp/zip-exc-source")
	client.Run("echo 'keep' > /tmp/zip-exc-source/keep.txt")
	client.Run("echo 'skip' > /tmp/zip-exc-source/skip.txt")
	client.Run("cd /tmp/zip-exc-source && zip -r " + archiveFile + " .")
	client.Run("rm -rf /tmp/zip-exc-source")

	playbook := playbookHeader + `
  - name: Unarchive zip with exclude
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
      exclude:
        - "skip.txt"
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	if !remoteFileExists(t, client, destDir+"/keep.txt") {
		t.Error("keep.txt should exist")
	}

	if remoteFileExists(t, client, destDir+"/skip.txt") {
		t.Error("skip.txt should NOT exist (was excluded)")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// UNSUPPORTED FORMAT TESTS
// ====================

func TestPlaybook_UnarchiveUnsupportedFormat(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-unsup"
	client.Run("mkdir -p " + destDir)

	// Create a file with unsupported extension
	client.Run("echo 'not an archive' > /tmp/notarchive.rar")

	playbook := playbookHeader + `
  - name: Unarchive unsupported format
    unarchive:
      src: /tmp/notarchive.rar
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED for unsupported format, got:\n%s", output)
	}

	client.Run("rm -rf " + destDir + " /tmp/notarchive.rar")
}

// ====================
// DEST IS FILE TESTS
// ====================

func TestPlaybook_UnarchiveDestIsFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destFile := "/tmp/goansible-dest-is-file"
	archiveFile := "/tmp/goansible-dest-file.tar.gz"

	client.Run("rm -f " + destFile + " " + archiveFile)
	client.Run("echo 'im a file' > " + destFile)

	// Create test archive
	client.Run("mkdir -p /tmp/dest-file-source")
	client.Run("echo 'test' > /tmp/dest-file-source/file.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/dest-file-source .")
	client.Run("rm -rf /tmp/dest-file-source")

	playbook := playbookHeader + `
  - name: Unarchive to file (should fail)
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destFile + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Errorf("Expected FAILED when dest is a file, got:\n%s", output)
	}

	client.Run("rm -f " + destFile + " " + archiveFile)
}

// ====================
// NESTED DIRECTORY TESTS
// ====================

func TestPlaybook_UnarchiveNestedDirectories(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-nested"
	archiveFile := "/tmp/goansible-nested.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with deeply nested structure
	client.Run("mkdir -p /tmp/nested-source/a/b/c/d/e")
	client.Run("echo 'deep' > /tmp/nested-source/a/b/c/d/e/deep.txt")
	client.Run("echo 'shallow' > /tmp/nested-source/shallow.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/nested-source .")
	client.Run("rm -rf /tmp/nested-source")

	playbook := playbookHeader + `
  - name: Unarchive nested directories
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Verify deeply nested file
	if !remoteFileExists(t, client, destDir+"/a/b/c/d/e/deep.txt") {
		t.Error("deep.txt should exist in nested path")
	}

	// Verify shallow file
	if !remoteFileExists(t, client, destDir+"/shallow.txt") {
		t.Error("shallow.txt should exist")
	}

	// Verify content
	content := remoteFileContent(t, client, destDir+"/a/b/c/d/e/deep.txt")
	if !strings.Contains(content, "deep") {
		t.Errorf("Expected 'deep', got '%s'", content)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// PERMISSIONS PRESERVATION TESTS
// ====================

func TestPlaybook_UnarchivePreservesPermissions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-perms"
	archiveFile := "/tmp/goansible-perms.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with specific permissions
	client.Run("mkdir -p /tmp/perms-source")
	client.Run("echo 'exec' > /tmp/perms-source/script.sh")
	client.Run("chmod 755 /tmp/perms-source/script.sh")
	client.Run("echo 'read' > /tmp/perms-source/readonly.txt")
	client.Run("chmod 444 /tmp/perms-source/readonly.txt")
	client.Run("tar -czf " + archiveFile + " --preserve-permissions -C /tmp/perms-source .")
	client.Run("rm -rf /tmp/perms-source")

	playbook := playbookHeader + `
  - name: Unarchive preserving permissions
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Verify script.sh is executable
	scriptMode := remoteFileMode(t, client, destDir+"/script.sh")
	if scriptMode != "755" {
		t.Errorf("Expected script.sh mode 755, got %s", scriptMode)
	}

	// Verify readonly.txt has read-only perms
	readonlyMode := remoteFileMode(t, client, destDir+"/readonly.txt")
	if readonlyMode != "444" {
		t.Errorf("Expected readonly.txt mode 444, got %s", readonlyMode)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// EMPTY ARCHIVE TESTS
// ====================

func TestPlaybook_UnarchiveEmptyArchive(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-empty"
	archiveFile := "/tmp/goansible-empty.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create empty archive
	client.Run("mkdir -p /tmp/empty-source")
	client.Run("tar -czf " + archiveFile + " -C /tmp/empty-source .")
	client.Run("rm -rf /tmp/empty-source")

	playbook := playbookHeader + `
  - name: Unarchive empty archive
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	// Should succeed but might not show CHANGED for empty archives
	if strings.Contains(output, "FAILED") {
		t.Errorf("Should not fail for empty archive, got:\n%s", output)
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}

// ====================
// SPECIAL CHARACTERS TESTS
// ====================

func TestPlaybook_UnarchiveSpecialCharFilenames(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destDir := "/tmp/goansible-unarchive-special"
	archiveFile := "/tmp/goansible-special.tar.gz"

	client.Run("rm -rf " + destDir + " " + archiveFile)
	client.Run("mkdir -p " + destDir)

	// Create test archive with special character filenames
	// Use a heredoc-style approach to avoid shell escaping issues
	client.Run("mkdir -p /tmp/special-source")
	client.Run("echo 'dash' > /tmp/special-source/file-with-dashes.txt")
	client.Run("echo 'underscore' > /tmp/special-source/file_with_underscores.txt")
	client.Run("echo 'dots' > /tmp/special-source/file.with.dots.txt")
	client.Run("tar -czf " + archiveFile + " -C /tmp/special-source .")
	client.Run("rm -rf /tmp/special-source")

	playbook := playbookHeader + `
  - name: Unarchive special character filenames
    unarchive:
      src: ` + archiveFile + `
      dest: ` + destDir + `
      remote_src: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Errorf("Expected CHANGED, got:\n%s", output)
	}

	// Verify files with special characters
	if !remoteFileExists(t, client, destDir+"/file-with-dashes.txt") {
		t.Error("'file-with-dashes.txt' should exist")
	}
	if !remoteFileExists(t, client, destDir+"/file_with_underscores.txt") {
		t.Error("'file_with_underscores.txt' should exist")
	}
	if !remoteFileExists(t, client, destDir+"/file.with.dots.txt") {
		t.Error("'file.with.dots.txt' should exist")
	}

	client.Run("rm -rf " + destDir + " " + archiveFile)
}
