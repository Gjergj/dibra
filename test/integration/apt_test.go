//go:build integration

package integration

import (
	"strings"
	"testing"
)

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
