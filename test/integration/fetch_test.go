//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybook_FetchBasic(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/dibra-test-fetch-src"
	content := "fetch test content 123"

	client.Run("echo -n '" + content + "' > " + remoteFile)

	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch file from remote
    fetch:
      src: /tmp/dibra-test-fetch-src
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

	remoteFile := "/tmp/dibra-test-fetch-flat"
	content := "flat fetch content"

	client.Run("echo -n '" + content + "' > " + remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "fetched-file.txt")

	playbook := playbookHeader + `
  - name: Fetch file with flat destination
    fetch:
      src: /tmp/dibra-test-fetch-flat
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

	remoteFile := "/tmp/dibra-test-fetch-flatdir"
	content := "flat dir fetch"

	client.Run("echo -n '" + content + "' > " + remoteFile)

	localDir := t.TempDir() + "/"

	playbook := playbookHeader + `
  - name: Fetch file with flat to directory
    fetch:
      src: /tmp/dibra-test-fetch-flatdir
      dest: ` + localDir + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch")
	}

	expectedPath := filepath.Join(strings.TrimSuffix(localDir, "/"), "dibra-test-fetch-flatdir")
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
      src: /tmp/dibra-nonexistent-file-12345
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
      src: /tmp/dibra-nonexistent-file-12345
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

	remoteDir := "/tmp/dibra-test-fetch-dir"
	client.Run("mkdir -p " + remoteDir)
	defer client.Run("rm -rf " + remoteDir)

	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch directory (should fail)
    fetch:
      src: /tmp/dibra-test-fetch-dir
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

func TestPlaybook_FetchValidateChecksumFalse(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/dibra-test-fetch-nochecksum"
	content := "no checksum validation content"

	client.Run("echo -n '" + content + "' > " + remoteFile)
	defer client.Run("rm -f " + remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "fetched-nochecksum.txt")

	playbook := playbookHeader + `
  - name: Fetch file without checksum validation
    fetch:
      src: /tmp/dibra-test-fetch-nochecksum
      dest: ` + localDest + `
      flat: true
      validate_checksum: false
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch")
	}

	data, err := os.ReadFile(localDest)
	if err != nil {
		t.Fatalf("Failed to read fetched file: %v", err)
	}
	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}
}

func TestPlaybook_FetchOverwriteExisting(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/dibra-test-fetch-overwrite"
	newContent := "updated remote content"

	client.Run("echo -n '" + newContent + "' > " + remoteFile)
	defer client.Run("rm -f " + remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "existing-file.txt")

	// Create existing local file with different content
	oldContent := "old local content"
	if err := os.WriteFile(localDest, []byte(oldContent), 0644); err != nil {
		t.Fatalf("Failed to create local file: %v", err)
	}

	playbook := playbookHeader + `
  - name: Fetch file overwriting existing
    fetch:
      src: /tmp/dibra-test-fetch-overwrite
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED when overwriting different content")
	}

	// Verify new content
	data, err := os.ReadFile(localDest)
	if err != nil {
		t.Fatalf("Failed to read fetched file: %v", err)
	}
	if string(data) != newContent {
		t.Errorf("Expected content '%s', got '%s'", newContent, string(data))
	}
}

func TestPlaybook_FetchDeepNestedPath(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Create deeply nested remote path
	remoteDir := "/tmp/dibra/deep/nested/path/to/file"
	remoteFile := remoteDir + "/deeply-nested.txt"
	content := "deeply nested content"

	client.Run("mkdir -p " + remoteDir)
	client.Run("echo -n '" + content + "' > " + remoteFile)
	defer client.Run("rm -rf /tmp/dibra")

	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch deeply nested file
    fetch:
      src: ` + remoteFile + `
      dest: ` + localDest + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch")
	}

	// Verify file exists in hostname/path structure
	expectedPath := filepath.Join(localDest, "testhost", remoteFile)
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read fetched file at %s: %v", expectedPath, err)
	}
	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}
}

func TestPlaybook_FetchSymlink(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Create a file and symlink to it
	realFile := "/tmp/dibra-test-fetch-real"
	symlinkFile := "/tmp/dibra-test-fetch-symlink"
	content := "content via symlink"

	client.Run("echo -n '" + content + "' > " + realFile)
	client.Run("ln -sf " + realFile + " " + symlinkFile)
	defer client.Run("rm -f " + realFile + " " + symlinkFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "fetched-via-symlink.txt")

	playbook := playbookHeader + `
  - name: Fetch file via symlink
    fetch:
      src: /tmp/dibra-test-fetch-symlink
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for fetch via symlink")
	}

	// Should fetch the content of the target file
	data, err := os.ReadFile(localDest)
	if err != nil {
		t.Fatalf("Failed to read fetched file: %v", err)
	}
	if string(data) != content {
		t.Errorf("Expected content '%s', got '%s'", content, string(data))
	}
}

func TestPlaybook_FetchBinaryFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/dibra-test-fetch-binary"
	// Create a small binary file with various byte values
	client.Run("printf '\\x00\\x01\\x02\\xff\\xfe\\xfd' > " + remoteFile)
	defer client.Run("rm -f " + remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "fetched-binary")

	playbook := playbookHeader + `
  - name: Fetch binary file
    fetch:
      src: /tmp/dibra-test-fetch-binary
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for binary fetch")
	}

	// Verify binary content
	data, err := os.ReadFile(localDest)
	if err != nil {
		t.Fatalf("Failed to read fetched file: %v", err)
	}
	expected := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}
	if len(data) != len(expected) {
		t.Errorf("Expected %d bytes, got %d", len(expected), len(data))
	}
	for i := range expected {
		if i < len(data) && data[i] != expected[i] {
			t.Errorf("Byte %d mismatch: expected %x, got %x", i, expected[i], data[i])
		}
	}
}

func TestPlaybook_FetchMultipleFiles(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Create multiple remote files
	client.Run("echo -n 'file1 content' > /tmp/dibra-fetch-multi1.txt")
	client.Run("echo -n 'file2 content' > /tmp/dibra-fetch-multi2.txt")
	client.Run("echo -n 'file3 content' > /tmp/dibra-fetch-multi3.txt")
	defer client.Run("rm -f /tmp/dibra-fetch-multi*.txt")

	localDest := t.TempDir()

	playbook := playbookHeader + `
  - name: Fetch first file
    fetch:
      src: /tmp/dibra-fetch-multi1.txt
      dest: ` + localDest + `

  - name: Fetch second file
    fetch:
      src: /tmp/dibra-fetch-multi2.txt
      dest: ` + localDest + `

  - name: Fetch third file
    fetch:
      src: /tmp/dibra-fetch-multi3.txt
      dest: ` + localDest + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify all files exist
	for i := 1; i <= 3; i++ {
		expectedPath := filepath.Join(localDest, "testhost", "/tmp", "dibra-fetch-multi"+string(rune('0'+i))+".txt")
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("File %d should exist at %s", i, expectedPath)
		}
	}
}

func TestPlaybook_FetchLogFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Simulate log file collection - a common use case
	logFile := "/tmp/dibra-test-app.log"
	logContent := `2024-01-15 10:00:00 INFO Application started
2024-01-15 10:00:01 DEBUG Initializing components
2024-01-15 10:00:02 INFO Ready to serve requests
2024-01-15 10:05:00 ERROR Connection timeout
2024-01-15 10:05:01 WARN Retrying connection`

	client.Run("cat > " + logFile + " << 'EOF'\n" + logContent + "\nEOF")
	defer client.Run("rm -f " + logFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "collected-app.log")

	playbook := playbookHeader + `
  - name: Collect application log
    fetch:
      src: /tmp/dibra-test-app.log
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify log content
	data, err := os.ReadFile(localDest)
	if err != nil {
		t.Fatalf("Failed to read fetched log: %v", err)
	}
	if !strings.Contains(string(data), "ERROR Connection timeout") {
		t.Error("Log should contain error message")
	}
	if !strings.Contains(string(data), "Application started") {
		t.Error("Log should contain startup message")
	}
}

func TestPlaybook_FetchConfigBackup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Simulate config file backup - another common use case
	configFile := "/tmp/dibra-test-nginx.conf"
	configContent := `server {
    listen 80;
    server_name example.com;

    location / {
        proxy_pass http://localhost:8080;
    }
}`

	client.Run("cat > " + configFile + " << 'EOF'\n" + configContent + "\nEOF")
	defer client.Run("rm -f " + configFile)

	localDir := t.TempDir()

	playbook := playbookHeader + `
  - name: Backup nginx config before changes
    fetch:
      src: /tmp/dibra-test-nginx.conf
      dest: ` + localDir + `
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for config backup")
	}

	// Verify config was backed up with hostname structure
	expectedPath := filepath.Join(localDir, "testhost", configFile)
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read backed up config: %v", err)
	}
	if !strings.Contains(string(data), "proxy_pass") {
		t.Error("Config backup should contain proxy_pass directive")
	}
}

func TestPlaybook_FetchLargeFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/dibra-test-fetch-large"
	// Create a ~100KB file
	client.Run("dd if=/dev/urandom of=" + remoteFile + " bs=1024 count=100 2>/dev/null")
	defer client.Run("rm -f " + remoteFile)

	// Get remote file size
	sizeStr := remoteExec(t, client, "stat -c %s "+remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "large-file")

	playbook := playbookHeader + `
  - name: Fetch large file
    fetch:
      src: /tmp/dibra-test-fetch-large
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for large file fetch")
	}

	// Verify file size matches
	info, err := os.Stat(localDest)
	if err != nil {
		t.Fatalf("Failed to stat fetched file: %v", err)
	}

	expectedSize := strings.TrimSpace(sizeStr)
	actualSize := fmt.Sprintf("%d", info.Size())
	if actualSize != expectedSize {
		t.Errorf("Size mismatch: expected %s, got %s", expectedSize, actualSize)
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_FetchEmptyFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	remoteFile := "/tmp/dibra-test-fetch-empty"
	client.Run("touch " + remoteFile)
	defer client.Run("rm -f " + remoteFile)

	localDir := t.TempDir()
	localDest := filepath.Join(localDir, "empty-file")

	playbook := playbookHeader + `
  - name: Fetch empty file
    fetch:
      src: /tmp/dibra-test-fetch-empty
      dest: ` + localDest + `
      flat: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for empty file fetch")
	}

	// Verify file exists and is empty
	info, err := os.Stat(localDest)
	if err != nil {
		t.Fatalf("Failed to stat fetched file: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("Expected empty file, got size %d", info.Size())
	}
}
