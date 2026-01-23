//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestWorkflow_PackageAndConfigDeployment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Cleanup before test
	client.Run("apt-get remove -y cowsay 2>/dev/null || true")
	client.Run("rm -rf /etc/goansible-test-cowsay")

	playbook := playbookHeader + `
  - name: Install cowsay package
    apt:
      name: cowsay
      state: present
      update_cache: true

  - name: Create config directory
    file:
      path: /etc/goansible-test-cowsay
      state: directory
      mode: "0755"

  - name: Deploy cowsay config
    copy:
      content: |
        # Cowsay configuration
        default_cow=tux
        wrap_text=true
      dest: /etc/goansible-test-cowsay/config
      mode: "0644"
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify package installed
	if !remotePackageInstalled(t, client, "cowsay") {
		t.Error("cowsay package should be installed")
	}

	// Verify config exists
	if !remoteFileExists(t, client, "/etc/goansible-test-cowsay/config") {
		t.Error("Config file should exist")
	}

	content := remoteFileContent(t, client, "/etc/goansible-test-cowsay/config")
	if !strings.Contains(content, "default_cow=tux") {
		t.Error("Config should contain default_cow setting")
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Idempotent run failed: %s", output)
	}

	// Cleanup
	client.Run("apt-get remove -y cowsay")
	client.Run("rm -rf /etc/goansible-test-cowsay")
}

func TestWorkflow_DownloadAndDeploy(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	appDir := "/opt/goansible-download-test"
	client.Run("rm -rf " + appDir)

	playbook := playbookHeader + `
  - name: Create application directory
    file:
      path: /opt/goansible-download-test
      state: directory
      mode: "0755"

  - name: Download sample file from internet
    uri:
      url: https://httpbin.org/robots.txt
      dest: /opt/goansible-download-test/robots.txt

  - name: Create bin directory
    file:
      path: /opt/goansible-download-test/bin
      state: directory
      mode: "0755"

  - name: Copy downloaded file to bin
    copy:
      src: /opt/goansible-download-test/robots.txt
      dest: /opt/goansible-download-test/bin/robots.txt
      remote_src: true
      mode: "0644"

  - name: Create symlink to latest
    file:
      path: /opt/goansible-download-test/latest
      src: /opt/goansible-download-test/bin/robots.txt
      state: link
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify download
	if !remoteFileExists(t, client, appDir+"/robots.txt") {
		t.Error("Downloaded file should exist")
	}

	// Verify copy
	if !remoteFileExists(t, client, appDir+"/bin/robots.txt") {
		t.Error("Copied file should exist in bin")
	}

	// Verify symlink
	if !remoteIsSymlink(t, client, appDir+"/latest") {
		t.Error("Symlink should exist")
	}

	// Cleanup
	client.Run("rm -rf " + appDir)
}

func TestWorkflow_CronJobSetup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	scriptDir := "/opt/goansible-cron-test"
	client.Run("rm -rf " + scriptDir)
	client.Run("crontab -r 2>/dev/null || true")

	playbook := playbookHeader + `
  - name: Create scripts directory
    file:
      path: /opt/goansible-cron-test
      state: directory
      mode: "0755"

  - name: Deploy backup script
    copy:
      content: |
        #!/bin/bash
        echo "Backup started at $(date)" >> /var/log/goansible-backup.log
        # Simulate backup
        sleep 1
        echo "Backup completed at $(date)" >> /var/log/goansible-backup.log
      dest: /opt/goansible-cron-test/backup.sh
      mode: "0755"

  - name: Deploy cleanup script
    copy:
      content: |
        #!/bin/bash
        find /tmp -name "*.tmp" -mtime +7 -delete 2>/dev/null || true
      dest: /opt/goansible-cron-test/cleanup.sh
      mode: "0755"

  - name: Schedule daily backup
    cron:
      name: daily backup
      minute: "0"
      hour: "2"
      job: /opt/goansible-cron-test/backup.sh

  - name: Schedule weekly cleanup
    cron:
      name: weekly cleanup
      minute: "30"
      hour: "3"
      weekday: "0"
      job: /opt/goansible-cron-test/cleanup.sh
`
	output := runPlaybook(t, playbook)

	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Verify scripts exist and are executable
	if !remoteFileExists(t, client, scriptDir+"/backup.sh") {
		t.Error("Backup script should exist")
	}
	if !remoteFileExists(t, client, scriptDir+"/cleanup.sh") {
		t.Error("Cleanup script should exist")
	}

	mode := remoteFileMode(t, client, scriptDir+"/backup.sh")
	if mode != "755" {
		t.Errorf("Expected mode 755, got %s", mode)
	}

	// Verify cron jobs
	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "daily backup") {
		t.Error("Daily backup cron job should exist")
	}
	if !strings.Contains(crontab, "weekly cleanup") {
		t.Error("Weekly cleanup cron job should exist")
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected idempotent run, got %d changes", changedCount)
	}

	// Cleanup
	client.Run("rm -rf " + scriptDir)
	client.Run("crontab -r 2>/dev/null || true")
}

func TestWorkflow_BlueGreenDeployment(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	baseDir := "/opt/goansible-bluegreen"
	client.Run("rm -rf " + baseDir)

	// Deploy Blue version
	bluePlaybook := playbookHeader + `
  - name: Create base directory
    file:
      path: /opt/goansible-bluegreen
      state: directory
      mode: "0755"

  - name: Create blue version directory
    file:
      path: /opt/goansible-bluegreen/blue
      state: directory
      mode: "0755"

  - name: Deploy blue version app
    copy:
      content: |
        #!/bin/bash
        echo "Blue version 1.0.0"
      dest: /opt/goansible-bluegreen/blue/app.sh
      mode: "0755"

  - name: Deploy blue version config
    copy:
      content: |
        version: 1.0.0
        color: blue
      dest: /opt/goansible-bluegreen/blue/config.yaml
      mode: "0644"

  - name: Point current to blue
    file:
      path: /opt/goansible-bluegreen/current
      src: /opt/goansible-bluegreen/blue
      state: link
      force: true
`
	output := runPlaybook(t, bluePlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Blue deployment failed: %s", output)
	}

	// Verify blue is current
	target := remoteSymlinkTarget(t, client, baseDir+"/current")
	if target != baseDir+"/blue" {
		t.Errorf("Expected current -> blue, got %s", target)
	}

	// Deploy Green version
	greenPlaybook := playbookHeader + `
  - name: Create green version directory
    file:
      path: /opt/goansible-bluegreen/green
      state: directory
      mode: "0755"

  - name: Deploy green version app
    copy:
      content: |
        #!/bin/bash
        echo "Green version 2.0.0"
      dest: /opt/goansible-bluegreen/green/app.sh
      mode: "0755"

  - name: Deploy green version config
    copy:
      content: |
        version: 2.0.0
        color: green
      dest: /opt/goansible-bluegreen/green/config.yaml
      mode: "0644"

  - name: Switch current to green
    file:
      path: /opt/goansible-bluegreen/current
      src: /opt/goansible-bluegreen/green
      state: link
      force: true
`
	output = runPlaybook(t, greenPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Green deployment failed: %s", output)
	}

	// Verify green is now current
	target = remoteSymlinkTarget(t, client, baseDir+"/current")
	if target != baseDir+"/green" {
		t.Errorf("Expected current -> green, got %s", target)
	}

	// Verify blue still exists (for rollback)
	if !remoteDirExists(t, client, baseDir+"/blue") {
		t.Error("Blue version should still exist for rollback")
	}

	// Verify green config
	content := remoteFileContent(t, client, baseDir+"/current/config.yaml")
	if !strings.Contains(content, "version: 2.0.0") {
		t.Error("Current should point to green version 2.0.0")
	}

	// Cleanup
	client.Run("rm -rf " + baseDir)
}

func TestWorkflow_ConfigBackupAndUpdate(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	configDir := "/etc/goansible-backup-test"
	client.Run("rm -rf " + configDir)

	// Initial deployment
	initialPlaybook := playbookHeader + `
  - name: Create config directory
    file:
      path: /etc/goansible-backup-test
      state: directory
      mode: "0755"

  - name: Deploy initial config
    copy:
      content: |
        # Version 1.0
        setting1: value1
        setting2: value2
      dest: /etc/goansible-backup-test/app.conf
      mode: "0644"
`
	output := runPlaybook(t, initialPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Initial deployment failed: %s", output)
	}

	// Verify initial config
	content := remoteFileContent(t, client, configDir+"/app.conf")
	if !strings.Contains(content, "Version 1.0") {
		t.Error("Initial config should contain Version 1.0")
	}

	// Update with backup
	updatePlaybook := playbookHeader + `
  - name: Update config with backup
    copy:
      content: |
        # Version 2.0
        setting1: new_value1
        setting2: new_value2
        setting3: added_value
      dest: /etc/goansible-backup-test/app.conf
      mode: "0644"
      backup: true
`
	output = runPlaybook(t, updatePlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Update deployment failed: %s", output)
	}

	// Verify new config
	content = remoteFileContent(t, client, configDir+"/app.conf")
	if !strings.Contains(content, "Version 2.0") {
		t.Error("Updated config should contain Version 2.0")
	}

	// Verify backup exists
	backupFiles := remoteExec(t, client, "ls "+configDir+"/*.conf.* 2>/dev/null | wc -l")
	if strings.TrimSpace(backupFiles) == "0" {
		t.Error("Backup file should exist")
	}

	// Verify backup contains old content
	backupContent := remoteExec(t, client, "cat "+configDir+"/*.conf.* 2>/dev/null | head -1")
	if !strings.Contains(backupContent, "Version 1.0") {
		t.Error("Backup should contain old Version 1.0")
	}

	// Cleanup
	client.Run("rm -rf " + configDir)
}

func TestWorkflow_MultiUserPermissions(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	baseDir := "/opt/goansible-multiuser"
	client.Run("rm -rf " + baseDir)

	playbook := playbookHeader + `
  - name: Create base directory
    file:
      path: /opt/goansible-multiuser
      state: directory
      mode: "0755"
      owner: root
      group: root

  - name: Create shared data directory
    file:
      path: /opt/goansible-multiuser/shared
      state: directory
      mode: "0775"
      owner: root
      group: nogroup

  - name: Create private directory for nobody
    file:
      path: /opt/goansible-multiuser/private
      state: directory
      mode: "0700"
      owner: nobody
      group: nogroup

  - name: Create public read-only directory
    file:
      path: /opt/goansible-multiuser/public
      state: directory
      mode: "0755"
      owner: root
      group: root

  - name: Deploy shared file
    copy:
      content: "Shared content accessible by group"
      dest: /opt/goansible-multiuser/shared/data.txt
      mode: "0664"
      owner: root
      group: nogroup

  - name: Deploy private file
    copy:
      content: "Private content for nobody user only"
      dest: /opt/goansible-multiuser/private/secret.txt
      mode: "0600"
      owner: nobody
      group: nogroup

  - name: Deploy public file
    copy:
      content: "Public content readable by all"
      dest: /opt/goansible-multiuser/public/readme.txt
      mode: "0644"
      owner: root
      group: root
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Deployment failed: %s", output)
	}

	// Verify shared directory
	mode := remoteFileMode(t, client, baseDir+"/shared")
	if mode != "775" {
		t.Errorf("Expected shared mode 775, got %s", mode)
	}
	group := remoteFileGroup(t, client, baseDir+"/shared")
	if group != "nogroup" {
		t.Errorf("Expected shared group nogroup, got %s", group)
	}

	// Verify private directory
	mode = remoteFileMode(t, client, baseDir+"/private")
	if mode != "700" {
		t.Errorf("Expected private mode 700, got %s", mode)
	}
	owner := remoteFileOwner(t, client, baseDir+"/private")
	if owner != "nobody" {
		t.Errorf("Expected private owner nobody, got %s", owner)
	}

	// Verify private file
	mode = remoteFileMode(t, client, baseDir+"/private/secret.txt")
	if mode != "600" {
		t.Errorf("Expected secret file mode 600, got %s", mode)
	}

	// Verify public file
	mode = remoteFileMode(t, client, baseDir+"/public/readme.txt")
	if mode != "644" {
		t.Errorf("Expected public file mode 644, got %s", mode)
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected idempotent run, got %d changes", changedCount)
	}

	// Cleanup
	client.Run("rm -rf " + baseDir)
}

func TestWorkflow_FetchAndBackup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Create files on remote to fetch
	remoteDir := "/opt/goansible-fetch-source"
	client.Run("rm -rf " + remoteDir)
	client.Run("mkdir -p " + remoteDir)
	client.Run("echo 'config data version 1' > " + remoteDir + "/config.txt")
	client.Run("echo 'important log data' > " + remoteDir + "/app.log")

	// Create local backup directory
	localBackupDir := filepath.Join(os.TempDir(), "goansible-fetch-backup")
	os.RemoveAll(localBackupDir)
	os.MkdirAll(localBackupDir, 0755)

	playbook := playbookHeader + `
  - name: Fetch config file
    fetch:
      src: /opt/goansible-fetch-source/config.txt
      dest: ` + localBackupDir + `/
      flat: false

  - name: Fetch log file flat
    fetch:
      src: /opt/goansible-fetch-source/app.log
      dest: ` + localBackupDir + `/app.log
      flat: true
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Fetch failed: %s", output)
	}

	// Verify hierarchical fetch (includes hostname from playbook)
	configPath := filepath.Join(localBackupDir, "testhost", "opt/goansible-fetch-source/config.txt")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("Config file should be fetched to %s", configPath)
	}

	// Verify flat fetch
	logPath := filepath.Join(localBackupDir, "app.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("Log file should be fetched flat to %s", logPath)
	}

	// Verify content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Errorf("Failed to read fetched log: %v", err)
	}
	if !strings.Contains(string(content), "important log data") {
		t.Error("Fetched log should contain expected content")
	}

	// Idempotency - fetch again should not change
	output = runPlaybook(t, playbook)
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected idempotent fetch, got %d changes", changedCount)
	}

	// Cleanup
	client.Run("rm -rf " + remoteDir)
	os.RemoveAll(localBackupDir)
}

func TestWorkflow_RollbackScenario(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	baseDir := "/opt/goansible-rollback"
	client.Run("rm -rf " + baseDir)

	// Deploy version 1
	v1Playbook := playbookHeader + `
  - name: Create releases directory
    file:
      path: /opt/goansible-rollback/releases
      state: directory
      mode: "0755"

  - name: Create v1 release
    file:
      path: /opt/goansible-rollback/releases/v1.0.0
      state: directory
      mode: "0755"

  - name: Deploy v1 application
    copy:
      content: |
        #!/bin/bash
        echo "Application v1.0.0"
      dest: /opt/goansible-rollback/releases/v1.0.0/app.sh
      mode: "0755"

  - name: Deploy v1 config
    copy:
      content: |
        version: 1.0.0
        feature_x: false
      dest: /opt/goansible-rollback/releases/v1.0.0/config.yaml
      mode: "0644"

  - name: Link current to v1
    file:
      path: /opt/goansible-rollback/current
      src: /opt/goansible-rollback/releases/v1.0.0
      state: link
`
	output := runPlaybook(t, v1Playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("V1 deployment failed: %s", output)
	}

	// Verify v1 is current
	content := remoteFileContent(t, client, baseDir+"/current/config.yaml")
	if !strings.Contains(content, "version: 1.0.0") {
		t.Error("Current should be v1.0.0")
	}

	// Deploy version 2
	v2Playbook := playbookHeader + `
  - name: Create v2 release
    file:
      path: /opt/goansible-rollback/releases/v2.0.0
      state: directory
      mode: "0755"

  - name: Deploy v2 application
    copy:
      content: |
        #!/bin/bash
        echo "Application v2.0.0 with new features"
      dest: /opt/goansible-rollback/releases/v2.0.0/app.sh
      mode: "0755"

  - name: Deploy v2 config
    copy:
      content: |
        version: 2.0.0
        feature_x: true
        feature_y: true
      dest: /opt/goansible-rollback/releases/v2.0.0/config.yaml
      mode: "0644"

  - name: Link current to v2
    file:
      path: /opt/goansible-rollback/current
      src: /opt/goansible-rollback/releases/v2.0.0
      state: link
      force: true
`
	output = runPlaybook(t, v2Playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("V2 deployment failed: %s", output)
	}

	// Verify v2 is current
	content = remoteFileContent(t, client, baseDir+"/current/config.yaml")
	if !strings.Contains(content, "version: 2.0.0") {
		t.Error("Current should be v2.0.0")
	}

	// Rollback to v1
	rollbackPlaybook := playbookHeader + `
  - name: Rollback to v1
    file:
      path: /opt/goansible-rollback/current
      src: /opt/goansible-rollback/releases/v1.0.0
      state: link
      force: true
`
	output = runPlaybook(t, rollbackPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Rollback failed: %s", output)
	}

	// Verify v1 is current again
	content = remoteFileContent(t, client, baseDir+"/current/config.yaml")
	if !strings.Contains(content, "version: 1.0.0") {
		t.Error("Current should be rolled back to v1.0.0")
	}

	// Verify both versions still exist
	if !remoteDirExists(t, client, baseDir+"/releases/v1.0.0") {
		t.Error("V1 release should still exist")
	}
	if !remoteDirExists(t, client, baseDir+"/releases/v2.0.0") {
		t.Error("V2 release should still exist")
	}

	// Cleanup
	client.Run("rm -rf " + baseDir)
}

func TestWorkflow_BecomeUser(t *testing.T) {
	// Test running as non-root user with become: true
	client := getClient(t)
	defer client.Close()

	testDir := "/opt/goansible-become-test"
	client.Run("rm -rf " + testDir)

	// Use testuser with become
	playbook := `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: testuser
    password: testpass
    become: true
    become_password: testpass

tasks:
  - name: Create directory as root via become
    file:
      path: /opt/goansible-become-test
      state: directory
      mode: "0755"
      owner: root
      group: root

  - name: Create file as root via become
    copy:
      content: "Created via become"
      dest: /opt/goansible-become-test/root-file.txt
      mode: "0644"
      owner: root
      group: root

  - name: Create file owned by testuser
    copy:
      content: "Owned by testuser"
      dest: /opt/goansible-become-test/testuser-file.txt
      mode: "0644"
      owner: testuser
      group: testuser
`
	output := runPlaybook(t, playbook)

	// Check for connection or authentication failure first
	if strings.Contains(output, "Failed to connect") {
		t.Skipf("Skipping test - SSH connection as testuser failed: %s", output)
	}

	if strings.Contains(output, "FAILED") {
		t.Errorf("Become deployment failed: %s", output)
	}

	// Verify directory created with root ownership
	if !remoteDirExists(t, client, testDir) {
		t.Errorf("Directory should exist. Playbook output: %s", output)
	}
	owner := remoteFileOwner(t, client, testDir)
	if owner != "root" {
		t.Errorf("Expected owner root, got %s", owner)
	}

	// Verify root-owned file
	owner = remoteFileOwner(t, client, testDir+"/root-file.txt")
	if owner != "root" {
		t.Errorf("Expected root-file owner root, got %s", owner)
	}

	// Verify testuser-owned file
	owner = remoteFileOwner(t, client, testDir+"/testuser-file.txt")
	if owner != "testuser" {
		t.Errorf("Expected testuser-file owner testuser, got %s", owner)
	}

	// Cleanup
	client.Run("rm -rf " + testDir)
}

func TestWorkflow_WebServerSetup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Cleanup
	client.Run("apt-get remove -y nginx 2>/dev/null || true")
	client.Run("rm -rf /var/www/goansible-test")
	client.Run("rm -f /etc/nginx/sites-available/goansible-test")
	client.Run("rm -f /etc/nginx/sites-enabled/goansible-test")

	playbook := playbookHeader + `
  - name: Install nginx
    apt:
      name: nginx
      state: present
      update_cache: true
      cache_valid_time: 3600

  - name: Create web root directory
    file:
      path: /var/www/goansible-test
      state: directory
      mode: "0755"
      owner: www-data
      group: www-data

  - name: Deploy index page
    copy:
      content: |
        <!DOCTYPE html>
        <html>
        <head><title>GoAnsible Test</title></head>
        <body><h1>Hello from GoAnsible!</h1></body>
        </html>
      dest: /var/www/goansible-test/index.html
      mode: "0644"
      owner: www-data
      group: www-data

  - name: Deploy nginx site config
    copy:
      content: |
        server {
            listen 8888;
            root /var/www/goansible-test;
            index index.html;
            server_name _;
            location / {
                try_files $uri $uri/ =404;
            }
        }
      dest: /etc/nginx/sites-available/goansible-test
      mode: "0644"

  - name: Enable site
    file:
      path: /etc/nginx/sites-enabled/goansible-test
      src: /etc/nginx/sites-available/goansible-test
      state: link
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Web server setup failed: %s", output)
	}

	// Verify nginx installed
	if !remotePackageInstalled(t, client, "nginx") {
		t.Error("nginx should be installed")
	}

	// Verify web root
	if !remoteDirExists(t, client, "/var/www/goansible-test") {
		t.Error("Web root should exist")
	}

	// Verify index page
	content := remoteFileContent(t, client, "/var/www/goansible-test/index.html")
	if !strings.Contains(content, "Hello from GoAnsible") {
		t.Error("Index page should contain expected content")
	}

	// Verify site enabled
	if !remoteIsSymlink(t, client, "/etc/nginx/sites-enabled/goansible-test") {
		t.Error("Site should be enabled via symlink")
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected idempotent run, got %d changes", changedCount)
	}

	// Cleanup
	client.Run("rm -f /etc/nginx/sites-enabled/goansible-test")
	client.Run("rm -f /etc/nginx/sites-available/goansible-test")
	client.Run("rm -rf /var/www/goansible-test")
	client.Run("apt-get remove -y nginx")
}

func TestWorkflow_MultiStepDatabaseSetup(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	baseDir := "/opt/goansible-db-setup"
	client.Run("rm -rf " + baseDir)

	// Note: loops aren't supported in goansible, so we expand manually
	playbook := playbookHeader + `
  - name: Create data directory
    file:
      path: /opt/goansible-db-setup/data
      state: directory
      mode: "0700"

  - name: Create logs directory
    file:
      path: /opt/goansible-db-setup/logs
      state: directory
      mode: "0755"

  - name: Create backups directory
    file:
      path: /opt/goansible-db-setup/backups
      state: directory
      mode: "0700"

  - name: Create config directory
    file:
      path: /opt/goansible-db-setup/config
      state: directory
      mode: "0755"

  - name: Deploy database config
    copy:
      content: |
        [mysqld]
        datadir=/opt/goansible-db-setup/data
        log_error=/opt/goansible-db-setup/logs/error.log
        port=3306
        bind-address=127.0.0.1
      dest: /opt/goansible-db-setup/config/my.cnf
      mode: "0644"

  - name: Create backup script
    copy:
      content: |
        #!/bin/bash
        BACKUP_DIR=/opt/goansible-db-setup/backups
        DATE=$(date +%Y%m%d_%H%M%S)
        echo "Backup created at $DATE" > $BACKUP_DIR/backup_$DATE.sql
      dest: /opt/goansible-db-setup/backup.sh
      mode: "0755"

  - name: Schedule daily backup
    cron:
      name: database backup
      minute: "0"
      hour: "1"
      job: /opt/goansible-db-setup/backup.sh

  - name: Create mysql data subdirectory
    file:
      path: /opt/goansible-db-setup/data/mysql
      state: directory
      mode: "0700"

  - name: Create performance_schema subdirectory
    file:
      path: /opt/goansible-db-setup/data/performance_schema
      state: directory
      mode: "0700"

  - name: Create sys subdirectory
    file:
      path: /opt/goansible-db-setup/data/sys
      state: directory
      mode: "0700"
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Database setup failed: %s", output)
	}

	// Verify directory structure
	dirs := []string{"data", "logs", "backups", "config", "data/mysql", "data/performance_schema", "data/sys"}
	for _, dir := range dirs {
		if !remoteDirExists(t, client, baseDir+"/"+dir) {
			t.Errorf("Directory %s should exist", dir)
		}
	}

	// Verify config file
	content := remoteFileContent(t, client, baseDir+"/config/my.cnf")
	if !strings.Contains(content, "port=3306") {
		t.Error("Config should contain port setting")
	}

	// Verify backup script is executable
	mode := remoteFileMode(t, client, baseDir+"/backup.sh")
	if mode != "755" {
		t.Errorf("Expected backup script mode 755, got %s", mode)
	}

	// Verify cron job
	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "database backup") {
		t.Error("Database backup cron job should exist")
	}

	// Verify sensitive directories have restricted permissions
	mode = remoteFileMode(t, client, baseDir+"/data")
	if mode != "700" {
		t.Errorf("Expected data directory mode 700, got %s", mode)
	}

	// Idempotency check
	output = runPlaybook(t, playbook)
	changedCount := strings.Count(output, "CHANGED")
	if changedCount > 0 {
		t.Errorf("Expected idempotent run, got %d changes", changedCount)
	}

	// Cleanup
	client.Run("rm -rf " + baseDir)
	client.Run("crontab -r 2>/dev/null || true")
}

func TestWorkflow_CompleteAppLifecycle(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	baseDir := "/opt/goansible-lifecycle"
	client.Run("rm -rf " + baseDir)

	// Phase 1: Initial Setup
	setupPlaybook := playbookHeader + `
  - name: Create app directory
    file:
      path: /opt/goansible-lifecycle/app
      state: directory
      mode: "0755"

  - name: Create config directory
    file:
      path: /opt/goansible-lifecycle/config
      state: directory
      mode: "0755"

  - name: Create logs directory
    file:
      path: /opt/goansible-lifecycle/logs
      state: directory
      mode: "0777"

  - name: Create data directory
    file:
      path: /opt/goansible-lifecycle/data
      state: directory
      mode: "0755"
`
	output := runPlaybook(t, setupPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Setup phase failed: %s", output)
	}

	// Phase 2: Deploy Application
	deployPlaybook := playbookHeader + `
  - name: Deploy application binary
    copy:
      content: |
        #!/bin/bash
        echo "MyApp v1.0.0 starting..."
        echo "Reading config from /opt/goansible-lifecycle/config/app.yaml"
      dest: /opt/goansible-lifecycle/app/myapp
      mode: "0755"

  - name: Deploy configuration
    copy:
      content: |
        app:
          name: MyApp
          version: 1.0.0
        server:
          host: 0.0.0.0
          port: 8080
        logging:
          level: info
          file: /opt/goansible-lifecycle/logs/app.log
      dest: /opt/goansible-lifecycle/config/app.yaml
      mode: "0644"

  - name: Create convenience symlink
    file:
      path: /usr/local/bin/myapp
      src: /opt/goansible-lifecycle/app/myapp
      state: link
`
	output = runPlaybook(t, deployPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Deploy phase failed: %s", output)
	}

	// Verify deployment
	if !remoteFileExists(t, client, baseDir+"/app/myapp") {
		t.Error("Application binary should exist")
	}
	if !remoteIsSymlink(t, client, "/usr/local/bin/myapp") {
		t.Error("Convenience symlink should exist")
	}

	// Phase 3: Configure Monitoring
	monitorPlaybook := playbookHeader + `
  - name: Create monitoring script
    copy:
      content: |
        #!/bin/bash
        if pgrep -f "myapp" > /dev/null; then
          echo "OK: myapp is running"
          exit 0
        else
          echo "CRITICAL: myapp is not running"
          exit 2
        fi
      dest: /opt/goansible-lifecycle/app/check_myapp.sh
      mode: "0755"

  - name: Schedule health check
    cron:
      name: myapp health check
      minute: "*/5"
      job: /opt/goansible-lifecycle/app/check_myapp.sh >> /opt/goansible-lifecycle/logs/health.log 2>&1
`
	output = runPlaybook(t, monitorPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Monitor phase failed: %s", output)
	}

	// Verify monitoring
	if !remoteFileExists(t, client, baseDir+"/app/check_myapp.sh") {
		t.Error("Health check script should exist")
	}
	crontab := remoteExec(t, client, "crontab -l")
	if !strings.Contains(crontab, "myapp health check") {
		t.Error("Health check cron job should exist")
	}

	// Phase 4: Update Application
	updatePlaybook := playbookHeader + `
  - name: Backup current config
    copy:
      src: /opt/goansible-lifecycle/config/app.yaml
      dest: /opt/goansible-lifecycle/config/app.yaml.bak
      remote_src: true

  - name: Update application
    copy:
      content: |
        #!/bin/bash
        echo "MyApp v1.1.0 starting..."
        echo "New features enabled!"
        echo "Reading config from /opt/goansible-lifecycle/config/app.yaml"
      dest: /opt/goansible-lifecycle/app/myapp
      mode: "0755"

  - name: Update configuration
    copy:
      content: |
        app:
          name: MyApp
          version: 1.1.0
        server:
          host: 0.0.0.0
          port: 8080
        logging:
          level: debug
          file: /opt/goansible-lifecycle/logs/app.log
        features:
          new_feature: true
      dest: /opt/goansible-lifecycle/config/app.yaml
      mode: "0644"
`
	output = runPlaybook(t, updatePlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Update phase failed: %s", output)
	}

	// Verify update
	content := remoteFileContent(t, client, baseDir+"/config/app.yaml")
	if !strings.Contains(content, "version: 1.1.0") {
		t.Error("Config should be updated to v1.1.0")
	}

	// Verify backup exists
	if !remoteFileExists(t, client, baseDir+"/config/app.yaml.bak") {
		t.Error("Config backup should exist")
	}
	backupContent := remoteFileContent(t, client, baseDir+"/config/app.yaml.bak")
	if !strings.Contains(backupContent, "version: 1.0.0") {
		t.Error("Backup should contain old version")
	}

	// Phase 5: Cleanup/Teardown
	cleanupPlaybook := playbookHeader + `
  - name: Remove convenience symlink
    file:
      path: /usr/local/bin/myapp
      state: absent

  - name: Remove cron job
    cron:
      name: myapp health check
      state: absent

  - name: Remove application
    file:
      path: /opt/goansible-lifecycle
      state: absent
`
	output = runPlaybook(t, cleanupPlaybook)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Cleanup phase failed: %s", output)
	}

	// Verify cleanup
	if remoteFileExists(t, client, "/usr/local/bin/myapp") {
		t.Error("Symlink should be removed")
	}
	if remoteDirExists(t, client, baseDir) {
		t.Error("Application directory should be removed")
	}
	crontab = remoteExec(t, client, "crontab -l 2>/dev/null || echo ''")
	if strings.Contains(crontab, "myapp health check") {
		t.Error("Cron job should be removed")
	}
}
