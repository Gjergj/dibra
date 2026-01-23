//go:build integration

package integration

import (
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
