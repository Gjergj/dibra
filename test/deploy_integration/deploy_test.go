//go:build integration

package deploy_integration

import (
	"strings"
	"testing"
	"time"
)

func TestDibraDeployBlackBox(t *testing.T) {
	client, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	t.Run("executes_a_complete_local_project", func(t *testing.T) {
		resetHarness(t, client)
		queueFixture(t, client, "001-happy", "happy", true)
		startDeploy(t, client)
		defer stopDeploy(t, client)

		waitForCondition(t, 45*time.Second, "both playbooks to finish", func() bool {
			return remoteFileExists(client, remoteResult+"/order.txt") && strings.Contains(readRemoteFile(t, client, remoteResult+"/order.txt"), "second")
		})
		expectedFiles := map[string]string{
			"order.txt":             "first\nsecond",
			"imported.txt":          "imported",
			"included.txt":          "included",
			"loop.txt":              "one\ntwo",
			"registered.txt":        "registered-value",
			"copied.txt":            "copied-from-project",
			"template.txt":          "message=from-vars-file host=localhost",
			"script.txt":            "script-run",
			"fetched.txt":           "copied-from-project",
			"host.txt":              "host=localhost",
			"unarchived/inside.txt": "unarchived-from-project",
		}
		for relativePath, expected := range expectedFiles {
			actual := strings.TrimSpace(readRemoteFile(t, client, remoteResult+"/"+relativePath))
			if actual != expected {
				t.Errorf("%s = %q, want %q", relativePath, actual, expected)
			}
		}
		if facts := readRemoteFile(t, client, remoteResult+"/facts.txt"); !strings.Contains(facts, "user=root host=") {
			t.Errorf("gathered facts were not available to the next task: %q", facts)
		}
		waitForCondition(t, 10*time.Second, "job cleanup", func() bool {
			return jobDirectoriesEmpty(client)
		})
		waitForCondition(t, 10*time.Second, "successful outcome report", func() bool {
			return strings.Contains(readRemoteFile(t, client, requestLog), "outcome 001-happy succeeded")
		})
		if !serviceIsActive(client) {
			t.Fatal("dibra-deploy service stopped after a successful non-reboot job")
		}
		if mode, modeErr := runRemote(client, "stat -c %a /var/lib/dibra-deploy"); modeErr != nil || strings.TrimSpace(mode) != "700" {
			t.Fatalf("state directory mode = %q, error %v", mode, modeErr)
		}
		if !remoteFileExists(client, "/var/lib/dibra-deploy/agent/dibra-agent") {
			t.Error("versioned runtime agent was not installed")
		}
		if enabled, enabledErr := runRemote(client, "systemctl is-enabled dibra-deploy.service"); enabledErr != nil || strings.TrimSpace(enabled) != "enabled" {
			t.Fatalf("sample systemd service is not enabled: %q, %v", enabled, enabledErr)
		}
		if user := serviceProperty(t, client, "User"); user != "root" {
			t.Errorf("systemd User = %q, want root", user)
		}
		if logs := serviceLogs(t, client); !strings.Contains(logs, "job completed successfully") {
			t.Errorf("journald does not contain successful completion:\n%s", logs)
		}
	})

	t.Run("stops_the_job_after_the_first_failed_playbook", func(t *testing.T) {
		resetHarness(t, client)
		queueFixture(t, client, "001-failure", "failure", false)
		startDeploy(t, client)
		defer stopDeploy(t, client)

		waitForCondition(t, 30*time.Second, "failed playbook result", func() bool {
			return strings.Contains(serviceLogs(t, client), `playbook "failing.yaml" failed`)
		})
		if !remoteFileExists(client, remoteResult+"/first-ran.txt") {
			t.Error("first playbook did not execute")
		}
		if remoteFileExists(client, remoteResult+"/must-not-run.txt") {
			t.Error("playbook after the failure unexpectedly executed")
		}
		if !serviceIsActive(client) {
			t.Error("daemon exited after a failed job instead of continuing to poll")
		}
		waitForCondition(t, 10*time.Second, "failed outcome report", func() bool {
			return strings.Contains(readRemoteFile(t, client, requestLog), "outcome 001-failure failed")
		})
	})

	t.Run("rejects_an_unsafe_archive_and_remains_running", func(t *testing.T) {
		resetHarness(t, client)
		queueArchive(t, client, "001-traversal", createTraversalArchive(t))
		startDeploy(t, client)
		defer stopDeploy(t, client)

		waitForCondition(t, 30*time.Second, "unsafe archive rejection", func() bool {
			return strings.Contains(serviceLogs(t, client), "contains path traversal")
		})
		if remoteFileExists(client, "/tmp/dibra-deploy-it-escape") {
			t.Error("unsafe archive wrote outside the extraction directory")
		}
		if !serviceIsActive(client) {
			t.Error("daemon exited after rejecting an invalid archive")
		}
		waitForCondition(t, 10*time.Second, "invalid job cleanup", func() bool {
			return jobDirectoriesEmpty(client)
		})
		waitForCondition(t, 10*time.Second, "unsafe archive outcome report", func() bool {
			return strings.Contains(readRemoteFile(t, client, requestLog), "outcome 001-traversal failed")
		})
	})

	t.Run("handles_204_and_sigterm_cleanly", func(t *testing.T) {
		resetHarness(t, client)
		startDeploy(t, client)
		waitForCondition(t, 15*time.Second, "HTTP 204 poll", func() bool {
			return strings.Contains(readRemoteFile(t, client, requestLog), " 204 empty")
		})
		stopDeploy(t, client)
		if status := serviceProperty(t, client, "ExecMainStatus"); status != "0" {
			t.Errorf("SIGTERM exit status = %q, want 0", status)
		}
		if state := serviceProperty(t, client, "ActiveState"); state != "inactive" {
			t.Errorf("ActiveState after stop = %q, want inactive", state)
		}
	})

	t.Run("uses_the_real_agent_but_a_fake_reboot_command", func(t *testing.T) {
		resetHarness(t, client)
		bootIDBefore, bootErr := runRemote(client, "cat /proc/sys/kernel/random/boot_id")
		if bootErr != nil {
			t.Fatal(bootErr)
		}
		queueFixture(t, client, "001-reboot", "reboot", false)
		startDeploy(t, client)

		waitForCondition(t, 30*time.Second, "fake reboot and clean daemon exit", func() bool {
			return remoteFileExists(client, remoteRoot+"/fake-reboot.txt") && !serviceIsActive(client)
		})
		if status := serviceProperty(t, client, "ExecMainStatus"); status != "0" {
			t.Errorf("reboot job exit status = %q, want 0", status)
		}
		if result := serviceProperty(t, client, "Result"); result != "success" {
			t.Errorf("systemd result = %q, want success", result)
		}
		bootIDAfter, bootErr := runRemote(client, "cat /proc/sys/kernel/random/boot_id")
		if bootErr != nil {
			t.Fatal(bootErr)
		}
		if bootIDAfter != bootIDBefore {
			t.Error("integration test unexpectedly rebooted the container")
		}
		if logs := serviceLogs(t, client); !strings.Contains(logs, "local reboot initiated") {
			t.Errorf("reboot completion was not logged:\n%s", logs)
		}
		if !jobDirectoriesEmpty(client) {
			t.Error("reboot job directory was not cleaned before exit")
		}
		if requests := readRemoteFile(t, client, requestLog); !strings.Contains(requests, "outcome 001-reboot succeeded") {
			t.Errorf("reboot outcome was not reported:\n%s", requests)
		}
	})
}
