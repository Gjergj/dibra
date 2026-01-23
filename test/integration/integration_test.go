//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/goansible/internal/ssh"
)

func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
}

const (
	testHost     = "localhost"
	testPort     = 2222
	testUser     = "root"
	testPassword = "rootpass"
)

func TestMain(m *testing.M) {
	if err := waitForSSH(30 * time.Second); err != nil {
		println("SSH not available:", err.Error())
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitForSSH(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ssh.Connect(ssh.Config{
			Host:     testHost,
			Port:     testPort,
			User:     testUser,
			Password: testPassword,
		})
		if err == nil {
			client.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return nil
}

func getClient(t *testing.T) *ssh.Client {
	client, err := ssh.Connect(ssh.Config{
		Host:     testHost,
		Port:     testPort,
		User:     testUser,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	return client
}

func runPlaybook(t *testing.T, playbook string) string {
	tmpFile := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(tmpFile, []byte(playbook), 0644); err != nil {
		t.Fatalf("Failed to write playbook: %v", err)
	}

	projectRoot := getProjectRoot()
	cmd := exec.Command("go", "run", "./cmd/controller", "-config", tmpFile, "-v")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Playbook output:\n%s", string(output))
	}
	return string(output)
}

func remoteExec(t *testing.T, client *ssh.Client, cmd string) string {
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Logf("Command failed: %s\nstdout: %s\nstderr: %s", cmd, stdout, stderr)
	}
	return strings.TrimSpace(stdout)
}

func remoteFileExists(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -e " + path)
	return err == nil
}

func remoteFileContent(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "cat "+path)
}

func remoteFileMode(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "stat -c %a "+path)
}

func remoteDirExists(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -d " + path)
	return err == nil
}

func remoteIsSymlink(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -L " + path)
	return err == nil
}

func remoteSymlinkTarget(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "readlink "+path)
}

func remotePackageInstalled(t *testing.T, client *ssh.Client, pkg string) bool {
	_, _, err := client.Run("dpkg -s " + pkg)
	return err == nil
}

const playbookHeader = `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
`

// =============================================================================
// APT Tests
// =============================================================================

func TestPlaybook_AptInstall(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Ensure package is not installed
	client.Run("apt-get remove -y sl 2>/dev/null")

	playbook := playbookHeader + `
  - name: Update cache and install sl
    apt:
      name: sl
      state: present
      update_cache: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for package install")
	}

	// Verify package is installed
	if !remotePackageInstalled(t, client, "sl") {
		t.Error("Package 'sl' should be installed")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") && !strings.Contains(output, "Update cache") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	// Cleanup
	client.Run("apt-get remove -y sl")
}

func TestPlaybook_AptRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Install package first
	client.Run("apt-get update && apt-get install -y sl")

	playbook := playbookHeader + `
  - name: Remove sl package
    apt:
      name: sl
      state: absent
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for package removal")
	}

	// Verify package is removed
	if remotePackageInstalled(t, client, "sl") {
		t.Error("Package 'sl' should be removed")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run")
	}
}

// =============================================================================
// File Module Tests
// =============================================================================

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

// =============================================================================
// Copy Module Tests
// =============================================================================

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

// =============================================================================
// APT Key and Repository Tests
// =============================================================================

func TestPlaybook_AptKeyAndRepo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	keyring := "/etc/apt/keyrings/goansible-test.gpg"
	repoFile := "/etc/apt/sources.list.d/goansible-test.list"

	client.Run("rm -f " + keyring + " " + repoFile)

	playbook := playbookHeader + `
  - name: Add GPG key
    apt_key:
      url: https://apt.grafana.com/gpg.key
      keyring: /etc/apt/keyrings/goansible-test.gpg
      state: present

  - name: Add repository
    apt_repository:
      repo: "deb [signed-by=/etc/apt/keyrings/goansible-test.gpg] https://apt.grafana.com stable main"
      filename: goansible-test
      update_cache: false
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for apt_key and apt_repository")
	}

	// Verify keyring exists
	if !remoteFileExists(t, client, keyring) {
		t.Error("Keyring file should exist")
	}

	// Verify repo file exists with correct content
	repoContent := remoteFileContent(t, client, repoFile)
	if !strings.Contains(repoContent, "apt.grafana.com") {
		t.Error("Repo file should contain grafana URL")
	}

	// Run again - should be idempotent
	output = runPlaybook(t, playbook)
	// Count CHANGED occurrences - should be 0 for idempotent run
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected no changes on second run, got %d", changedCount)
	}

	// Cleanup
	client.Run("rm -f " + keyring + " " + repoFile)
}

// =============================================================================
// Fetch Module Tests
// =============================================================================

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

// =============================================================================
// URI Module Tests
// =============================================================================

func TestPlaybook_URIGet(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make GET request
    uri:
      url: https://httpbin.org/get
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "200 OK") {
		t.Errorf("Expected 200 OK status, got: %s", output)
	}
}

func TestPlaybook_URIPost(t *testing.T) {
	playbook := playbookHeader + `
  - name: Make POST request with JSON body
    uri:
      url: https://httpbin.org/post
      method: POST
      body: '{"key": "value", "number": 42}'
      body_format: json
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for POST request")
	}
}

func TestPlaybook_URIStatusCode(t *testing.T) {
	playbook := playbookHeader + `
  - name: Accept 404 as success
    uri:
      url: https://httpbin.org/status/404
      status_code:
        - 404
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 404 accepted, got: %s", output)
	}
}

func TestPlaybook_URIStatusCodeFail(t *testing.T) {
	playbook := playbookHeader + `
  - name: Fail on unexpected status
    uri:
      url: https://httpbin.org/status/500
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for 500 status")
	}
}

func TestPlaybook_URIHeaders(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with custom headers
    uri:
      url: https://httpbin.org/headers
      headers:
        X-Custom-Header: "test-value"
        Accept: "application/json"
      return_content: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_URIDownload(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	destFile := "/tmp/goansible-uri-download.txt"
	client.Run("rm -f " + destFile)

	playbook := playbookHeader + `
  - name: Download file via URI
    uri:
      url: https://httpbin.org/robots.txt
      dest: /tmp/goansible-uri-download.txt
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for download")
	}

	if !remoteFileExists(t, client, destFile) {
		t.Error("Downloaded file should exist")
	}

	client.Run("rm -f " + destFile)
}

func TestPlaybook_URICreates(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	markerFile := "/tmp/goansible-uri-creates-marker"
	client.Run("touch " + markerFile)
	defer client.Run("rm -f " + markerFile)

	playbook := playbookHeader + `
  - name: Skip request if file exists
    uri:
      url: https://httpbin.org/get
      creates: /tmp/goansible-uri-creates-marker
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "OK") {
		t.Error("Expected OK (skipped)")
	}
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes when creates file exists")
	}
}

func TestPlaybook_URITimeout(t *testing.T) {
	playbook := playbookHeader + `
  - name: Request with short timeout
    uri:
      url: https://httpbin.org/delay/5
      timeout: 2
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "FAILED") {
		t.Error("Expected FAILED for timeout")
	}
	if !strings.Contains(output, "timed out") {
		t.Error("Expected timeout error message")
	}
}

func TestPlaybook_URIFollowRedirects(t *testing.T) {
	playbook := playbookHeader + `
  - name: Follow redirect
    uri:
      url: https://httpbin.org/redirect/1
      follow_redirects: all
      status_code:
        - 200
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success after following redirect, got: %s", output)
	}
}

func TestPlaybook_URINoFollowRedirects(t *testing.T) {
	playbook := playbookHeader + `
  - name: Do not follow redirect
    uri:
      url: https://httpbin.org/redirect/1
      follow_redirects: none
      status_code:
        - 302
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success with 302, got: %s", output)
	}
}

// =============================================================================
// Cron Module Tests
// =============================================================================

func TestPlaybook_CronAddJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add cron job
    cron:
      name: test job
      minute: "0"
      hour: "5"
      job: /usr/bin/echo hello
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for new cron job")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "#Ansible: test job") {
		t.Error("Crontab should contain Ansible marker")
	}
	if !strings.Contains(crontab, "0 5 * * * /usr/bin/echo hello") {
		t.Errorf("Crontab should contain job, got: %s", crontab)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronRemoveJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	setupPlaybook := playbookHeader + `
  - name: Add cron job to remove
    cron:
      name: job to remove
      job: /usr/bin/echo remove
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove cron job
    cron:
      name: job to remove
      state: absent
`
	output := runPlaybook(t, removePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for job removal")
	}

	crontab := remoteExec(t, client, "crontab -l 2>/dev/null || echo empty")
	if strings.Contains(crontab, "job to remove") {
		t.Error("Crontab should not contain removed job")
	}

	output = runPlaybook(t, removePlaybook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronSpecialTime(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add reboot job
    cron:
      name: reboot job
      special_time: reboot
      job: /usr/bin/echo rebooted
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for special_time job")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "@reboot /usr/bin/echo rebooted") {
		t.Errorf("Crontab should contain @reboot job, got: %s", crontab)
	}
}

func TestPlaybook_CronDisabled(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add disabled job
    cron:
      name: disabled job
      job: /usr/bin/echo disabled
      disabled: true
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "#* * * * * /usr/bin/echo disabled") {
		t.Errorf("Crontab should contain commented job, got: %s", crontab)
	}
}

func TestPlaybook_CronEnv(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Add environment variable
    cron:
      name: PATH
      env: true
      job: /opt/bin
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for env variable")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, `PATH="/opt/bin"`) {
		t.Errorf("Crontab should contain PATH env, got: %s", crontab)
	}

	output = runPlaybook(t, playbook)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}
}

func TestPlaybook_CronEnvRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	setupPlaybook := playbookHeader + `
  - name: Add env var
    cron:
      name: MYVAR
      env: true
      job: myvalue
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove env var
    cron:
      name: MYVAR
      env: true
      state: absent
`
	output := runPlaybook(t, removePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for env removal")
	}

	crontab := remoteExec(t, client, "crontab -l 2>/dev/null || echo empty")
	if strings.Contains(crontab, "MYVAR") {
		t.Error("Crontab should not contain removed env var")
	}
}

func TestPlaybook_CronFile(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	cronFile := "/etc/cron.d/goansible-test"
	client.Run("rm -f " + cronFile)
	defer client.Run("rm -f " + cronFile)

	playbook := playbookHeader + `
  - name: Create cron.d file
    cron:
      name: cron.d job
      minute: "30"
      hour: "2"
      user: root
      job: /usr/bin/echo crond
      cron_file: goansible-test
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for cron.d file")
	}

	if !remoteFileExists(t, client, cronFile) {
		t.Error("Cron.d file should exist")
	}

	content := remoteFileContent(t, client, cronFile)
	if !strings.Contains(content, "30 2 * * * root /usr/bin/echo crond") {
		t.Errorf("Cron.d file should contain job with user, got: %s", content)
	}
}

func TestPlaybook_CronFileRemove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	cronFile := "/etc/cron.d/goansible-test-remove"
	client.Run("rm -f " + cronFile)
	defer client.Run("rm -f " + cronFile)

	setupPlaybook := playbookHeader + `
  - name: Create cron.d file
    cron:
      name: temp job
      user: root
      job: /usr/bin/echo temp
      cron_file: goansible-test-remove
`
	runPlaybook(t, setupPlaybook)

	removePlaybook := playbookHeader + `
  - name: Remove cron.d job
    cron:
      name: temp job
      cron_file: goansible-test-remove
      state: absent
`
	output := runPlaybook(t, removePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	if remoteFileExists(t, client, cronFile) {
		t.Error("Cron.d file should be removed when empty")
	}
}

func TestPlaybook_CronUpdateJob(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	client.Run("crontab -r 2>/dev/null || true")
	defer client.Run("crontab -r 2>/dev/null || true")

	setupPlaybook := playbookHeader + `
  - name: Add initial job
    cron:
      name: updatable job
      minute: "0"
      job: /usr/bin/echo original
`
	runPlaybook(t, setupPlaybook)

	updatePlaybook := playbookHeader + `
  - name: Update job
    cron:
      name: updatable job
      minute: "30"
      job: /usr/bin/echo updated
`
	output := runPlaybook(t, updatePlaybook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for job update")
	}

	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "30 * * * * /usr/bin/echo updated") {
		t.Errorf("Crontab should contain updated job, got: %s", crontab)
	}
	if strings.Contains(crontab, "original") {
		t.Error("Crontab should not contain old job")
	}
}

// =============================================================================
// Full Workflow Test
// =============================================================================

func TestPlaybook_FullDeployWorkflow(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	appDir := "/opt/goansible-test-app"
	client.Run("rm -rf " + appDir)

	playbook := playbookHeader + `
  - name: Create application directory
    file:
      path: /opt/goansible-test-app
      state: directory
      mode: "0755"

  - name: Create config subdirectory
    file:
      path: /opt/goansible-test-app/config
      state: directory
      mode: "0755"

  - name: Deploy configuration
    copy:
      content: |
        server:
          host: 0.0.0.0
          port: 8080
        database:
          host: localhost
      dest: /opt/goansible-test-app/config/app.yaml
      mode: "0644"

  - name: Create logs directory
    file:
      path: /opt/goansible-test-app/logs
      state: directory
      mode: "0777"

  - name: Create symlink to config
    file:
      path: /opt/goansible-test-app/current-config
      src: /opt/goansible-test-app/config/app.yaml
      state: link
`
	output := runPlaybook(t, playbook)

	// Should have multiple CHANGED
	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for deployment")
	}

	// Verify structure
	if !remoteDirExists(t, client, appDir) {
		t.Error("App directory should exist")
	}
	if !remoteDirExists(t, client, appDir+"/config") {
		t.Error("Config directory should exist")
	}
	if !remoteDirExists(t, client, appDir+"/logs") {
		t.Error("Logs directory should exist")
	}

	// Verify config content
	configContent := remoteFileContent(t, client, appDir+"/config/app.yaml")
	if !strings.Contains(configContent, "port: 8080") {
		t.Error("Config should contain port setting")
	}

	// Verify symlink
	if !remoteIsSymlink(t, client, appDir+"/current-config") {
		t.Error("Should have symlink")
	}

	// Verify logs permissions
	logsMode := remoteFileMode(t, client, appDir+"/logs")
	if logsMode != "777" {
		t.Errorf("Expected logs mode 777, got %s", logsMode)
	}

	// Run again - should be fully idempotent
	output = runPlaybook(t, playbook)
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected fully idempotent run, got %d changes", changedCount)
	}

	// Cleanup
	cleanupPlaybook := playbookHeader + `
  - name: Cleanup test app
    file:
      path: /opt/goansible-test-app
      state: absent
`
	runPlaybook(t, cleanupPlaybook)

	// Verify cleanup
	if remoteDirExists(t, client, appDir) {
		t.Error("App directory should be deleted")
	}
}
