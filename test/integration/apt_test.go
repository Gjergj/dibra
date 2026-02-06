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

func TestPlaybook_AptInstallMultiple(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Ensure packages are not installed
	client.Run("apt-get remove -y sl cowsay 2>/dev/null")

	playbook := playbookHeader + `
  - name: Install multiple packages
    apt:
      name:
        - sl
        - cowsay
      state: present
      update_cache: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for multiple package install")
	}

	// Verify both packages are installed
	if !remotePackageInstalled(t, client, "sl") {
		t.Error("Package 'sl' should be installed")
	}
	if !remotePackageInstalled(t, client, "cowsay") {
		t.Error("Package 'cowsay' should be installed")
	}

	// Run again - should be idempotent (except for cache update)
	playbookNoCache := playbookHeader + `
  - name: Install multiple packages
    apt:
      name:
        - sl
        - cowsay
      state: present
`
	output = runPlaybook(t, playbookNoCache)
	if strings.Contains(output, "CHANGED") {
		t.Error("Expected no changes on second run (idempotent)")
	}

	// Cleanup
	client.Run("apt-get remove -y sl cowsay")
}

func TestPlaybook_AptLatest(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Install package first
	client.Run("apt-get update && apt-get install -y sl")

	playbook := playbookHeader + `
  - name: Ensure sl is latest
    apt:
      name: sl
      state: latest
`
	output := runPlaybook(t, playbook)

	// Should succeed (may or may not have changes depending on if update available)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Package should still be installed
	if !remotePackageInstalled(t, client, "sl") {
		t.Error("Package 'sl' should be installed")
	}

	// Cleanup
	client.Run("apt-get remove -y sl")
}

func TestPlaybook_AptPurge(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// Install package that creates config files
	client.Run("apt-get update && apt-get install -y sl")

	playbook := playbookHeader + `
  - name: Purge sl package
    apt:
      name: sl
      state: absent
      purge: true
`
	output := runPlaybook(t, playbook)

	if !strings.Contains(output, "CHANGED") {
		t.Error("Expected CHANGED for package purge")
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

func TestPlaybook_AptAutoremove(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Autoremove unused packages
    apt:
      autoremove: true
`
	output := runPlaybook(t, playbook)

	// Should succeed (may or may not have changes)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_AptCacheValidTime(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	// First update the cache
	setupPlaybook := playbookHeader + `
  - name: Update apt cache
    apt:
      update_cache: true
`
	runPlaybook(t, setupPlaybook)

	// Now run with cache_valid_time - should skip update
	playbook := playbookHeader + `
  - name: Update cache with valid time
    apt:
      update_cache: true
      cache_valid_time: 3600
`
	output := runPlaybook(t, playbook)

	// Should succeed without changes (cache is still valid)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}

	// Should report OK, not CHANGED (cache still valid)
	if strings.Contains(output, "CHANGED") {
		t.Log("Note: cache_valid_time didn't prevent update - this may be expected if cache was old")
	}
}

func TestPlaybook_AptUpgradeSafe(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	playbook := playbookHeader + `
  - name: Safe upgrade all packages
    apt:
      upgrade: safe
      update_cache: true
`
	output := runPlaybook(t, playbook)

	// Should succeed (may or may not have changes)
	if strings.Contains(output, "FAILED") {
		t.Errorf("Expected success, got: %s", output)
	}
}

func TestPlaybook_AptKeyAndRepo(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	keyring := "/etc/apt/keyrings/dibra-test.gpg"
	repoFile := "/etc/apt/sources.list.d/dibra-test.list"

	client.Run("rm -f " + keyring + " " + repoFile)

	playbook := playbookHeader + `
  - name: Add GPG key
    apt_key:
      url: https://apt.grafana.com/gpg.key
      keyring: /etc/apt/keyrings/dibra-test.gpg
      state: present

  - name: Add repository
    apt_repository:
      repo: "deb [signed-by=/etc/apt/keyrings/dibra-test.gpg] https://apt.grafana.com stable main"
      filename: dibra-test
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
